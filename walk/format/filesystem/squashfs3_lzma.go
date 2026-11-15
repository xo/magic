//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package filesystem

import (
	"encoding/binary"
	"errors"
)

// This file decodes LZMA as the decoders that sasquatch links do. Each
// decoder reads the same stream, but each ends it in its own way, and a
// vendor image of SquashFS works with one of them only. lzmaDec copies the
// behavior of the three decoders, not their code:
//
//   - LzmaDec.c of the LZMA SDK 4.65, which lzma_standard_uncompress of
//     lzma_wrapper.c calls. The data must hold every byte that it decodes.
//   - LZMADecoder.cpp of 7-Zip 4.x, in LZMA/lzmadaptive, which
//     lzmaspec_uncompress and lzmawrt_uncompress call. At the end of the
//     data, it reads bytes of 0xFF.
//   - LZMADecoder.c of Broadcom, in LZMA/lzmalt, which decompress_lzma_alt
//     calls. At the end of the data, it reads bytes of 0, it knows no end
//     marker, and it can write past the end of its buffer.

// errLZMA is the error of LZMA data that a decoder rejects.
var errLZMA = errors.New("squashfs: the LZMA data is not valid")

// The decoders that sasquatch links.
const (
	lzmaSDK = iota
	lzmaCPP
	lzmaBRCM
)

// The constants of LZMA.
const (
	lzmaStates      = 12
	lzmaPosStates   = 1 << 4
	lzmaEndPosModel = 14
	lzmaFullDists   = 1 << (lzmaEndPosModel >> 1)
	lzmaAlignBits   = 4
	lzmaTop         = 1 << 24
)

// lzmaLen is the decoder of a length.
type lzmaLen struct {
	choice, choice2 uint16
	low, mid        [lzmaPosStates][1 << 3]uint16
	high            [1 << 8]uint16
}

// lzmaDec decodes one raw LZMA stream, with no header.
type lzmaDec struct {
	kind       int
	lc, lp, pb uint
	dict       uint32

	src       []byte
	pos       int
	short     bool // the SDK needed a byte past the end of the data
	rng, code uint32

	lit                              []uint16
	isMatch, isRep0Long              [lzmaStates][lzmaPosStates]uint16
	isRep, isRepG0, isRepG1, isRepG2 [lzmaStates]uint16
	posSlot                          [4][1 << 6]uint16
	posSpec                          [1 + lzmaFullDists - lzmaEndPosModel]uint16
	align                            [1 << lzmaAlignBits]uint16
	lenDec, repLen                   lzmaLen
	out                              []byte
}

// reset prepares d to decode src with the properties lc, lp and pb.
func (d *lzmaDec) reset(kind int, lc, lp, pb uint, dict uint32, src []byte) {
	d.kind, d.lc, d.lp, d.pb, d.dict = kind, lc, lp, pb, dict
	d.src, d.pos, d.short = src, 0, false
	n := 0x300 << (lc + lp)
	if cap(d.lit) < n {
		d.lit = make([]uint16, n)
	}
	d.lit = d.lit[:n]
	for i := range d.lit {
		d.lit[i] = 1024
	}
	for i := range d.isMatch {
		for j := range d.isMatch[i] {
			d.isMatch[i][j], d.isRep0Long[i][j] = 1024, 1024
		}
		d.isRep[i], d.isRepG0[i], d.isRepG1[i], d.isRepG2[i] = 1024, 1024, 1024, 1024
	}
	for i := range d.posSlot {
		for j := range d.posSlot[i] {
			d.posSlot[i][j] = 1024
		}
	}
	for i := range d.posSpec {
		d.posSpec[i] = 1024
	}
	for i := range d.align {
		d.align[i] = 1024
	}
	for _, l := range []*lzmaLen{&d.lenDec, &d.repLen} {
		l.choice, l.choice2 = 1024, 1024
		for i := range l.low {
			for j := range l.low[i] {
				l.low[i][j], l.mid[i][j] = 1024, 1024
			}
		}
		for i := range l.high {
			l.high[i] = 1024
		}
	}
	d.out = d.out[:0]
	d.rng, d.code = 0xFFFFFFFF, 0
	for range 5 {
		d.code = d.code<<8 | uint32(d.next())
	}
}

