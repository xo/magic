// Copyright (c) 2020 Lonami. Under the MIT license, in LICENSE.
//
// This file translates src/lib.rs, src/block.rs and src/window.rs of lzxd, at
// the commit that docs/upstream/upstreams.tsv names (D61). It differs from
// lzxd in these places:
//
//   - It decodes LZX as CAB and CHM use it, with windows of 2^15 to 2^21
//     bytes. lzxd also takes the larger windows of LZX DELTA. It has no
//     extra match length of LZX DELTA, as lzxd has none.
//   - A match can write past the end of a frame. The bytes past it start the
//     next frame, and Overrun gives their number. lzxd does not stop when a
//     match passes the end.
//   - An uncompressed block of an odd size is followed by a byte of padding
//     in the same frame. When the block ends with the frame, the next frame
//     starts with no padding, and Used counts the byte of padding in the
//     frame that ends. lzxd always skips a byte before the next block.
//   - E8 translation changes a value of 0 to the negative of the position.
//     lzxd tests for a value over 0, and changes 0 by the translation size.
//   - Every match offset is checked against the bytes decoded since the
//     last reset, and errors do not panic.

// Package lzx decodes LZX, the compression of Microsoft that CAB archives and
// CHM help files use. A frame of LZX holds 32768 bytes of output, and the last
// frame can hold fewer. The decoder reads one frame at a time, and keeps the
// window and the Huffman codes from one frame to the next, until a reset.
// Decoder.Used gives the bytes that a frame read, so a caller finds where the
// next frame starts in a stream, such as the data of a folder of a CAB file.
// Decoder.Overrun tells when the last match of a frame passed its end, and
// Decoder.Uncompressed tells whether the frame ended in an uncompressed block.
//
// Section reads the content of a CHM file, which resets the decoder at a fixed
// interval of frames. It can start at any reset, so it can read any part of
// the content.
//
// LZX is not a format of libmagic, so the package does not register with
// github.com/xo/magic/compress (D61). It is a port of the crate lzxd, which
// follows the specification of LZX DELTA of Microsoft, [MS-PATCH].
package lzx

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// FrameSize is the number of bytes that a frame decodes to. The last frame
// of a stream can decode to fewer.
const FrameSize = 32768

// ErrCorrupt reports data that LZX does not allow. The errors of a decoder
// wrap it with the fault.
var ErrCorrupt = errors.New("lzx: corrupt data")

// The types of block.
const (
	verbatim     = 1
	alignedBlock = 2
	uncompressed = 3
)

// footer and base give the number of extra bits of each position slot, and
// the lowest offset of the slot, plus 2.
var footer, base [50]uint32

func init() {
	for s := range footer {
		switch {
		case s < 4:
			footer[s] = 0
		case s >= 36:
			footer[s] = 17
		default:
			footer[s] = uint32(s-2) / 2
		}
		if s > 0 {
			base[s] = base[s-1] + 1<<footer[s-1]
		}
	}
}

// Decoder decodes the frames of one stream of LZX. It is not safe for
// concurrent use.
type Decoder struct {
	windowSize int
	// slots is the number of position slots of the window.
	slots int

	// win holds the output since the last reset. It holds the window, and the
	// bytes that a match wrote past the end of the last frame. wpos is where
	// the next byte goes. written and emitted count the bytes since the reset
	// that went into win, and that went out.
	win               []byte
	wpos              int
	written, emitted  int64
	main, length, aln tree
	pre               tree

	// pos is the position of the next frame in the output since the last
	// reset, which E8 translation uses. 7z and lzxd count it from the reset
	// too.
	pos int64
	// header is true after the first frame since the reset read the header
	// of E8 translation. e8 is true when the stream translates, and e8size
	// is the translation size.
	header bool
	e8     bool
	e8size int32
	// r holds the three most recent match offsets.
	r [3]uint32
	// kind, size and left are the type of the current block, its size, and
	// the bytes of it that are left.
	kind       int
	size, left int
	// pad is true when an uncompressed block of an odd size ended in this
	// frame, so that a byte of padding comes before the next block.
	pad bool
	// used is the number of bytes of src that the last frame read, for Used,
	// over is the number of bytes that it wrote past its end, for Overrun,
	// and raw is true when it ended in an uncompressed block, for
	// Uncompressed.
	used, over int
	raw        bool
	// err is the error that broke the stream. Reset clears it.
	err error
}

// NewDecoder returns a decoder with a window of windowSize bytes, a power of
// two from 2^15 to 2^21.
func NewDecoder(windowSize int) (*Decoder, error) {
	slots := map[int]int{1 << 15: 30, 1 << 16: 32, 1 << 17: 34, 1 << 18: 36, 1 << 19: 38, 1 << 20: 42, 1 << 21: 50}[windowSize]
	if slots == 0 {
		return nil, fmt.Errorf("lzx: the window size %d is not a power of two from 2^15 to 2^21", windowSize)
	}
	d := &Decoder{
		windowSize: windowSize,
		slots:      slots,
		win:        make([]byte, windowSize+FrameSize+512),
		main:       newTree(256 + 8*slots),
		length:     newTree(249),
		aln:        newTree(8),
		pre:        newTree(20),
	}
	d.Reset()
	return d, nil
}

