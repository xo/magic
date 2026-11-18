// Copyright (c) 2015-2016, Apple Inc. All rights reserved. Under the BSD
// 3-clause license, in LICENSE.
//
// This file translates src/lzfse_decode_base.c, src/lzfse_decode.c and the
// parts of src/lzfse_internal.h that decode, of lzfse, at the commit that
// docs/upstream/upstreams.tsv names (D63). fse.go and lzvn.go translate the
// other files that decode. The port differs from lzfse in these places:
//
//   - Decode decodes a whole stream in one call, and stops at a limit of
//     output. lzfse can stop when its buffer is full, and go on later.
//   - It reads the bits of FSE as lzfse does on a 64-bit machine. On a 32-bit
//     machine, lzfse reads 4 bytes at a time, and it can stop at another
//     place in a damaged stream.
//   - A match of LZVN that takes the distance of the match before it, opcodes
//     0xF0 to 0xFF, must reach no further back than the start of the output.
//     lzfse does not check the distance of these opcodes.
//   - No read goes past the end of the input, and an error does not panic.

// Package lzfse decodes LZFSE, the compression of Apple. DMG files and other
// formats of Apple use it.
//
// A stream of LZFSE is a list of blocks, and a block of the end closes it. A
// block holds bytes that are not compressed, or bytes that LZVN compresses,
// or bytes that LZFSE itself compresses: an LZ77 encoding whose literals and
// matches Finite State Entropy codes.
//
// LZFSE is not a format of libmagic, so the package does not register with
// github.com/xo/magic/compress (D63). It is a port of the decoder of lzfse,
// the reference code of Apple.
package lzfse

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// The errors of Decode. Decode wraps them with the place of the fault.
var (
	// ErrCorrupt reports data that LZFSE does not allow, such as a match
	// from before the start of the output, or a stream with no block of the
	// end.
	ErrCorrupt = errors.New("lzfse: corrupt data")
	// ErrLimit reports a stream that decodes to more bytes than the limit.
	ErrLimit = errors.New("lzfse: data over the limit")
)

// The magic numbers that start each block.
const (
	magicEnd          = 0x24787662 // bvx$, the end of the stream
	magicUncompressed = 0x2d787662 // bvx-, bytes that are not compressed
	magicV1           = 0x31787662 // bvx1, LZFSE with tables that are not compressed
	magicV2           = 0x32787662 // bvx2, LZFSE with compressed tables
	magicLZVN         = 0x6e787662 // bvxn, LZVN
)

// The numbers of symbols and states of the four streams of FSE, and the
// limits of a block.
const (
	lSymbols       = 20
	mSymbols       = 20
	dSymbols       = 64
	literalSymbols = 256
	allSymbols     = lSymbols + mSymbols + dSymbols + literalSymbols

	lStates       = 64
	mStates       = 64
	dStates       = 256
	literalStates = 1024

	matchesPerBlock  = 10000
	literalsPerBlock = 4 * matchesPerBlock
)

// The sizes of the headers of the blocks. The header of version 1 is the C
// struct lzfse_compressed_block_header_v1, with the 2 bytes of padding that
// the C compiler adds at its end. The header of version 2 has a size of its
// own, after 32 fixed bytes.
const (
	v1HeaderSize   = 772
	v2FixedSize    = 32
	lzvnHeaderSize = 12
)

// The extra bits and the base values of the symbols of L, the length of a
// literal, M, the length of a match, and D, the distance of a match.
var (
	lExtraBits = [lSymbols]uint8{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 3, 5, 8}
	lBase      = [lSymbols]int32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 20, 28, 60}
	mExtraBits = [mSymbols]uint8{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3, 5, 8, 11}
	mBase      = [mSymbols]int32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 24, 56, 312}
	dExtraBits = [dSymbols]uint8{
		0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3,
		4, 4, 4, 4, 5, 5, 5, 5, 6, 6, 6, 6, 7, 7, 7, 7,
		8, 8, 8, 8, 9, 9, 9, 9, 10, 10, 10, 10, 11, 11, 11, 11,
		12, 12, 12, 12, 13, 13, 13, 13, 14, 14, 14, 14, 15, 15, 15, 15,
	}
	dBase = [dSymbols]int32{
		0, 1, 2, 3, 4, 6, 8, 10, 12, 16,
		20, 24, 28, 36, 44, 52, 60, 76, 92, 108,
		124, 156, 188, 220, 252, 316, 380, 444, 508, 636,
		764, 892, 1020, 1276, 1532, 1788, 2044, 2556, 3068, 3580,
		4092, 5116, 6140, 7164, 8188, 10236, 12284, 14332, 16380, 20476,
		24572, 28668, 32764, 40956, 49148, 57340, 65532, 81916, 98300, 114684,
		131068, 163836, 196604, 229372,
	}
)