// next returns the next byte of the data. Past the end, the SDK stops, and
// the other decoders read a fixed byte.
func (d *lzmaDec) next() byte {
	if d.pos < len(d.src) {
		d.pos++
		return d.src[d.pos-1]
	}
	switch d.kind {
	case lzmaSDK:
		d.short = true
	case lzmaCPP:
		return 0xFF
	}
	return 0
}

// norm takes a byte into the range coder when the range is small. The SDK
// does it before it decodes a bit, and the older decoders after.
func (d *lzmaDec) norm() {
	if d.rng < lzmaTop {
		d.rng <<= 8
		d.code = d.code<<8 | uint32(d.next())
	}
}

func (d *lzmaDec) bit(p *uint16) uint32 {
	if d.kind == lzmaSDK {
		d.norm()
	}
	bound := (d.rng >> 11) * uint32(*p)
	var b uint32
	if d.code < bound {
		d.rng = bound
		*p += (2048 - *p) >> 5
	} else {
		d.rng -= bound
		d.code -= bound
		*p -= *p >> 5
		b = 1
	}
	if d.kind != lzmaSDK {
		d.norm()
	}
	return b
}

func (d *lzmaDec) direct(n uint) uint32 {
	var r uint32
	for range n {
		if d.kind == lzmaSDK {
			d.norm()
		}
		d.rng >>= 1
		var b uint32
		if d.code >= d.rng {
			d.code -= d.rng
			b = 1
		}
		r = r<<1 | b
		if d.kind != lzmaSDK {
			d.norm()
		}
	}
	return r
}

func (d *lzmaDec) tree(p []uint16, bits uint) uint32 {
	m := uint32(1)
	for range bits {
		m = m<<1 | d.bit(&p[m])
	}
	return m - 1<<bits
}

func (d *lzmaDec) reverse(p []uint16, bits uint) uint32 {
	m, r := uint32(1), uint32(0)
	for i := range bits {
		b := d.bit(&p[m])
		m = m<<1 | b
		r |= b << i
	}
	return r
}

func (d *lzmaDec) length(l *lzmaLen, posState int) int {
	if d.bit(&l.choice) == 0 {
		return 2 + int(d.tree(l.low[posState][:], 3))
	}
	if d.bit(&l.choice2) == 0 {
		return 10 + int(d.tree(l.mid[posState][:], 3))
	}
	return 18 + int(d.tree(l.high[:], 8))
}

