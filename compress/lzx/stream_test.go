package lzx

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"
)

// decodeStream decodes the frames of stream one after another, as
// cabextract reads the data of a folder of a CAB file. Each frame starts
// where Used says that the last one ended.
func decodeStream(t *testing.T, windowSize int, stream []byte, length int) []byte {
	t.Helper()
	d, err := NewDecoder(windowSize)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	var out []byte
	p := 0
	for len(out) < length {
		if out, err = d.Decode(out, stream[p:], min(FrameSize, length-len(out))); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		p += d.Used()
	}
	if p != len(stream) {
		t.Errorf("expected %d bytes read, got: %d", len(stream), p)
	}
	return out
}

// TestUsedVerbatim decodes the frames of verbatim and aligned offset blocks
// as one stream. The encoder ends each frame at the end of a word, so Used
// gives the size of the frame, and the next frame starts after it.
func TestUsedVerbatim(t *testing.T) {
	t.Parallel()
	for _, enc := range []encoder{
		{windowSize: 1 << 15, blockSize: 1 << 20, kinds: []int{verbatim}},
		{windowSize: 1 << 16, e8size: 12000000, blockSize: 20000, kinds: []int{alignedBlock, verbatim}},
	} {
		data := sample(100000, 9)
		frames := enc.encode(data)
		got := decodeStream(t, enc.windowSize, slices.Concat(frames...), len(data))
		if !bytes.Equal(got, data) {
			t.Errorf("the decoded data differs from the sample at byte %d", diff(got, data))
		}
	}
}

// uncompressedHeader writes the header of an uncompressed block of size
// bytes, and the three match offsets, which are 1.
func uncompressedHeader(w *bitWriter, size int) {
	w.write(uncompressed, 3)
	w.write(uint32(size), 24)
	w.write(0, 16-w.n)
	for range 3 {
		w.out = binary.LittleEndian.AppendUint32(w.out, 1)
	}
}

// TestUsedUncompressed decodes streams of uncompressed blocks, as
// cabextract 1.11 extracted them from cabinets made for the test. A block of an
// odd size that ends with the first frame has its byte of padding at the
// start of the second frame. A block that goes on past the end of the first
// frame goes on at the next byte, odd or even.
func TestUsedUncompressed(t *testing.T) {
	t.Parallel()
	text := make([]byte, FrameSize+100)
	for i := range text {
		text[i] = 'a' + byte(i%26)
	}
	// Block 1 holds 1 byte, then a byte of padding. Block 2 ends with the
	// frame, at an odd size, then a byte of padding. Block 3 is in the next
	// frame.
	w := &bitWriter{}
	w.write(0, 1)
	uncompressedHeader(w, 1)
	w.out = append(w.out, text[0], 0)
	uncompressedHeader(w, FrameSize-1)
	w.out = append(w.out, text[1:FrameSize]...)
	first := len(w.out)
	w.out = append(w.out, 0)
	uncompressedHeader(w, 100)
	w.out = append(w.out, text[FrameSize:]...)
	d, err := NewDecoder(1 << 15)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if _, err := d.Decode(nil, w.out, FrameSize); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if d.Used() != first+1 || !d.Uncompressed() {
		t.Errorf("expected %d and true, got: %d and %v", first+1, d.Used(), d.Uncompressed())
	}
	// The byte of padding is not in src.
	d.Reset()
	if _, err := d.Decode(nil, w.out[:first], FrameSize); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if d.Used() != first {
		t.Errorf("expected %d, got: %d", first, d.Used())
	}
	if got := decodeStream(t, 1<<15, w.out, len(text)); !bytes.Equal(got, text) {
		t.Errorf("the decoded data differs from the text at byte %d", diff(got, text))
	}

	// Block 2 goes on into the next frame, from an odd offset.
	w = &bitWriter{}
	w.write(0, 1)
	uncompressedHeader(w, 1)
	w.out = append(w.out, text[0], 0)
	uncompressedHeader(w, FrameSize-1+100)
	w.out = append(w.out, text[1:]...)
	if got := decodeStream(t, 1<<15, w.out, len(text)); !bytes.Equal(got, text) {
		t.Errorf("the decoded data differs from the text at byte %d", diff(got, text))
	}

	// A frame that fails reads nothing.
	if _, err := d.Decode(nil, nil, FrameSize); err == nil || d.Used() != 0 || d.Uncompressed() {
		t.Errorf("expected an error, 0 and false, got: %v, %d and %v", err, d.Used(), d.Uncompressed())
	}
}

// match is the token of literalStream for a match of 3 bytes from the last
// match offset, which is 1 at the start of a stream.
const match = -1

// literalStream returns a stream of one verbatim block, for a window of
// 2^15 bytes. Its main code gives 8 bits to each literal but 255, and 9 bits
// to 255 and to the one match symbol, a match of 3 bytes from the last match
// offset. Each frame holds the literals and matches of its tokens, and ends
// at the end of a word. text is the data of the stream.
func literalStream(frames [][]int) (stream, text []byte) {
	total := 0
	for _, f := range frames {
		for _, v := range f {
			if total++; v == match {
				total += 2
			}
		}
	}
	w := &bitWriter{}
	w.write(0, 1) // no E8 translation
	w.write(verbatim, 3)
	w.write(uint32(total), 24)
	// The pretree gives 1 bit to 9, which makes a length of 8 from 0, and 2
	// bits to 0, which keeps a length of 0, and to 8, which makes 9.
	pretree := func() {
		for s := range 20 {
			w.write(map[int]uint32{9: 1, 0: 2, 8: 2}[s], 4)
		}
	}
	pre := map[int][2]uint32{9: {0, 1}, 0: {2, 2}, 8: {3, 2}}
	put := func(s int) { w.write(pre[s][0], uint(pre[s][1])) }
	pretree()
	for range 255 {
		put(9)
	}
	put(8)
	// The match symbol is 257: slot 0, and a length of 3.
	pretree()
	for i := range 8 * 30 {
		if i == 1 {
			put(8)
		} else {
			put(0)
		}
	}
	pretree()
	for range 249 {
		put(0)
	}
	for _, f := range frames {
		for _, v := range f {
			switch v {
			case match:
				w.write(511, 9)
				last := text[len(text)-1]
				text = append(text, last, last, last)
			case 255:
				w.write(510, 9)
				text = append(text, 255)
			default:
				w.write(uint32(v), 8)
				text = append(text, byte(v))
			}
		}
		w.flush()
	}
	return w.out, text
}

// TestOverrun decodes a frame whose last match ends at the end of the frame,
// and frames whose last match passes the end by 1 and by 2 bytes.
// cabextract 1.11 extracts the first stream, and fails on the others.
func TestOverrun(t *testing.T) {
	t.Parallel()
	lits := make([]int, FrameSize)
	for i := range lits {
		lits[i] = 'a' + i*7%26
	}
	for over := range 3 {
		first := append(slices.Clone(lits[:FrameSize-3+over]), match)
		stream, text := literalStream([][]int{first, lits[:100-over]})
		d, err := NewDecoder(1 << 15)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if _, err := d.Decode(nil, stream, FrameSize); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if d.Overrun() != over || d.Uncompressed() {
			t.Errorf("expected %d and false, got: %d and %v", over, d.Overrun(), d.Uncompressed())
		}
		// Decode keeps the bytes past the end for the next frame.
		if got := decodeStream(t, 1<<15, stream, len(text)); !bytes.Equal(got, text) {
			t.Errorf("the decoded data differs from the text at byte %d", diff(got, text))
		}
	}
}
