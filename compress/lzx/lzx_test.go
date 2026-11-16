package lzx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// sample returns n bytes of text and code: words, x86 calls with the byte
// E8, and runs of bytes that do not compress, from a fixed seed.
func sample(n int, seed uint32) []byte {
	words := []string{"the ", "window ", "frame ", "of ", "LZX ", "help ", "file ", "and ", "reset ", "table ", "\n"}
	out := make([]byte, 0, n+64)
	next := func() uint32 {
		seed = seed*1664525 + 1013904223
		return seed >> 8
	}
	for len(out) < n {
		switch v := next() % 16; {
		case v < 11:
			out = append(out, words[v]...)
		case v < 13:
			out = append(out, 0xE8)
			out = binary.LittleEndian.AppendUint32(out, next()%200000-1000)
		case v < 14:
			for range next() % 40 {
				out = append(out, byte(next()))
			}
		default:
			// A copy of an earlier part, far back.
			if len(out) > 100 {
				at := int(next()) % (len(out) - 50)
				out = append(out, out[at:at+int(next()%50)]...)
			}
		}
	}
	return out[:n]
}

// decodeFrames decodes the frames of data with one decoder, with a reset at
// each interval of frames.
func decodeFrames(t *testing.T, windowSize, interval int, frames [][]byte, length int) []byte {
	t.Helper()
	d, err := NewDecoder(windowSize)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	var out []byte
	for i, f := range frames {
		if interval > 0 && i%interval == 0 {
			d.Reset()
		}
		if out, err = d.Decode(out, f, min(FrameSize, length-len(out))); err != nil {
			t.Fatalf("frame %d: expected no error, got: %v", i, err)
		}
	}
	return out
}

// TestRoundTrip compresses samples with the encoder of the tests, in each
// type of block, and decodes them frame by frame.
func TestRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		enc  encoder
		n    int
	}{
		{"verbatim", encoder{windowSize: 1 << 15, blockSize: 1 << 20, kinds: []int{verbatim}}, 100000},
		{"aligned", encoder{windowSize: 1 << 17, blockSize: 30000, kinds: []int{alignedBlock}}, 150000},
		{"uncompressed", encoder{windowSize: 1 << 16, blockSize: 10001, kinds: []int{uncompressed}}, 70001},
		{"mixed", encoder{windowSize: 1 << 16, blockSize: 9999, kinds: []int{verbatim, uncompressed, alignedBlock}}, 200000},
		{"e8", encoder{windowSize: 1 << 16, e8size: 12000000, blockSize: 20000, kinds: []int{alignedBlock, verbatim}}, 100000},
		{"resets", encoder{windowSize: 1 << 16, interval: 2, e8size: 5000000, blockSize: 25000, kinds: []int{verbatim, alignedBlock, uncompressed}}, 230000},
		{"large window", encoder{windowSize: 1 << 21, interval: 3, blockSize: 50000, kinds: []int{alignedBlock}}, 300000},
		{"small", encoder{windowSize: 1 << 15, e8size: 1000, blockSize: 100, kinds: []int{verbatim}}, 9},
		{"empty", encoder{windowSize: 1 << 15, blockSize: 100, kinds: []int{verbatim}}, 0},
	} {
		data := sample(tt.n, 7)
		frames := tt.enc.encode(data)
		if want := max((tt.n+FrameSize-1)/FrameSize, 1); len(frames) != want {
			t.Fatalf("%s: expected %d frames, got: %d", tt.name, want, len(frames))
		}
		got := decodeFrames(t, tt.enc.windowSize, tt.enc.interval, frames, tt.n)
		if !bytes.Equal(got, data) {
			t.Errorf("%s: the decoded data differs from the sample at byte %d", tt.name, diff(got, data))
		}
	}
}