// Reset starts the decoder again, as at the start of a stream. The next
// frame reads the header of E8 translation, the Huffman codes start empty,
// and no match can reach the bytes before the reset.
func (d *Decoder) Reset() {
	d.wpos, d.written, d.emitted, d.pos = 0, 0, 0, 0
	clear(d.main.lens)
	clear(d.length.lens)
	d.header, d.e8, d.e8size = false, false, 0
	d.r = [3]uint32{1, 1, 1}
	d.kind, d.size, d.left = 0, 0, 0
	d.pad, d.err = false, nil
}

// Decode decodes the next frame, from its compressed bytes src, and appends
// it to dst. n is the size of the frame, FrameSize for each frame but the
// last of a stream. When the frame is damaged, Decode returns dst as it was
// and an error that wraps ErrCorrupt. Each later call fails until Reset.
func (d *Decoder) Decode(dst, src []byte, n int) ([]byte, error) {
	if n < 0 || n > FrameSize {
		return dst, fmt.Errorf("lzx: the frame size %d is not from 0 to %d", n, FrameSize)
	}
	d.used, d.over, d.raw = 0, 0, false
	if d.err != nil {
		return dst, d.err
	}
	if err := d.frame(src, n); err != nil {
		d.err = err
		return dst, err
	}
	start := len(dst)
	i := d.wpos - int(d.written-d.emitted)
	if i < 0 {
		i += len(d.win)
	}
	for k := n; k > 0; {
		m := min(k, len(d.win)-i)
		dst = append(dst, d.win[i:i+m]...)
		i, k = 0, k-m
	}
	d.emitted += int64(n)
	// E8 translation stops after the first 2^30 bytes, and leaves out the
	// last 10 bytes of each frame.
	if d.e8 && d.pos < 1<<30 && n > 10 {
		d.translate(dst[start:])
	}
	d.pos += int64(n)
	return dst, nil
}

// Used returns the number of bytes of src that the last call of Decode read,
// or 0 when it failed. In a stream whose frames follow one another, as in the
// data of a folder of a CAB file, the next frame starts at that offset of
// src. The bits of a frame are in 16-bit words, so a frame that ends inside a
// word reads all of it. When an uncompressed block of an odd size ends with
// the frame, the frame reads the byte of padding after the block too, if src
// holds it. cabextract 1.11 reads a stream of frames in this way.
func (d *Decoder) Used() int {
	return d.used
}

// Overrun returns the number of bytes that the last match of the last frame
// wrote past the end of the frame, or 0 when Decode failed. Decode keeps
// them, and they start the next frame. cabextract 1.11 fails on a frame
// whose last match passes its end, so a caller that reads as cabextract
// fails when Overrun is not 0.
func (d *Decoder) Overrun() int {
	return d.over
}

// Uncompressed reports whether the last frame ended with the bytes of an
// uncompressed block, which the decoder copies, and not with the bits of a
// compressed block. It is false when Decode failed. A reader of bits that
// loads a word before it needs it, as cabextract 1.11 does, has read 2 bytes
// past Used after bits, and none after the bytes of an uncompressed block.
func (d *Decoder) Uncompressed() bool {
	return d.raw
}

// frame decodes until win holds the n bytes of the frame.
func (d *Decoder) frame(src []byte, n int) error {
	b := &bitReader{src: src}
	d.pad = false
	if !d.header {
		d.header = true
		v, err := b.read(1)
		if err != nil {
			return err
		}
		if v == 1 {
			size, err := b.read(32)
			if err != nil {
				return err
			}
			d.e8, d.e8size = true, int32(size)
		}
	}
	for d.written-d.emitted < int64(n) {
		if d.left == 0 {
			if d.pad {
				if _, err := b.bytes(1); err != nil {
					return err
				}
				d.pad = false
			}
			if err := d.block(b); err != nil {
				return err
			}
		}
		if d.kind == uncompressed {
			k := min(d.left, n-int(d.written-d.emitted))
			data, err := b.bytes(k)
			if err != nil {
				return err
			}
			for len(data) > 0 {
				m := copy(d.win[d.wpos:], data)
				data = data[m:]
				d.advance(m)
			}
			if d.left -= k; d.left == 0 && d.size%2 == 1 {
				d.pad = true
			}
			continue
		}
		if err := d.symbol(b); err != nil {
			return err
		}
	}
	d.used, d.over, d.raw = b.used(), int(d.written-d.emitted)-n, d.kind == uncompressed
	if d.pad {
		d.used = min(d.used+1, len(src))
	}
	return nil
}