// decode decodes until it writes limit bytes or finds the end marker. The
// SDK and 7-Zip stop inside a match at limit. The decoder of Broadcom copies
// the whole match, and so can pass limit.
func (d *lzmaDec) decode(limit int) error {
	var state, rep0, rep1, rep2, rep3 uint32
	pbMask, lpMask := 1<<d.pb-1, 1<<d.lp-1
	for len(d.out) < limit {
		if d.short {
			return errLZMA
		}
		pos := len(d.out)
		posState := pos & pbMask
		if d.bit(&d.isMatch[state][posState]) == 0 {
			var prev byte
			if pos > 0 {
				prev = d.out[pos-1]
			}
			probs := d.lit[0x300*(((pos&lpMask)<<d.lc)+int(prev>>(8-d.lc))):]
			sym := uint32(1)
			if state >= 7 {
				if int(rep0) >= pos {
					return errLZMA
				}
				match := uint32(d.out[pos-int(rep0)-1])
				for sym < 0x100 {
					mb := (match >> 7) & 1
					match <<= 1
					b := d.bit(&probs[(1+mb)<<8+sym])
					sym = sym<<1 | b
					if b != mb {
						break
					}
				}
			}
			for sym < 0x100 {
				sym = sym<<1 | d.bit(&probs[sym])
			}
			d.out = append(d.out, byte(sym))
			switch {
			case state < 4:
				state = 0
			case state < 10:
				state -= 3
			default:
				state -= 6
			}
			continue
		}
		var n int
		if d.bit(&d.isRep[state]) == 0 {
			n = d.length(&d.lenDec, posState)
			state = lzmaNext(state, 7, 10)
			dist := d.distance(n)
			if dist == 0xFFFFFFFF && d.kind != lzmaBRCM {
				if d.short {
					return errLZMA
				}
				return nil // the end marker
			}
			if int64(dist) >= int64(pos) || (d.kind != lzmaBRCM && dist >= d.dict) {
				return errLZMA
			}
			rep3, rep2, rep1, rep0 = rep2, rep1, rep0, dist
		} else {
			if pos == 0 {
				return errLZMA
			}
			if d.bit(&d.isRepG0[state]) == 0 {
				if d.bit(&d.isRep0Long[state][posState]) == 0 {
					state = lzmaNext(state, 9, 11)
					if int(rep0) >= pos {
						return errLZMA
					}
					d.out = append(d.out, d.out[pos-int(rep0)-1])
					continue
				}
			} else {
				var dist uint32
				if d.bit(&d.isRepG1[state]) == 0 {
					dist = rep1
				} else {
					if d.bit(&d.isRepG2[state]) == 0 {
						dist = rep2
					} else {
						dist, rep3 = rep3, rep2
					}
					rep2 = rep1
				}
				rep1, rep0 = rep0, dist
			}
			n = d.length(&d.repLen, posState)
			state = lzmaNext(state, 8, 11)
			if int(rep0) >= pos {
				return errLZMA
			}
		}
		if d.kind != lzmaBRCM {
			n = min(n, limit-pos)
		}
		for range n {
			d.out = append(d.out, d.out[len(d.out)-int(rep0)-1])
		}
	}
	if d.short {
		return errLZMA
	}
	return nil
}

// distance decodes the distance of a match of length n.
func (d *lzmaDec) distance(n int) uint32 {
	slot := d.tree(d.posSlot[min(n-2, 3)][:], 6)
	if slot < 4 {
		return slot
	}
	bits := uint(slot>>1) - 1
	dist := (2 | slot&1) << bits
	if slot < lzmaEndPosModel {
		return dist + d.reverse(d.posSpec[dist-slot:], bits)
	}
	dist += d.direct(bits-lzmaAlignBits) << lzmaAlignBits
	return dist + d.reverse(d.align[:], lzmaAlignBits)
}

// lzmaNext is the next state after a match: a when the state is one of a
// literal, and b otherwise.
func lzmaNext(state, a, b uint32) uint32 {
	if state < 7 {
		return a
	}
	return b
}

// lzmaProps splits the byte of the properties into lc, lp and pb.
func lzmaProps(b byte) (lc, lp, pb uint) {
	return uint(b) % 9, uint(b) / 9 % 5, uint(b) / 45
}

// lzmaStandard is lzma_standard_uncompress: the 5 bytes of the properties,
// the size in 64 bits, and the stream. When the size is larger than the
// buffer, the stream follows the properties, with no size.
func (d *lzmaDec) lzmaStandard(src []byte, outsize int) ([]byte, bool) {
	if len(src) < 13 || src[0] >= 9*5*5 {
		return nil, false
	}
	size := int64(binary.LittleEndian.Uint32(src[5:]))
	stream := src[13:]
	if size > int64(outsize) {
		size, stream = int64(outsize), src[5:]
	}
	if len(stream) == 0 || stream[0] != 0 {
		return nil, false
	}
	lc, lp, pb := lzmaProps(src[0])
	d.reset(lzmaSDK, lc, lp, pb, max(binary.LittleEndian.Uint32(src[1:]), 4096), stream)
	if d.decode(int(size)) != nil {
		return nil, false
	}
	return d.out, true
}