var le = binary.LittleEndian

// Decode appends the bytes that the LZFSE stream src decodes to to dst, and
// returns the extended slice. It reads blocks up to the block of the end,
// and ignores the bytes that follow it. It appends at most limit bytes. When
// the stream is longer, it appends the first limit bytes and returns an error
// that wraps ErrLimit. When the stream is damaged, it returns the bytes that
// it decoded before the fault and an error that wraps ErrCorrupt.
//
// A match can reach back to the start of the stream, never into the bytes of
// dst before it.
func Decode(dst, src []byte, limit int) ([]byte, error) {
	d := &decoder{src: src, out: dst, start: len(dst), limit: max(limit, 0)}
	err := d.decode()
	return d.out, err
}

// decoder holds the state of one call of Decode, and the tables of the
// compressed block that it reads.
type decoder struct {
	src   []byte
	out   []byte
	start int // the length of dst, before the first byte of the stream
	limit int

	// literals and the tables keep what earlier blocks left in them, as the
	// state of lzfse does. They start at 0.
	literals [literalsPerBlock + 64]byte
	litTable [literalStates]litEntry
	lTable   [lStates]valueEntry
	mTable   [mStates]valueEntry
	dTable   [dStates]valueEntry
	header   header
}

// header holds the fields of the header of a compressed block, as version 1
// stores them. Version 2 packs the same fields into fewer bytes.
type header struct {
	nLiterals      uint32
	nMatches       uint32
	literalPayload uint32
	lmdPayload     uint32
	literalBits    int32
	literalState   [4]uint16
	lmdBits        int32
	lState         uint16
	mState         uint16
	dState         uint16
	// freq holds the frequencies of L, M, D and the literals, in this order.
	freq [allSymbols]uint16
}

// produced returns the number of bytes that the stream decoded to so far.
func (d *decoder) produced() int {
	return len(d.out) - d.start
}

// room returns the number of bytes that the stream can still append.
func (d *decoder) room() int {
	return d.limit - d.produced()
}

// literal appends b, or the part of it that fits in the limit.
func (d *decoder) literal(b []byte) error {
	if room := d.room(); len(b) > room {
		d.out = append(d.out, b[:room]...)
		return fmt.Errorf("%w: %d bytes", ErrLimit, d.limit)
	}
	d.out = append(d.out, b...)
	return nil
}

// match appends n bytes that start dist bytes back in the output, or the part
// of them that fits in the limit. The copy can overlap the bytes that it
// appends, as LZ77 allows, so it goes by at most dist bytes at a time. The
// caller checks that dist is in the output.
func (d *decoder) match(dist, n int) error {
	room := d.room()
	over := n > room
	n = min(n, room)
	for n > 0 {
		c := min(n, dist)
		from := len(d.out) - dist
		d.out = append(d.out, d.out[from:from+c]...)
		n -= c
	}
	if over {
		return fmt.Errorf("%w: %d bytes", ErrLimit, d.limit)
	}
	return nil
}

