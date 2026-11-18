// Copyright (c) 2015-2016, Apple Inc. All rights reserved. Under the BSD
// 3-clause license, in LICENSE.
//
// This file translates the decoder of src/lzfse_fse.h and src/lzfse_fse.c of
// lzfse, at the commit that docs/upstream/upstreams.tsv names (D63): the
// stream of bits that FSE reads backward, and the tables that turn a state
// into a symbol or a value. It reads the stream as lzfse does with
// FSE_IOSTREAM_64, on a 64-bit machine.

package lzfse

import (
	"fmt"
	"math/bits"
)

// bitReader is fse_in_stream64. It holds from 56 to 63 bits after each
// flush, and the bits come out from the most significant end.
type bitReader struct {
	acc   uint64
	nbits uint
}

// load8 returns the 8 bytes of b at pos as a little-endian number. Bytes past
// the end of b are 0. lzfse reads them from memory, and masks them away.
func load8(b []byte, pos int) uint64 {
	if pos+8 <= len(b) {
		return le.Uint64(b[pos:])
	}
	var v uint64
	for i := len(b) - 1; i >= pos; i-- {
		v = v<<8 | uint64(b[i])
	}
	return v
}

// init is fse_in_checked_init64. The stream ends at byte *pos of b, and does
// not start before byte start. n is the number of bits of the last byte
// that the encoder did not fill, from -7 to 0.
func (r *bitReader) init(b []byte, pos *int, start int, n int32) error {
	if n < -7 || n > 0 {
		return fmt.Errorf("%w: a stream of FSE starts with %d bits", ErrCorrupt, n)
	}
	if n != 0 {
		if *pos-start < 8 {
			return fmt.Errorf("%w: a stream of FSE is shorter than 8 bytes", ErrCorrupt)
		}
		*pos -= 8
		r.acc = load8(b, *pos)
		r.nbits = uint(n + 64)
	} else {
		if *pos-start < 7 {
			return fmt.Errorf("%w: a stream of FSE is shorter than 7 bytes", ErrCorrupt)
		}
		*pos -= 7
		r.acc = load8(b, *pos) & (1<<56 - 1)
		r.nbits = 56
	}
	// The encoder writes zeros above the bits of the stream.
	if r.acc>>r.nbits != 0 {
		return fmt.Errorf("%w: a stream of FSE has bits above its start", ErrCorrupt)
	}
	return nil
}

// flush is fse_in_checked_flush64. It reads whole bytes backward until the
// reader holds from 56 to 63 bits.
func (r *bitReader) flush(b []byte, pos *int, start int) error {
	n := (63 - r.nbits) &^ 7
	if n == 0 {
		return nil
	}
	p := *pos - int(n>>3)
	if p < start {
		return fmt.Errorf("%w: a stream of FSE ends before its start", ErrCorrupt)
	}
	*pos = p
	r.acc = r.acc<<n | load8(b, p)&(1<<n-1)
	r.nbits += n
	return nil
}

// pull is fse_in_pull64. It takes the n most significant bits. A flush
// leaves enough bits for each read of the decoder.
func (r *bitReader) pull(n uint) uint64 {
	r.nbits -= n
	v := r.acc >> r.nbits
	r.acc &= 1<<r.nbits - 1
	return v
}

// litEntry is fse_decoder_entry: one state of the table of the literals.
type litEntry struct {
	k      uint8 // the bits to read for the next state
	symbol uint8
	delta  int32 // the base of the next state
}

// valueEntry is fse_value_decoder_entry: one state of the table of L, M or D.
type valueEntry struct {
	totalBits uint8 // the bits of the state and of the value
	valueBits uint8 // the extra bits of the value
	delta     int32 // the base of the next state
	base      int32 // the base of the value
}

// decode is fse_value_decode. It returns the value of the state *s, and
// moves *s to the next state.
func (e valueEntry) decode(s *int, r *bitReader) int32 {
	v := r.pull(uint(e.totalBits))
	*s = int(e.delta) + int(v>>e.valueBits)
	return e.base + int32(v&(1<<e.valueBits-1))
}

// spread gives the number of bits k to read for each of the f states of a
// symbol, among n states in all, and the base of the next state. It calls
// fn for each state j, from 0 to f-1. It is the part that
// fse_init_decoder_table and fse_init_value_decoder_table share. n is a
// power of 2, and f is at most n.
func spread(n, f int, fn func(j, k int, delta int32)) {
	k := bits.LeadingZeros32(uint32(f)) - bits.LeadingZeros32(uint32(n))
	j0 := (2*n)>>k - f
	for j := range f {
		if j < j0 {
			fn(j, k, int32((f+j)<<k-n))
		} else {
			fn(j, k-1, int32((j-j0)<<(k-1)))
		}
	}
}

// initLiteralTable is fse_init_decoder_table. The states of each symbol follow
// the states of the symbol before it. The states past the sum of the
// frequencies keep what they held, as in lzfse. A valid stream does not
// reach them.
func initLiteralTable(t []litEntry, freq []uint16) {
	i := 0
	for sym, f := range freq {
		spread(len(t), int(f), func(_, k int, delta int32) {
			t[i] = litEntry{k: uint8(k), symbol: uint8(sym), delta: delta}
			i++
		})
	}
}

// initValueTable is fse_init_value_decoder_table. Each state also holds the
// extra bits and the base of the value of its symbol. It leaves the states
// past the sum of the frequencies as initLiteralTable does.
func initValueTable(t []valueEntry, freq []uint16, extra []uint8, base []int32) {
	i := 0
	for sym, f := range freq {
		spread(len(t), int(f), func(_, k int, delta int32) {
			t[i] = valueEntry{
				totalBits: uint8(k) + extra[sym],
				valueBits: extra[sym],
				delta:     delta,
				base:      base[sym],
			}
			i++
		})
	}
}
