//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/compressd.rs of binwalk-ng. binwalk-ng
// extracts the data of compress(1) with 7z, from src/formats/sevenzip.rs,
// and extractCompressd writes what 7z writes.

package compressed

import (
	"fmt"

	"github.com/xo/magic/walk"
)

// Compressd is the signature "compressd" of binwalk-ng: the LZW data of the
// program compress, a .Z file.
var Compressd = &walk.Signature{
	Name:        "compressd",
	Short:       true,
	Magic:       [][]byte{[]byte("\x1F\x9D\x90")},
	Description: "compress'd data",
	Parse:       parseCompressd,
	Extract:     extractCompressd,
	// binwalk gives 7-Zip the bytes of the match, which has no size of its
	// own: up to the next match, or to the end of the input.
	Carve: true,
}

// parseCompressd is compressd_parser. It reads only the magic, and does
// not know the size.
func parseCompressd(_ []byte, off int) (walk.Match, error) {
	m := walk.Match{Offset: off, Confidence: walk.Low, Description: "compress'd data"}
	if off == 0 {
		m.Confidence = walk.Medium
	}
	return m, nil
}

// The constants of ZDecoder.cpp of 7-Zip.
const (
	lzwMinBits    = 9
	lzwMaxBits    = 16
	lzwBlockMode  = 0x80
	lzwCheckBytes = 64 // the bytes that 7zz checks to open the file
)

// 7-Zip 26.06 opens a .Z file whose first bad code starts at byte 64 or
// later, and refuses one whose bad code starts before it. A damaged copy of
// the corpus showed this on 2026-11-20, and a byte changed at each offset
// from 56 to 66 of compressd-1.bin gave the limit.

// extractCompressd writes what 7zz writes for a .Z file: its data, in a file
// named after the signature and the offset. 7zz opens the file only when
// lzwCheck accepts its first bytes. 7zz keeps the data before an error,
// and binwalk accepts the exit code of that error.
//
// binwalk carves the match up to the next match of medium or high
// confidence, or to the end of the file. The extractor does not get that
// size, so it decodes to the end of data, and the file can hold more bytes
// than the file of 7zz.
func extractCompressd(data []byte, off int, out *walk.Output) walk.Extraction {
	in := data[off:]
	if !lzwCheck(in[:min(len(in), lzwCheckBytes)]) {
		return walk.Extraction{}
	}
	w := &appender{out: out, name: fmt.Sprintf("compressd_%X", off)}
	if !lzwDecode(in, w) {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true}
}

// lzwHeader reads the header of a .Z file: the number of bits of the
// largest code, and the block mode.
func lzwHeader(in []byte) (maxBits uint, block, ok bool) {
	if len(in) < 3 || in[0] != 0x1F || in[1] != 0x9D || in[2]&0x60 != 0 {
		return 0, false, false
	}
	maxBits = uint(in[2] & 0x1F)
	return maxBits, in[2]&lzwBlockMode != 0, maxBits >= lzwMinBits && maxBits <= lzwMaxBits
}

// lzwCodes reads the codes of a .Z file, as compress writes them: in
// groups of as many bytes as the bits of a code, so that a group holds 8
// codes. When the size of a code changes, the rest of the group is
// skipped.
type lzwCodes struct {
	in      []byte
	buf     [lzwMaxBits + 3]byte
	bits    uint // the size of a code
	bitPos  uint
	bufBits uint
}

// next returns the next code, or false at the end of the data.
func (c *lzwCodes) next() (uint32, bool) {
	if c.bufBits == c.bitPos {
		n := copy(c.buf[:c.bits], c.in)
		c.in = c.in[n:]
		c.bufBits = uint(n) * 8
		c.bitPos = 0
	}
	p := c.bitPos >> 3
	code := uint32(c.buf[p]) | uint32(c.buf[p+1])<<8 | uint32(c.buf[p+2])<<16
	code = code >> (c.bitPos & 7) & (1<<c.bits - 1)
	c.bitPos += c.bits
	return code, c.bitPos <= c.bufBits
}

// skip drops the rest of the group, and sets the size of a code.
func (c *lzwCodes) skip(bits uint) {
	c.bufBits, c.bitPos, c.bits = 0, 0, bits
}

// lzwCheck is CheckStream of ZDecoder.cpp: it reads the codes of in, and
// fails at a code that is not in the table.
func lzwCheck(in []byte) bool {
	maxBits, block, ok := lzwHeader(in)
	if !ok {
		return false
	}
	c := lzwCodes{in: in[3:], bits: lzwMinBits}
	head := uint32(256)
	if block {
		head = 257
	}
	for {
		code, ok := c.next()
		if !ok {
			return true
		}
		if code >= head {
			return false
		}
		if block && code == 256 {
			c.skip(lzwMinBits)
			head = 257
			continue
		}
		if head < 1<<maxBits {
			head++
			if head > 1<<c.bits && c.bits < maxBits {
				c.skip(c.bits + 1)
			}
		}
	}
}

// lzwDecode is CodeReal of ZDecoder.cpp. It writes the data of in to w,
// and stops at the end of in, or at a code that is not in the table. It
// fails only when w fails.
func lzwDecode(in []byte, w *appender) bool {
	maxBits, block, _ := lzwHeader(in)
	c := lzwCodes{in: in[3:], bits: lzwMinBits}
	parents := make([]uint16, 1<<maxBits)
	suffixes := make([]byte, 1<<maxBits)
	stack := make([]byte, 1<<maxBits)
	head := uint32(256)
	if block {
		head = 257
	}
	needPrev := false
	var buf []byte
	for {
		code, ok := c.next()
		if !ok || code >= head {
			break
		}
		if block && code == 256 {
			c.skip(lzwMinBits)
			head = 257
			needPrev = false
			continue
		}
		cur, i := code, 0
		for cur >= 256 {
			stack[i] = suffixes[cur]
			i++
			cur = uint32(parents[cur])
		}
		stack[i] = byte(cur)
		i++
		if needPrev {
			suffixes[head-1] = byte(cur)
			if code == head-1 {
				stack[0] = byte(cur)
			}
		}
		for i > 0 {
			i--
			buf = append(buf, stack[i])
		}
		if head < 1<<maxBits {
			needPrev = true
			parents[head] = uint16(code)
			head++
			if head > 1<<c.bits && c.bits < maxBits {
				c.skip(c.bits + 1)
			}
		} else {
			needPrev = false
		}
		if len(buf) >= 64<<10 {
			if _, err := w.Write(buf); err != nil {
				return false
			}
			buf = buf[:0]
		}
	}
	_, err := w.Write(buf)
	return err == nil
}