// decode is lzfse_decode, for a source that holds the whole stream.
func (d *decoder) decode() error {
	src := d.src
	for p := 0; ; {
		avail := len(src) - p
		if avail < 4 {
			return fmt.Errorf("%w: the stream ends at byte %d with no block of the end", ErrCorrupt, p)
		}
		switch magic := le.Uint32(src[p:]); magic {
		case magicEnd:
			return nil
		case magicUncompressed:
			if avail < 8 {
				return fmt.Errorf("%w: the header of the block at byte %d is cut", ErrCorrupt, p)
			}
			n := le.Uint32(src[p+4:])
			if uint64(n) > uint64(avail-8) {
				return fmt.Errorf("%w: the block at byte %d holds %d bytes, and the stream has %d", ErrCorrupt, p, n, avail-8)
			}
			if err := d.literal(src[p+8 : p+8+int(n)]); err != nil {
				return err
			}
			p += 8 + int(n)
		case magicLZVN:
			if avail < lzvnHeaderSize {
				return fmt.Errorf("%w: the header of the block at byte %d is cut", ErrCorrupt, p)
			}
			raw, n := le.Uint32(src[p+4:]), le.Uint32(src[p+8:])
			if uint64(n) > uint64(avail-lzvnHeaderSize) {
				return fmt.Errorf("%w: the block at byte %d holds %d bytes, and the stream has %d", ErrCorrupt, p, n, avail-lzvnHeaderSize)
			}
			payload := src[p+lzvnHeaderSize : p+lzvnHeaderSize+int(n)]
			if err := d.lzvn(payload, int64(raw)); err != nil {
				return fmt.Errorf("the LZVN block at byte %d: %w", p, err)
			}
			p += lzvnHeaderSize + int(n)
		case magicV1, magicV2:
			n, err := d.compressed(p, magic)
			if err != nil {
				return fmt.Errorf("the block at byte %d: %w", p, err)
			}
			p += n
		default:
			return fmt.Errorf("%w: no block starts with %#08x, at byte %d", ErrCorrupt, magic, p)
		}
	}
}

// compressed decodes the compressed block of version 1 or 2 at byte p of the
// source, and returns its size. It is the part of lzfse_decode that reads such
// a block, with lzfse_decode_lmd.
func (d *decoder) compressed(p int, magic uint32) (int, error) {
	src := d.src
	h := &d.header
	var size int
	if magic == magicV2 {
		n, err := d.readV2(src[p:])
		if err != nil {
			return 0, err
		}
		size = n
	} else {
		if len(src)-p < v1HeaderSize {
			return 0, fmt.Errorf("%w: the header is cut", ErrCorrupt)
		}
		d.readV1(src[p:])
		size = v1HeaderSize
	}
	// lzfse needs the whole block in its source, and so does the port.
	if uint64(size)+uint64(h.literalPayload)+uint64(h.lmdPayload) > uint64(len(src)-p) {
		return 0, fmt.Errorf("%w: the block holds %d bytes, and the stream has %d", ErrCorrupt,
			uint64(size)+uint64(h.literalPayload)+uint64(h.lmdPayload), len(src)-p)
	}
	if err := checkHeader(h); err != nil {
		return 0, err
	}
	litEnd := p + size + int(h.literalPayload)
	lmdEnd := litEnd + int(h.lmdPayload)
	d.buildTables()
	if err := d.decodeLiterals(litEnd); err != nil {
		return 0, err
	}
	if err := d.decodeLMD(litEnd, lmdEnd); err != nil {
		return 0, err
	}
	return lmdEnd - p, nil
}

// readV1 reads the header of version 1 at the start of b, which holds at least
// v1HeaderSize bytes.
func (d *decoder) readV1(b []byte) {
	h := &d.header
	h.nLiterals = le.Uint32(b[12:])
	h.nMatches = le.Uint32(b[16:])
	h.literalPayload = le.Uint32(b[20:])
	h.lmdPayload = le.Uint32(b[24:])
	h.literalBits = int32(le.Uint32(b[28:]))
	for i := range h.literalState {
		h.literalState[i] = le.Uint16(b[32+2*i:])
	}
	h.lmdBits = int32(le.Uint32(b[40:]))
	h.lState = le.Uint16(b[44:])
	h.mState = le.Uint16(b[46:])
	h.dState = le.Uint16(b[48:])
	for i := range h.freq {
		h.freq[i] = le.Uint16(b[50+2*i:])
	}
}