// The sizes that lzma_adaptive_uncompress accepts for a buffer it does not
// fill: SQUASHFS_METADATA_SIZE and SQUASHFS_FILE_SIZE.
const (
	sqMetadataSize = 8192
	sqFileSize     = 131072
)

// lzmaSpec is lzmaspec_uncompress: the stream at offset, with the given
// properties and a dictionary of 8 MiB.
func (d *lzmaDec) lzmaSpec(src []byte, limit int, lc, lp, pb uint, offset int) bool {
	if offset > len(src) {
		return false
	}
	d.reset(lzmaCPP, lc, lp, pb, 1<<23, src[offset:])
	return d.decode(limit) == nil
}

// lzmaAdaptive is lzma_adaptive_uncompress. It tries each lc, lp and pb up
// to 4, at each offset up to 10, and takes the first stream that gives no
// more than outsize bytes: outsize itself, or any size when outsize is one
// of the two sizes above. It keeps the properties for the next block.
func (s *sqfs3) lzmaAdaptive(src []byte, outsize int) ([]byte, bool) {
	d := &s.lzma
	if p := s.adaptive; p.found {
		if d.lzmaSpec(src, outsize, p.lc, p.lp, p.pb, p.offset) {
			return d.out, true
		}
	}
	for offset := 0; offset <= 10; offset++ {
		for lc := range uint(5) {
			for lp := range uint(5) {
				for pb := range uint(5) {
					// The decoder of sasquatch writes up to twice outsize,
					// and rejects more than outsize. The walker stops at one
					// byte more, which it rejects the same.
					if !d.lzmaSpec(src, outsize+1, lc, lp, pb, offset) {
						continue
					}
					n := len(d.out)
					if outsize >= n && (outsize == sqMetadataSize || outsize == sqFileSize || outsize == n) {
						s.adaptive = lzmaParams{found: true, lc: lc, lp: lp, pb: pb, offset: offset}
						return d.out, true
					}
				}
			}
		}
	}
	return nil, false
}

// lzmaParams are the properties that lzmaAdaptive found.
type lzmaParams struct {
	found      bool
	lc, lp, pb uint
	offset     int
}

// lzmaAlt is lzma_alt_uncompress: the properties, a dictionary of 32 bits,
// and the stream, at the offset 0 or 4. It takes a stream that fills the
// buffer exactly, with no byte of the data left. When bytes are left, the
// decoder of Broadcom loops forever, and the walker rejects the stream.
func (d *lzmaDec) lzmaAlt(src []byte, outsize int) ([]byte, bool) {
	for _, offset := range []int{0, 4} {
		if len(src) < offset+5 {
			continue
		}
		lc, lp, pb := lzmaProps(src[offset])
		if lc+lp+pb == 0 || lc > 4 || lp > 4 || pb > 4 || lc+lp > 4 {
			continue
		}
		d.reset(lzmaBRCM, lc, lp, pb, 0, src[offset+5:])
		if d.pos == len(d.src) {
			continue // the loop of the decoder does not start
		}
		if d.decode(outsize) == nil && len(d.out) == outsize && d.pos == len(d.src) {
			return d.out, true
		}
	}
	return nil, false
}

// lzmaWRT is lzma_wrt_uncompress of DD-WRT: pb, lc and lp in the first three
// bytes, and the stream after four.
func (d *lzmaDec) lzmaWRT(src []byte, outsize int) ([]byte, bool) {
	if len(src) < 4 || src[0] > 4 || src[1] > 4 || src[2] > 4 || src[1]+src[2] > 4 {
		return nil, false
	}
	d.reset(lzmaCPP, uint(src[1]), uint(src[2]), uint(src[0]), 1<<23, src[4:])
	if d.decode(outsize) != nil {
		return nil, false
	}
	return d.out, true
}
