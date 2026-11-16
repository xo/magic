// Copyright (c) 2020 Lonami. Under the MIT license, in LICENSE.
//
// This file translates src/tree.rs of lzxd, at the commit that
// docs/upstream/upstreams.tsv names (D61).

package lzx

import "fmt"

// tree is a canonical Huffman code, as LZX stores it: the length of the code
// of each symbol, from 1 to 16, or 0 for a symbol with no code.
type tree struct {
	lens []uint8
	// table gives the symbol of each value of the next bits bits. bits is 0
	// when no symbol has a code.
	table []uint16
	bits  uint
}

// newTree returns a tree of n symbols with no code.
func newTree(n int) tree {
	return tree{lens: make([]uint8, n)}
}

// build makes the table from the lengths. Each symbol of length l takes
// 1<<(bits-l) entries, in the order of the lengths, then of the symbols. The
// code must fill the table, or have no symbol.
func (t *tree) build() error {
	var longest uint8
	for _, l := range t.lens {
		longest = max(longest, l)
	}
	t.bits = uint(longest)
	if longest == 0 {
		return nil
	}
	size := 1 << longest
	if cap(t.table) < size {
		t.table = make([]uint16, size)
	}
	t.table = t.table[:size]
	pos := 0
	for l := uint8(1); l <= longest; l++ {
		n := 1 << (longest - l)
		for sym, sl := range t.lens {
			if sl != l {
				continue
			}
			if pos+n > size {
				return fmt.Errorf("%w: a Huffman code is over full", ErrCorrupt)
			}
			for i := range n {
				t.table[pos+i] = uint16(sym)
			}
			pos += n
		}
	}
	if pos != size {
		return fmt.Errorf("%w: a Huffman code is not complete", ErrCorrupt)
	}
	return nil
}

// decode reads one symbol.
func (t *tree) decode(b *bitReader) (int, error) {
	if t.bits == 0 {
		return 0, fmt.Errorf("%w: a Huffman code that has no symbol is used", ErrCorrupt)
	}
	sym := t.table[b.peek(t.bits)]
	return int(sym), b.skip(uint(t.lens[sym]))
}

// readLengths reads the lengths of the symbols from to end of t, with a
// pretree, which pre holds. Each code of the pretree is one of these:
//
//   - 0 to 16: the length is the old length minus the code, modulo 17.
//   - 17: 4 to 19 lengths of 0, with 4 more bits.
//   - 18: 20 to 51 lengths of 0, with 5 more bits.
//   - 19: 4 or 5 lengths of the same value, with 1 more bit and then a code
//     from 0 to 16 for the value, from the old length of the first symbol.
//
// A run can pass end, as in lzxd, but not the end of t.
func (t *tree) readLengths(b *bitReader, pre *tree, from, end int) error {
	for i := range pre.lens {
		v, err := b.read(4)
		if err != nil {
			return err
		}
		pre.lens[i] = uint8(v)
	}
	if err := pre.build(); err != nil {
		return err
	}
	for i := from; i < end; {
		code, err := pre.decode(b)
		if err != nil {
			return err
		}
		var run, extra uint
		var value uint8
		switch code {
		case 17:
			run, extra = 4, 4
		case 18:
			run, extra = 20, 5
		case 19:
			run, extra = 4, 1
		default:
			t.lens[i] = (17 + t.lens[i] - uint8(code)) % 17
			i++
			continue
		}
		v, err := b.read(extra)
		if err != nil {
			return err
		}
		run += uint(v)
		if code == 19 {
			c, err := pre.decode(b)
			if err != nil {
				return err
			}
			if c > 16 {
				return fmt.Errorf("%w: a run of the pretree holds code %d", ErrCorrupt, c)
			}
			value = (17 + t.lens[i] - uint8(c)) % 17
		}
		if run > uint(len(t.lens)-i) {
			return fmt.Errorf("%w: a run of the pretree passes the end of the code", ErrCorrupt)
		}
		for j := range int(run) {
			t.lens[i+j] = value
		}
		i += int(run)
	}
	return nil
}
