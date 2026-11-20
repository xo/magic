// Copyright (c) 2020 Lonami. Under the MIT license, in LICENSE.
//
// This file translates src/bitstream.rs of lzxd, at the commit that
// docs/upstream/upstreams.tsv names (D61). The reader loads two words at a
// time. A word past the end of a frame, or a word that has only 1 byte, reads
// as 0, as lzxd peeks it. To read a bit of such a word is an error, as lzxd
// returns UnexpectedEof when it loads it.

package lzx

import "fmt"

// errShort reports a frame that ends before its data does.
var errShort = fmt.Errorf("%w: the data of a frame ends too soon", ErrCorrupt)

// bitReader reads the bits of one frame. LZX stores them in 16-bit words,
// little endian, and reads each word from its highest bit.
type bitReader struct {
	src []byte
	// pos is the next byte of src to load. It can pass the end of src.
	pos int
	// buf holds n bits that are loaded and not read, from bit 31 down.
	buf uint32
	n   uint
}

// fill loads words until buf holds more than 16 bits.
func (b *bitReader) fill() {
	for b.n <= 16 {
		var w uint32
		if b.pos+1 < len(b.src) {
			w = uint32(b.src[b.pos]) | uint32(b.src[b.pos+1])<<8
		}
		b.buf |= w << (16 - b.n)
		b.n += 16
		b.pos += 2
	}
}

// peek returns the next k bits, k from 0 to 16, and does not read them.
func (b *bitReader) peek(k uint) uint32 {
	b.fill()
	return b.buf >> (32 - k)
}

// skip reads k bits, k from 0 to 16. It fails when they pass the end of
// the last whole word of the frame. The words start at an odd byte after an
// uncompressed block of an odd size, so the last whole word ends at the end
// of the frame, or 1 byte before it.
func (b *bitReader) skip(k uint) error {
	b.fill()
	b.buf <<= k
	b.n -= k
	end := len(b.src) - (len(b.src)-b.pos)&1
	if b.pos*8-int(b.n) > end*8 {
		return errShort
	}
	return nil
}

// read reads k bits, k from 0 to 32, as a number with the first bit the
// highest.
func (b *bitReader) read(k uint) (uint32, error) {
	if k > 16 {
		hi, err := b.read(16)
		if err != nil {
			return 0, err
		}
		lo, err := b.read(k - 16)
		return hi<<(k-16) | lo, err
	}
	v := b.peek(k)
	return v, b.skip(k)
}

// align goes to the start of the next word, as the header of an uncompressed
// block does. At the start of a word, it reads a whole word of padding. Then
// it gives back the words that fill loaded and did not read, so that bytes
// reads the next byte.
func (b *bitReader) align() error {
	k := b.n % 16
	if k == 0 {
		k = 16
	}
	if err := b.skip(k); err != nil {
		return err
	}
	b.pos -= int(b.n / 8)
	b.buf, b.n = 0, 0
	return nil
}

// bytes reads k bytes, after align.
func (b *bitReader) bytes(k int) ([]byte, error) {
	if k > len(b.src)-b.pos {
		return nil, errShort
	}
	v := b.src[b.pos : b.pos+k]
	b.pos += k
	return v, nil
}

// used returns the number of bytes that the reader read, up to the end of the
// word that holds the last bit that it read. It leaves out the words that
// fill loaded and that hold no bit that the reader read.
func (b *bitReader) used() int {
	return b.pos - 2*int(b.n/16)
}