// readV2 reads the header of version 2 at the start of b, and returns its
// size. It is lzfse_decode_v1, which unpacks the header into the fields of
// version 1.
func (d *decoder) readV2(b []byte) (int, error) {
	if len(b) < v2FixedSize {
		return 0, fmt.Errorf("%w: the header is cut", ErrCorrupt)
	}
	h := &d.header
	v0, v1, v2 := le.Uint64(b[8:]), le.Uint64(b[16:]), le.Uint64(b[24:])
	size := uint32(v2)
	if uint64(size) > uint64(len(b)) {
		return 0, fmt.Errorf("%w: the header has %d bytes, and the stream has %d", ErrCorrupt, size, len(b))
	}
	if size < v2FixedSize {
		return 0, fmt.Errorf("%w: the header has %d bytes, fewer than %d", ErrCorrupt, size, v2FixedSize)
	}
	h.nLiterals = field(v0, 0, 20)
	h.literalPayload = field(v0, 20, 20)
	h.nMatches = field(v0, 40, 20)
	h.literalBits = int32(field(v0, 60, 3)) - 7
	for i := range h.literalState {
		h.literalState[i] = uint16(field(v1, uint(10*i), 10))
	}
	h.lmdPayload = field(v1, 40, 20)
	h.lmdBits = int32(field(v1, 60, 3)) - 7
	h.lState = uint16(field(v2, 32, 10))
	h.mState = uint16(field(v2, 42, 10))
	h.dState = uint16(field(v2, 52, 10))
	h.freq = [allSymbols]uint16{}
	tables := b[v2FixedSize:size]
	// A header with no tables leaves every frequency at 0.
	if len(tables) == 0 {
		return int(size), nil
	}
	var acc uint32
	nbits := 0
	i := 0
	for s := range h.freq {
		// Fill the accumulator a byte at a time, up to the end of the
		// header, or until it holds 32 bits.
		for i < len(tables) && nbits+8 <= 32 {
			acc |= uint32(tables[i]) << nbits
			nbits += 8
			i++
		}
		v, n := freqValue(acc)
		if n > nbits {
			return 0, fmt.Errorf("%w: the frequencies run past the end of the header", ErrCorrupt)
		}
		h.freq[s] = v
		acc >>= n
		nbits -= n
	}
	// The tables end at the end of the header, with less than a byte left.
	if nbits >= 8 || i != len(tables) {
		return 0, fmt.Errorf("%w: the frequencies end before the end of the header", ErrCorrupt)
	}
	return int(size), nil
}

// field returns the n bits of v from bit off, with bit 0 the least
// significant. It is get_field.
func field(v uint64, off, n uint) uint32 {
	return uint32(v>>off) & (1<<n - 1)
}

// The tables of lzfse_decode_v1_freq_value: the number of bits of the code
// that the 5 low bits start, and the value of the codes of up to 5 bits.
var (
	freqBits = [32]int8{
		2, 3, 2, 5, 2, 3, 2, 8, 2, 3, 2, 5, 2, 3, 2, 14,
		2, 3, 2, 5, 2, 3, 2, 8, 2, 3, 2, 5, 2, 3, 2, 14,
	}
	freqValues = [32]int8{
		0, 2, 1, 4, 0, 3, 1, -1, 0, 2, 1, 5, 0, 3, 1, -1,
		0, 2, 1, 6, 0, 3, 1, -1, 0, 2, 1, 7, 0, 3, 1, -1,
	}
)

// freqValue decodes one frequency from the low bits of acc, and returns it
// with the number of bits of its code. It is lzfse_decode_v1_freq_value.
func freqValue(acc uint32) (uint16, int) {
	b := acc & 31
	n := int(freqBits[b])
	switch n {
	case 8:
		return uint16(8 + (acc>>4)&0xF), n
	case 14:
		return uint16(24 + (acc>>4)&0x3FF), n
	}
	return uint16(freqValues[b]), n
}

// checkHeader is lzfse_check_block_header_v1: it checks the counts, the
// states and the sums of the frequencies of a header.
func checkHeader(h *header) error {
	switch {
	case h.nLiterals > literalsPerBlock:
		return fmt.Errorf("%w: %d literals, over %d", ErrCorrupt, h.nLiterals, literalsPerBlock)
	case h.nMatches > matchesPerBlock:
		return fmt.Errorf("%w: %d matches, over %d", ErrCorrupt, h.nMatches, matchesPerBlock)
	case h.lState >= lStates || h.mState >= mStates || h.dState >= dStates:
		return fmt.Errorf("%w: a state of L, M or D is out of its table", ErrCorrupt)
	}
	for _, s := range h.literalState {
		if s >= literalStates {
			return fmt.Errorf("%w: a state of the literals is out of its table", ErrCorrupt)
		}
	}
	for _, t := range []struct {
		freq   []uint16
		states int
	}{
		{h.freq[:lSymbols], lStates},
		{h.freq[lSymbols : lSymbols+mSymbols], mStates},
		{h.freq[lSymbols+mSymbols : lSymbols+mSymbols+dSymbols], dStates},
		{h.freq[lSymbols+mSymbols+dSymbols:], literalStates},
	} {
		sum := 0
		for _, f := range t.freq {
			sum += int(f)
		}
		if sum > t.states {
			return fmt.Errorf("%w: the frequencies sum to %d, over %d states", ErrCorrupt, sum, t.states)
		}
	}
	return nil
}

