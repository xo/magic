//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// szDecodeBCJ2 copies the behavior of Bcj2_Decode of Bcj2.c of 7-Zip by Igor
// Pavlov, which is in the public domain.

package archive

import (
	"encoding/binary"
	"fmt"
	"io"
)

// The constants of the range coder of BCJ2, as in LZMA.
const (
	szBCJ2TopValue  = 1 << 24
	szBCJ2ProbBits  = 11
	szBCJ2ProbTotal = 1 << szBCJ2ProbBits
	szBCJ2MoveBits  = 5
)

// szBCJ2Range is the range decoder of BCJ2.
type szBCJ2Range struct {
	in        []byte
	rng, code uint32
	bad       bool
	probs     [256 + 2]uint16
}

func (r *szBCJ2Range) init() {
	r.rng = 0xFFFFFFFF
	for i := range r.probs {
		r.probs[i] = szBCJ2ProbTotal >> 1
	}
	for range 5 {
		r.code = r.code<<8 | uint32(r.byte())
	}
}

func (r *szBCJ2Range) byte() byte {
	if len(r.in) == 0 {
		r.bad = true
		return 0
	}
	b := r.in[0]
	r.in = r.in[1:]
	return b
}

// bit decodes one bit with the probability p.
func (r *szBCJ2Range) bit(p *uint16) bool {
	if r.rng < szBCJ2TopValue {
		r.rng <<= 8
		r.code = r.code<<8 | uint32(r.byte())
	}
	bound := (r.rng >> szBCJ2ProbBits) * uint32(*p)
	if r.code < bound {
		r.rng = bound
		*p += (szBCJ2ProbTotal - *p) >> szBCJ2MoveBits
		return false
	}
	r.rng -= bound
	r.code -= bound
	*p -= *p >> szBCJ2MoveBits
	return true
}

// szBCJ2Jump reports whether b1 after b0 is an opcode that BCJ2 converts:
// a CALL (0xE8), a JMP (0xE9), or a conditional jump (0x0F 0x8x). It is
// IsJ of Bcj2.c.
func szBCJ2Jump(b0, b1 byte) bool {
	return b1&0xFE == 0xE8 || b0 == 0x0F && b1&0xF0 == 0x80
}

// szDecodeBCJ2 is the method BCJ2, Bcj2_Decode of Bcj2.c. Its four input
// streams are the main stream, the addresses of the calls, the addresses
// of the jumps, and the stream of the range coder that says which opcodes
// the encoder converted. An address is absolute and big-endian, and the
// decoder makes it relative again.
func szDecodeBCJ2(_ *szCoder, in [][]byte, size int, w io.Writer) error {
	if len(in) != 4 {
		return errSevenZip
	}
	main, call, jump := in[0], in[1], in[2]
	r := &szBCJ2Range{in: in[3]}
	r.init()
	if r.bad {
		return errSevenZip
	}
	const chunk = 64 << 10
	out := make([]byte, 0, min(size, chunk)+4)
	written := 0 // the bytes before out
	flush := func() error {
		if _, err := w.Write(out); err != nil {
			return fmt.Errorf("writing the output of BCJ2: %w", err)
		}
		written += len(out)
		out = out[:0]
		return nil
	}
	var prev byte
	for written+len(out) < size {
		if len(out) >= chunk {
			if err := flush(); err != nil {
				return err
			}
		}
		if len(main) == 0 {
			break
		}
		b := main[0]
		main = main[1:]
		out = append(out, b)
		if !szBCJ2Jump(prev, b) {
			prev = b
			continue
		}
		if written+len(out) == size {
			break
		}
		var p *uint16
		switch b {
		case 0xE8:
			p = &r.probs[prev]
		case 0xE9:
			p = &r.probs[256]
		default:
			p = &r.probs[257]
		}
		if !r.bit(p) {
			prev = b
			if r.bad {
				break
			}
			continue
		}
		if r.bad {
			break
		}
		var v []byte
		if b == 0xE8 {
			if len(call) < 4 {
				break
			}
			v, call = call[:4], call[4:]
		} else {
			if len(jump) < 4 {
				break
			}
			v, jump = jump[:4], jump[4:]
		}
		dest := binary.BigEndian.Uint32(v) - uint32(written+len(out)+4)
		var d [4]byte
		binary.LittleEndian.PutUint32(d[:], dest)
		n := min(4, size-written-len(out))
		out = append(out, d[:n]...)
		prev = d[3]
	}
	if err := flush(); err != nil {
		return err
	}
	if written != size {
		return errSevenZip
	}
	return nil
}