// block reads the header of the next block: its type in 3 bits and its size
// in 24 bits, then the codes of a verbatim or aligned offset block, or the
// match offsets of an uncompressed block.
func (d *Decoder) block(b *bitReader) error {
	kind, err := b.read(3)
	if err != nil {
		return err
	}
	size, err := b.read(24)
	if err != nil {
		return err
	}
	if size == 0 {
		return fmt.Errorf("%w: a block of size 0", ErrCorrupt)
	}
	switch kind {
	case alignedBlock:
		for i := range d.aln.lens {
			v, err := b.read(3)
			if err != nil {
				return err
			}
			d.aln.lens[i] = uint8(v)
		}
		if err := d.aln.build(); err != nil {
			return err
		}
		if d.aln.bits == 0 {
			return fmt.Errorf("%w: the aligned offset code has no symbol", ErrCorrupt)
		}
		fallthrough
	case verbatim:
		if err := d.trees(b); err != nil {
			return err
		}
	case uncompressed:
		if err := b.align(); err != nil {
			return err
		}
		data, err := b.bytes(12)
		if err != nil {
			return err
		}
		for i := range d.r {
			d.r[i] = binary.LittleEndian.Uint32(data[4*i:])
		}
	default:
		return fmt.Errorf("%w: block type %d", ErrCorrupt, kind)
	}
	d.kind, d.size, d.left = int(kind), int(size), int(size)
	return nil
}

// trees reads the main code, in two parts, and the code of the lengths.
// Each part has a pretree of its own.
func (d *Decoder) trees(b *bitReader) error {
	if err := d.main.readLengths(b, &d.pre, 0, 256); err != nil {
		return err
	}
	if err := d.main.readLengths(b, &d.pre, 256, len(d.main.lens)); err != nil {
		return err
	}
	if err := d.length.readLengths(b, &d.pre, 0, len(d.length.lens)); err != nil {
		return err
	}
	if err := d.main.build(); err != nil {
		return err
	}
	if d.main.bits == 0 {
		return fmt.Errorf("%w: the main code has no symbol", ErrCorrupt)
	}
	// A block with no long match has no code of lengths.
	return d.length.build()
}

// symbol decodes a literal or a match of a verbatim or aligned offset block.
func (d *Decoder) symbol(b *bitReader) error {
	sym, err := d.main.decode(b)
	if err != nil {
		return err
	}
	if sym < 256 {
		d.win[d.wpos] = byte(sym)
		d.advance(1)
		d.left--
		return nil
	}
	sym -= 256
	n := sym&7 + 2
	if n == 9 {
		l, err := d.length.decode(b)
		if err != nil {
			return err
		}
		n += l
	}
	var off uint32
	switch slot := sym >> 3; slot {
	case 0:
		off = d.r[0]
	case 1:
		off = d.r[1]
		d.r[1] = d.r[0]
		d.r[0] = off
	case 2:
		off = d.r[2]
		d.r[2] = d.r[0]
		d.r[0] = off
	default:
		extra := uint(footer[slot])
		var v uint32
		if d.kind == alignedBlock && extra >= 3 {
			if v, err = b.read(extra - 3); err != nil {
				return err
			}
			a, err := d.aln.decode(b)
			if err != nil {
				return err
			}
			v = v<<3 + uint32(a)
		} else if v, err = b.read(extra); err != nil {
			return err
		}
		off = base[slot] + v - 2
		d.r[2], d.r[1], d.r[0] = d.r[1], d.r[0], off
	}
	if n > d.left {
		return fmt.Errorf("%w: a match of %d bytes passes the end of its block", ErrCorrupt, n)
	}
	if off == 0 || int64(off) > d.written || int(off) > d.windowSize {
		return fmt.Errorf("%w: a match from %d bytes back, after %d bytes", ErrCorrupt, off, d.written)
	}
	from := d.wpos - int(off)
	if from < 0 {
		from += len(d.win)
	}
	for range n {
		d.win[d.wpos] = d.win[from]
		if from++; from == len(d.win) {
			from = 0
		}
		d.advance(1)
	}
	d.left -= n
	return nil
}

// advance moves the window on by n bytes that were written at wpos.
func (d *Decoder) advance(n int) {
	if d.wpos += n; d.wpos == len(d.win) {
		d.wpos = 0
	}
	d.written += int64(n)
}

// translate undoes E8 translation in the frame f. The encoder changed the
// relative offset after each byte 0xE8 to an absolute one, so that more of
// the calls of x86 code match.
func (d *Decoder) translate(f []byte) {
	for i := 0; i < len(f)-10; {
		if f[i] != 0xE8 {
			i++
			continue
		}
		cur := int32(d.pos) + int32(i)
		abs := int32(binary.LittleEndian.Uint32(f[i+1:]))
		if abs >= -cur && abs < d.e8size {
			rel := abs - cur
			if abs < 0 {
				rel = abs + d.e8size
			}
			binary.LittleEndian.PutUint32(f[i+1:], uint32(rel))
		}
		i += 5
	}
}
