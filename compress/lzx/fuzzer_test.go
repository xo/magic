package lzx

import (
	"errors"
	"testing"
)

// TestFuzzerFrames decodes the frames of the 3 tests that lzxd added in
// commit aa723fd, from crashes that its fuzzer found. lzxd returns
// UnexpectedEof for each one. The port returns errShort, which wraps
// ErrCorrupt.
func TestFuzzerFrames(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		src  []byte
		n    int
	}{
		// The tests of lzxd. In the first one, the frame has 1 byte.
		{"uncompressed_single_byte_data", []byte{0x00}, 32},
		// An uncompressed block that has no bytes left in the frame.
		{"uncompressed_advance_zero", []byte{
			0x32, 0x32, 0x32, 0x32, 0x32, 0x32, 0x32, 0x32, 0x32, 0x32, 0x32, 0x32, 0x32, 0x32,
			0x32, 0x32,
		}, 32},
		// A frame that ends with 1 byte of a word.
		{"peek_bits_oneword_bounds_check", []byte{
			0x12, 0x12, 0x12, 0x12, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x12,
			0x12, 0x12, 0x12, 0x12, 0x12, 0x12, 0x12, 0x12, 0x00, 0x00, 0x00, 0x12, 0x8a,
		}, 32},
		// A frame of 0 bytes reads only the bit of E8, which is in a word that
		// has only 1 byte. Before aa723fd, lzxd panicked, and the port read
		// the bit as 0.
		{"bit of E8 in 1 byte", []byte{0x00}, 0},
	} {
		d, err := NewDecoder(1 << 15)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if _, err := d.Decode(nil, tt.src, tt.n); !errors.Is(err, errShort) {
			t.Errorf("%s: expected %v, got: %v", tt.name, errShort, err)
		}
	}
}
