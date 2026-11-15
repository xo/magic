//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// The filters in this file copy the behavior of Bra86.c, Bra.c, BraIA64.c
// and Delta.c of 7-Zip by Igor Pavlov, which are in the public domain. Each
// function names the C function that it copies. They decode only.

package archive

import (
	"encoding/binary"
	"fmt"
	"io"
)

// szConverter changes data in place as a filter of 7-Zip decodes it. It
// keeps its state between calls, and returns the count of bytes at the
// start of b that it finished. The bytes after them come again at the
// start of the next call, with more data after them.
type szConverter interface {
	convert(b []byte) int
}

// szFilterWriter runs a filter on the data written to it, and writes the
// result to w.
type szFilterWriter struct {
	c   szConverter
	w   io.Writer
	buf []byte // the bytes that the filter did not finish
}

// Write is the method of io.Writer.
func (f *szFilterWriter) Write(p []byte) (int, error) {
	f.buf = append(f.buf, p...)
	n := f.c.convert(f.buf)
	if n > 0 {
		if _, err := f.w.Write(f.buf[:n]); err != nil {
			return 0, fmt.Errorf("writing the output of a filter: %w", err)
		}
		f.buf = f.buf[:copy(f.buf, f.buf[n:])]
	}
	return len(p), nil
}

// flush writes the bytes at the end that the filter did not change, as the
// FilterCoder of 7-Zip writes them at the end of the stream.
func (f *szFilterWriter) flush() error {
	_, err := f.w.Write(f.buf)
	f.buf = nil
	if err != nil {
		return fmt.Errorf("writing the output of a filter: %w", err)
	}
	return nil
}

// szBranch is a filter of branches that keeps only the position of the
// data, ip.
type szBranch struct {
	ip   uint32
	conv func(b []byte, ip uint32) int
}

func (f *szBranch) convert(b []byte) int {
	n := f.conv(b, f.ip)
	f.ip += uint32(n)
	return n
}

// szBranchFilter makes a filter of branches that takes no properties.
func szBranchFilter(conv func(b []byte, ip uint32) int) func([]byte) (szConverter, bool) {
	return func(props []byte) (szConverter, bool) {
		return &szBranch{conv: conv}, len(props) == 0
	}
}

// newSzARM64 makes the filter ARM64. Its properties are empty, or the
// position of the start in 4 bytes, a multiple of 4, as the decoder of
// BranchMisc.cpp accepts them.
func newSzARM64(props []byte) (szConverter, bool) {
	f := &szBranch{conv: szARM64Convert}
	switch len(props) {
	case 0:
	case 4:
		f.ip = binary.LittleEndian.Uint32(props)
		if f.ip&3 != 0 {
			return nil, false
		}
	default:
		return nil, false
	}
	return f, true
}

// szX86 is the filter BCJ, for the code of x86.
type szX86 struct {
	ip   uint32
	mask uint32
}

func newSzX86(props []byte) (szConverter, bool) {
	return &szX86{}, len(props) == 0
}

// szX86MSByte reports whether b is the high byte of a near offset: 0x00 or
// 0xFF. It is Test86MSByte of Bra86.c.
func szX86MSByte(b byte) bool {
	return (b+1)&0xFE == 0
}

