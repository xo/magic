// Copyright (c) 2015-2016, Apple Inc. All rights reserved. Under the BSD
// 3-clause license, in LICENSE.
//
// This file translates src/lzvn_decode_base.c of lzfse, and the part of
// lzfse_decode in src/lzfse_decode_base.c that reads an LZVN block, at the
// commit that docs/upstream/upstreams.tsv names (D63).

package lzfse

import "fmt"

// lzvn decodes the payload of an LZVN block, which decodes to raw bytes. It
// is lzvn_decode, for a payload that is all in memory. Each opcode holds a
// literal of up to 3 bytes and a match, only a literal, or only a match.
// The payload ends with the opcode of the end and 7 bytes of padding.
//
// A match can reach back into the blocks before it, to the start of the
// stream. The distance of the last match carries over to the next opcode,
// and starts at 0 in each block, which no match accepts.
func (d *decoder) lzvn(src []byte, raw int64) error {
	// lzfse fails a block of no bytes, because lzvn_decode returns before it
	// reads the end.
	if raw == 0 {
		return fmt.Errorf("%w: the block decodes to no bytes", ErrCorrupt)
	}
	dist := 0
	for p := 0; ; {
		if p >= len(src) {
			return fmt.Errorf("%w: the block ends at byte %d with no opcode of the end", ErrCorrupt, p)
		}
		left := len(src) - p
		opc := int(src[p])
		// n is the length of the opcode, l the length of its literal, and m
		// the length of its match.
		var n, l, m int
		switch {
		case opc >= 0xF0:
			// A match with the distance of the match before it: 1111MMMM, or
			// 11110000 MMMMMMMM for 16 to 271 bytes.
			n, m = 1, opc&0xF
			if opc == 0xF0 {
				n = 2
			}
			if left <= n {
				return fmt.Errorf("%w: the opcode at byte %d is cut", ErrCorrupt, p)
			}
			if opc == 0xF0 {
				m = int(src[p+1]) + 16
			}
			p += n
			if err := d.lzvnMatch(dist, m, &raw); err != nil {
				return fmt.Errorf("the opcode at byte %d: %w", p-n, err)
			}
			continue
		case opc >= 0xE0:
			// A literal: 1110LLLL, or 11100000 LLLLLLLL for 16 to 271 bytes.
			n, l = 1, opc&0xF
			if opc == 0xE0 {
				n = 2
				if left <= n {
					return fmt.Errorf("%w: the opcode at byte %d is cut", ErrCorrupt, p)
				}
				l = int(src[p+1]) + 16
			}
			if left <= n+l {
				return fmt.Errorf("%w: the opcode at byte %d is cut", ErrCorrupt, p)
			}
			if err := d.lzvnLiteral(src[p+n:p+n+l], &raw); err != nil {
				return fmt.Errorf("the opcode at byte %d: %w", p, err)
			}
			p += n + l
			continue
		case opc >= 0xD0, opc >= 0x70 && opc < 0x80:
			return fmt.Errorf("%w: the opcode %#02x at byte %d is not defined", ErrCorrupt, opc, p)
		case opc >= 0xA0 && opc < 0xC0:
			// A medium distance: 101LLMMM DDDDDDMM DDDDDDDD.
			n, l = 3, (opc>>3)&3
			if left <= n+l {
				return fmt.Errorf("%w: the opcode at byte %d is cut", ErrCorrupt, p)
			}
			v := int(le.Uint16(src[p+1:]))
			m = ((opc&7)<<2 | v&3) + 3
			dist = v >> 2
		case opc&7 == 7:
			// A large distance: LLMMM111 DDDDDDDD DDDDDDDD.
			n, l, m = 3, opc>>6, (opc>>3&7)+3
			if left <= n+l {
				return fmt.Errorf("%w: the opcode at byte %d is cut", ErrCorrupt, p)
			}
			dist = int(le.Uint16(src[p+1:]))
		case opc == 6:
			// The end: the opcode and 7 bytes, the last bytes of the block.
			if left < 8 {
				return fmt.Errorf("%w: the opcode of the end at byte %d is cut", ErrCorrupt, p)
			}
			if left > 8 || raw != 0 {
				return fmt.Errorf("%w: the block ends at byte %d with %d bytes still to decode", ErrCorrupt, p, raw)
			}
			return nil
		case opc == 14 || opc == 22:
			// No operation.
			if left <= 1 {
				return fmt.Errorf("%w: the opcode at byte %d is cut", ErrCorrupt, p)
			}
			p++
			continue
		case opc&7 == 6 && opc < 0x40:
			return fmt.Errorf("%w: the opcode %#02x at byte %d is not defined", ErrCorrupt, opc, p)
		case opc&7 == 6:
			// The distance of the match before it: LLMMM110.
			n, l, m = 1, opc>>6, (opc>>3&7)+3
			if left <= n+l {
				return fmt.Errorf("%w: the opcode at byte %d is cut", ErrCorrupt, p)
			}
		default:
			// A small distance: LLMMMDDD DDDDDDDD.
			n, l, m = 2, opc>>6, (opc>>3&7)+3
			if left <= n+l {
				return fmt.Errorf("%w: the opcode at byte %d is cut", ErrCorrupt, p)
			}
			dist = (opc&7)<<8 | int(src[p+1])
		}
		if err := d.lzvnLiteral(src[p+n:p+n+l], &raw); err != nil {
			return fmt.Errorf("the opcode at byte %d: %w", p, err)
		}
		if err := d.lzvnMatch(dist, m, &raw); err != nil {
			return fmt.Errorf("the opcode at byte %d: %w", p, err)
		}
		p += n + l
	}
}

// lzvnLiteral appends the literal b of an LZVN block, which has *raw bytes
// left to decode.
func (d *decoder) lzvnLiteral(b []byte, raw *int64) error {
	if int64(len(b)) > *raw {
		return fmt.Errorf("%w: a literal of %d bytes, and the block has %d left", ErrCorrupt, len(b), *raw)
	}
	*raw -= int64(len(b))
	return d.literal(b)
}

// lzvnMatch appends a match of n bytes from dist bytes back, in an LZVN
// block that has *raw bytes left to decode.
func (d *decoder) lzvnMatch(dist, n int, raw *int64) error {
	if dist == 0 || dist > d.produced() {
		return fmt.Errorf("%w: a match from %d bytes back, and the output has %d", ErrCorrupt, dist, d.produced())
	}
	if int64(n) > *raw {
		return fmt.Errorf("%w: a match of %d bytes, and the block has %d left", ErrCorrupt, n, *raw)
	}
	*raw -= int64(n)
	return d.match(dist, n)
}