// buildTables makes the four tables of FSE of the block from the frequencies
// of its header. checkHeader already checked the frequencies.
func (d *decoder) buildTables() {
	f := d.header.freq[:]
	initLiteralTable(d.litTable[:], f[lSymbols+mSymbols+dSymbols:])
	initValueTable(d.lTable[:], f[:lSymbols], lExtraBits[:], lBase[:])
	initValueTable(d.mTable[:], f[lSymbols:lSymbols+mSymbols], mExtraBits[:], mBase[:])
	initValueTable(d.dTable[:], f[lSymbols+mSymbols:lSymbols+mSymbols+dSymbols], dExtraBits[:], dBase[:])
}

// decodeLiterals decodes the literals of the block into d.literals. Their
// stream of FSE ends at byte end of the source, and is read backward. As in
// lzfse, its loads can reach back past its own start, into the header and
// the blocks before it. A valid stream uses only the bits that the encoder
// wrote.
func (d *decoder) decodeLiterals(end int) error {
	h := &d.header
	var in bitReader
	pos := end
	if err := in.init(d.src, &pos, 0, h.literalBits); err != nil {
		return err
	}
	s := [4]int{int(h.literalState[0]), int(h.literalState[1]), int(h.literalState[2]), int(h.literalState[3])}
	n := int(h.nLiterals)
	// The count is a multiple of 4 in a valid block. The buffer has room
	// for the 3 bytes past a count that is not.
	for i := 0; i < n; i += 4 {
		if err := in.flush(d.src, &pos, 0); err != nil {
			return err
		}
		for j := range 4 {
			e := d.litTable[s[j]]
			s[j] = int(e.delta) + int(in.pull(uint(e.k)))
			d.literals[i+j] = e.symbol
		}
	}
	return nil
}

// decodeLMD decodes the triplets of L, M and D of the block, and writes their
// literals and matches. The stream of FSE starts at byte start of the source
// and ends at byte end, and is read backward. It is lzfse_decode_lmd. Its
// first load must stay in the stream, but lzfse lets the loads after it reach
// back to the start of the source, as for the literals.
func (d *decoder) decodeLMD(start, end int) error {
	h := &d.header
	var in bitReader
	pos := end
	if err := in.init(d.src, &pos, start, h.lmdBits); err != nil {
		return err
	}
	ls, ms, ds := int(h.lState), int(h.mState), int(h.dState)
	// D starts at a value that no match accepts, so that a first match that
	// takes the distance before it fails.
	dist := -1
	lit := 0
	for range h.nMatches {
		if err := in.flush(d.src, &pos, 0); err != nil {
			return err
		}
		l := int(d.lTable[ls].decode(&ls, &in))
		// lzfse checks the literal against its buffer, not against the
		// count of the header.
		if lit+l >= len(d.literals) {
			return fmt.Errorf("%w: a literal of %d bytes, at byte %d of the literals", ErrCorrupt, l, lit)
		}
		m := int(d.mTable[ms].decode(&ms, &in))
		if nd := int(d.dTable[ds].decode(&ds, &in)); nd != 0 {
			dist = nd
		}
		if dist < 0 || dist > d.produced()+l {
			return fmt.Errorf("%w: a match from %d bytes back, and the output has %d", ErrCorrupt, dist, d.produced()+l)
		}
		if err := d.literal(d.literals[lit : lit+l]); err != nil {
			return err
		}
		lit += l
		if err := d.match(dist, m); err != nil {
			return err
		}
	}
	return nil
}