// diff returns the first index where a and b differ.
func diff(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// section compresses a sample with resets, as a CHM file holds it.
func section(t *testing.T, n, interval int) (*Section, []byte) {
	t.Helper()
	data := sample(n, 11)
	enc := encoder{windowSize: 1 << 16, interval: interval, e8size: 3000000, blockSize: 20000, kinds: []int{verbatim, alignedBlock, uncompressed}}
	s := &Section{WindowSize: 1 << 16, ResetInterval: interval, Length: int64(n)}
	for _, f := range enc.encode(data) {
		s.ResetTable = append(s.ResetTable, uint64(len(s.Data)))
		s.Data = append(s.Data, f...)
	}
	return s, data
}

// TestSection reads parts of a section in an order that jumps back and
// forth, and across frames and resets.
func TestSection(t *testing.T) {
	t.Parallel()
	s, data := section(t, 300000, 2)
	for _, r := range [][2]int{
		{0, 10}, {5, 40000}, {250000, 50000}, {65536, 1}, {65535, 2}, {100, 100},
		{131072, 70000}, {299990, 10}, {0, 300000}, {200000, 0}, {32767, 32770},
	} {
		got, err := s.Decode([]byte("x"), int64(r[0]), r[1])
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if want := append([]byte("x"), data[r[0]:r[0]+r[1]]...); !bytes.Equal(got, want) {
			t.Errorf("read of %d at %d: the data differs at byte %d", r[1], r[0], diff(got, want))
		}
	}
	got, err := s.Decode(nil, 299995, 100)
	if !errors.Is(err, io.EOF) || !bytes.Equal(got, data[299995:]) {
		t.Errorf("expected the last 5 bytes and %v, got: %d bytes, %v", io.EOF, len(got), err)
	}
	got, err = s.Decode(nil, 400000, 10)
	if !errors.Is(err, io.EOF) || len(got) != 0 {
		t.Errorf("expected no bytes and %v, got: %d bytes, %v", io.EOF, len(got), err)
	}
}

// TestSectionErrors reads damaged sections, and makes sure that a read of a
// good part works after a damaged one.
func TestSectionErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		change func(s *Section)
		off    int64
	}{
		{"short table", func(s *Section) { s.ResetTable = s.ResetTable[:3] }, 4 * FrameSize},
		{"offset past the end", func(s *Section) { s.ResetTable[5] = 1 << 40 }, 4 * FrameSize},
		{"offsets back", func(s *Section) { s.ResetTable[5] = s.ResetTable[4] - 1 }, 4 * FrameSize},
		{"damaged frame", func(s *Section) { clear(s.Data[s.ResetTable[4]:s.ResetTable[5]]) }, 4 * FrameSize},
		{"no interval", func(s *Section) { s.ResetInterval = 0 }, 0},
		{"bad window", func(s *Section) { s.WindowSize = 1000 }, 0},
		{"negative length", func(s *Section) { s.Length = -1 }, 0},
	} {
		s, data := section(t, 200000, 2)
		saved := *s
		tt.change(s)
		if _, err := s.Decode(nil, tt.off, 100); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: expected %v, got: %v", tt.name, ErrCorrupt, err)
		}
		if tt.off == 0 {
			continue
		}
		s.ResetTable, s.Data = saved.ResetTable, saved.Data
		got, err := s.Decode(nil, 10, 100)
		if err != nil || !bytes.Equal(got, data[10:110]) {
			t.Errorf("%s: expected the good part, got: %v", tt.name, err)
		}
	}
	s, _ := section(t, 1000, 1)
	if _, err := s.Decode(nil, -1, 10); err == nil {
		t.Errorf("expected an error for a negative offset")
	}
}