// convert is x86_Convert of Bra86.c, to decode. A CALL (0xE8) or a JMP
// (0xE9) with a near offset holds an absolute address, which it makes
// relative again. mask holds which of the 3 bytes before were 0xE8 or 0xE9,
// as the encoder kept them.
func (f *szX86) convert(data []byte) int {
	if len(data) < 5 {
		return 0
	}
	size := len(data) - 4
	ip := f.ip + 5
	mask := f.mask & 7
	pos := 0
	for {
		p := pos
		for p < size && data[p]&0xFE != 0xE8 {
			p++
		}
		d := p - pos
		pos = p
		if p >= size {
			if d > 2 {
				f.mask = 0
			} else {
				f.mask = mask >> uint(d)
			}
			f.ip += uint32(pos)
			return pos
		}
		if d > 2 {
			mask = 0
		} else {
			mask >>= uint(d)
			if mask != 0 && (mask > 4 || mask == 3 || szX86MSByte(data[p+int(mask>>1)+1])) {
				mask = mask>>1 | 4
				pos++
				continue
			}
		}
		if !szX86MSByte(data[p+4]) {
			mask = mask>>1 | 4
			pos++
			continue
		}
		v := binary.LittleEndian.Uint32(data[p+1:])
		cur := ip + uint32(pos)
		pos += 5
		v -= cur
		if mask != 0 {
			sh := (mask & 6) << 2
			if szX86MSByte(byte(v >> sh)) {
				v ^= uint32(0x100)<<sh - 1
				v -= cur
			}
			mask = 0
		}
		data[p+1] = byte(v)
		data[p+2] = byte(v >> 8)
		data[p+3] = byte(v >> 16)
		data[p+4] = byte(0 - (v>>24)&1)
	}
}

// szARMConvert is ARM_Convert of Bra.c, to decode: a BL instruction holds
// an absolute address in its 24 low bits, in words.
func szARMConvert(data []byte, ip uint32) int {
	i := 0
	for ; i+4 <= len(data); i += 4 {
		if data[i+3] != 0xEB {
			continue
		}
		src := (uint32(data[i+2])<<16 | uint32(data[i+1])<<8 | uint32(data[i])) << 2
		dest := (src - (ip + uint32(i) + 8)) >> 2
		data[i+2] = byte(dest >> 16)
		data[i+1] = byte(dest >> 8)
		data[i] = byte(dest)
	}
	return i
}

// szARMTConvert is ARMT_Convert of Bra.c, to decode: a BL instruction of
// Thumb, in two halves of 16 bits, holds an absolute address in half
// words.
func szARMTConvert(data []byte, ip uint32) int {
	if len(data) < 4 {
		return 0
	}
	size := len(data) - 4
	ip += 4
	i := 0
	for ; i <= size; i += 2 {
		if data[i+1]&0xF8 != 0xF0 || data[i+3]&0xF8 != 0xF8 {
			continue
		}
		src := (uint32(data[i+1]&7)<<19 | uint32(data[i])<<11 | uint32(data[i+3]&7)<<8 | uint32(data[i+2])) << 1
		dest := (src - (ip + uint32(i))) >> 1
		data[i+1] = byte(0xF0 | (dest>>19)&7)
		data[i] = byte(dest >> 11)
		data[i+3] = byte(0xF8 | (dest>>8)&7)
		data[i+2] = byte(dest)
		i += 2
	}
	return i
}

// szPPCConvert is PPC_Convert of Bra.c, to decode: a branch of PowerPC with
// the bit AA clear and the bit LK set, in big-endian order.
func szPPCConvert(data []byte, ip uint32) int {
	i := 0
	for ; i+4 <= len(data); i += 4 {
		if data[i]>>2 != 0x12 || data[i+3]&3 != 1 {
			continue
		}
		src := uint32(data[i]&3)<<24 | uint32(data[i+1])<<16 | uint32(data[i+2])<<8 | uint32(data[i+3]&^3)
		dest := src - (ip + uint32(i))
		data[i] = byte(0x48 | (dest>>24)&3)
		data[i+1] = byte(dest >> 16)
		data[i+2] = byte(dest >> 8)
		data[i+3] = data[i+3]&3 | byte(dest)&^3
	}
	return i
}

// szSPARCConvert is SPARC_Convert of Bra.c, to decode: a CALL of SPARC
// whose offset fits in 22 bits, in big-endian order.
func szSPARCConvert(data []byte, ip uint32) int {
	i := 0
	for ; i+4 <= len(data); i += 4 {
		if !(data[i] == 0x40 && data[i+1]&0xC0 == 0) && !(data[i] == 0x7F && data[i+1]&0xC0 == 0xC0) {
			continue
		}
		src := binary.BigEndian.Uint32(data[i:]) << 2
		dest := (src - (ip + uint32(i))) >> 2
		dest = ((0-(dest>>22)&1)<<22)&0x3FFFFFFF | dest&0x3FFFFF | 0x40000000
		binary.BigEndian.PutUint32(data[i:], dest)
	}
	return i
}