// TestDecoderErrors gives the decoder bad arguments and damaged frames.
func TestDecoderErrors(t *testing.T) {
	t.Parallel()
	for _, size := range []int{0, 1 << 14, 1<<15 + 1, 1 << 22} {
		if _, err := NewDecoder(size); err == nil {
			t.Errorf("window %d: expected an error", size)
		}
	}
	enc := encoder{windowSize: 1 << 15, blockSize: 1 << 20, kinds: []int{verbatim}}
	frames := enc.encode(sample(40000, 3))
	d, err := NewDecoder(1 << 15)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if _, err := d.Decode(nil, frames[0], FrameSize+1); err == nil {
		t.Errorf("expected an error for a frame over %d bytes", FrameSize)
	}
	for _, tt := range []struct {
		name string
		src  []byte
	}{
		{"empty", nil},
		{"cut short", frames[0][:len(frames[0])/2]},
		// No E8 bit, then block type 0.
		{"block type 0", []byte{0, 0, 0, 0, 0, 0}},
		// No E8 bit, then block type 7.
		{"block type 7", []byte{0, 0x70, 0, 0, 0, 0}},
		// No E8 bit, then an uncompressed block of size 0.
		{"block of size 0", []byte{0, 0x30, 0, 0, 0, 0}},
		// An uncompressed block of 1000 bytes, which are not there.
		{"short uncompressed", []byte{0x00, 0x30, 0x80, 0x07, 0x00, 0x00, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 'a'}},
		// A verbatim block whose pretree has no symbol.
		{"empty pretree", []byte{0x00, 0x10, 0x00, 0x01, 0x00, 0x00, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
	} {
		d.Reset()
		got, err := d.Decode([]byte("x"), tt.src, FrameSize)
		if !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: expected %v, got: %v", tt.name, ErrCorrupt, err)
		}
		if string(got) != "x" {
			t.Errorf("%s: expected %q, got: %q", tt.name, "x", got)
		}
		// The stream stays broken until Reset.
		if _, err2 := d.Decode(nil, frames[0], FrameSize); !errors.Is(err2, err) {
			t.Errorf("%s: expected %v, got: %v", tt.name, err, err2)
		}
	}
	// The second frame alone has matches into the first.
	d.Reset()
	if _, err := d.Decode(nil, frames[1], 40000-FrameSize); !errors.Is(err, ErrCorrupt) {
		t.Errorf("expected %v, got: %v", ErrCorrupt, err)
	}
}

// TestUncompressedBytes decodes an uncompressed block of 3 bytes, the
// example of the tests of lzxd, then a second block after the byte that pads
// the first.
func TestUncompressedBytes(t *testing.T) {
	t.Parallel()
	block := []byte{0x00, 0x30, 0x30, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 'a', 'b', 'c', 0x00}
	d, err := NewDecoder(1 << 15)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	got, err := d.Decode(nil, block, 3)
	if err != nil || string(got) != "abc" {
		t.Errorf("expected %q, got: %q, %v", "abc", got, err)
	}
	// The same block in one frame twice: the second has no bit of E8.
	second := append([]byte{0x00, 0x60, 0x00, 0x00}, block[4:]...)
	second[0], second[1], second[2], second[3] = 0x00, 0x60, 0x00, 0x03
	d.Reset()
	got, err = d.Decode(nil, append(block[:len(block):len(block)], second...), 6)
	if err != nil || string(got) != "abcabc" {
		t.Errorf("expected %q, got: %q, %v", "abcabc", got, err)
	}
}

// TestE8 decodes the frame of E8 translation of the tests of lzxd.
func TestE8(t *testing.T) {
	t.Parallel()
	src := []byte{ //nolint:prealloc // the header of the frame, then the text
		0x5B, 0x80, 0x80, 0x8D, 0x00, 0x30, 0x80, 0x0A, 0x18, 0x00, 0x00, 0x00, 0x01, 0x00,
		0x00, 0x00, 0x05, 0x00, 0x00, 0x00,
	}
	text := []byte("This file has an E8 byte to test E8 translation, X")
	text = append(text, bytes.Repeat([]byte("d"), 96)...)
	text = append(text, 0xE8, 0x7B, 0x00, 0x00, 0x00, 0xE8, 0x7B, 0x00, 0x00, 0x00)
	text = append(text, bytes.Repeat([]byte("d"), 12)...)
	src = append(src, text...)
	want := bytes.Clone(text)
	copy(want[146:], []byte{0xE8, 0xE9, 0xFF, 0xFF, 0xFF, 0xE8, 0xE4, 0xFF, 0xFF, 0xFF})
	d, err := NewDecoder(1 << 15)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	got, err := d.Decode(nil, src, len(text))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("expected %q, got: %q", want, got)
	}
}

// TestTranslateZero makes sure that E8 translation maps an absolute offset
// of 0 to the negative of the position, as the encoder makes it.
func TestTranslateZero(t *testing.T) {
	t.Parallel()
	f := make([]byte, 40)
	f[20] = 0xE8
	binary.LittleEndian.PutUint32(f[21:], uint32(0xFFFFFFFF-100+1)) // -100
	enc := bytes.Clone(f)
	translate(enc, 80, 1000)
	if v := int32(binary.LittleEndian.Uint32(enc[21:])); v != 0 {
		t.Fatalf("expected the encoder to write 0, got: %d", v)
	}
	d := &Decoder{pos: 80, e8: true, e8size: 1000}
	d.translate(enc)
	if !bytes.Equal(enc, f) {
		t.Errorf("expected %x, got: %x", f, enc)
	}
}

// FuzzDecode decodes any data as frames of a stream, and as a section with
// resets. It must not panic, and must stop.
func FuzzDecode(f *testing.F) {
	enc := encoder{windowSize: 1 << 15, interval: 1, e8size: 1000, blockSize: 5000, kinds: []int{verbatim, alignedBlock, uncompressed}}
	for _, fr := range enc.encode(sample(3*FrameSize, 5)) {
		f.Add(fr)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		d, err := NewDecoder(1 << 15)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		for _, part := range [][]byte{b, b[len(b)/2:], b} {
			out, err := d.Decode(nil, part, FrameSize)
			if err != nil {
				if !errors.Is(err, ErrCorrupt) {
					t.Fatalf("expected %v, got: %v", ErrCorrupt, err)
				}
				break
			}
			if len(out) != FrameSize {
				t.Fatalf("expected %d bytes, got: %d", FrameSize, len(out))
			}
		}
		s := &Section{Data: b, WindowSize: 1 << 15, ResetInterval: 2, ResetTable: []uint64{0, uint64(len(b) / 3), uint64(len(b) / 2)}, Length: 3*FrameSize - 7}
		if _, err := s.Decode(nil, FrameSize+5, 2*FrameSize); err != nil && !errors.Is(err, ErrCorrupt) && !errors.Is(err, io.EOF) {
			t.Fatalf("expected %v, got: %v", ErrCorrupt, err)
		}
	})
}