// szIA64Branch gives, for the template of a bundle of IA-64, the slots
// that can hold a branch. It is kBranchTable of BraIA64.c.
var szIA64Branch = [32]byte{
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	4, 4, 6, 6, 0, 0, 7, 7,
	4, 4, 0, 0, 4, 4, 0, 0,
}

// szIA64Convert is IA64_Convert of BraIA64.c, to decode: a bundle of 16
// bytes holds 3 slots of 41 bits after a template of 5 bits, and a slot
// that holds a branch holds an absolute address in 21 bits.
func szIA64Convert(data []byte, ip uint32) int {
	if len(data) < 16 {
		return 0
	}
	size := len(data) - 16
	i := 0
	for ; i <= size; i += 16 {
		mask := uint32(szIA64Branch[data[i]&0x1F])
		bitPos := uint32(5)
		for slot := range 3 {
			if (mask>>slot)&1 == 0 {
				bitPos += 41
				continue
			}
			bytePos := i + int(bitPos>>3)
			bitRes := bitPos & 7
			bitPos += 41
			var instr uint64
			for j := range 6 {
				instr |= uint64(data[bytePos+j]) << (8 * j)
			}
			norm := instr >> bitRes
			if (norm>>37)&0xF != 0x5 || (norm>>9)&0x7 != 0 {
				continue
			}
			src := uint32((norm >> 13) & 0xFFFFF)
			src |= uint32((norm>>36)&1) << 20
			src <<= 4
			dest := (src - (ip + uint32(i))) >> 4
			norm &^= uint64(0x8FFFFF) << 13
			norm |= uint64(dest&0xFFFFF) << 13
			norm |= uint64(dest&0x100000) << (36 - 20)
			instr &= 1<<bitRes - 1
			instr |= norm << bitRes
			for j := range 6 {
				data[bytePos+j] = byte(instr >> (8 * j))
			}
		}
	}
	return i
}

// szARM64Convert is the decoder of the filter ARM64 of Bra.c, which xz
// uses too: a BL instruction holds an absolute address in words, and an
// ADRP instruction with a small offset holds an absolute address in pages.
func szARM64Convert(data []byte, ip uint32) int {
	i := 0
	for ; i+4 <= len(data); i += 4 {
		pc := ip + uint32(i)
		instr := binary.LittleEndian.Uint32(data[i:])
		switch {
		case instr>>26 == 0x25:
			instr = 0x94000000 | (instr-pc>>2)&0x03FFFFFF
		case instr&0x9F000000 == 0x90000000:
			src := (instr>>29)&3 | (instr>>3)&0x001FFFFC
			if (src+0x00020000)&0x001C0000 != 0 {
				continue
			}
			dest := src - pc>>12
			instr &= 0x9000001F
			instr |= (dest & 3) << 29
			instr |= (dest & 0x0003FFFC) << 3
			instr |= (0 - (dest & 0x00020000)) & 0x00E00000
		default:
			continue
		}
		binary.LittleEndian.PutUint32(data[i:], instr)
	}
	return i
}

// szDelta is the filter Delta: each byte is the sum of its value and the
// byte dist bytes before it in the output. It is Delta_Decode of Delta.c.
type szDelta struct {
	hist [256]byte // the last dist bytes of the output, as a ring
	dist int
	pos  int
}

// newSzDelta makes the filter Delta. The one byte of its properties is the
// distance less one.
func newSzDelta(props []byte) (szConverter, bool) {
	if len(props) != 1 {
		return nil, false
	}
	return &szDelta{dist: int(props[0]) + 1}, true
}

func (f *szDelta) convert(data []byte) int {
	for i := range data {
		data[i] += f.hist[f.pos]
		f.hist[f.pos] = data[i]
		f.pos++
		if f.pos == f.dist {
			f.pos = 0
		}
	}
	return len(data)
}
