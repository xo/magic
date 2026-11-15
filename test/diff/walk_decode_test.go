package diff

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/xo/magic/walk"
)

// TestWalkDecodes extracts zstd, lzma and xz, which binwalk-ng extracts with
// its own code to decompressed.bin.
func TestWalkDecodes(t *testing.T) {
	t.Parallel()
	exp, err := os.ReadFile(filepath.Join("..", "..", "testdata", "utf8.txt"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	// binwalk needs two blocks of zstd, and a block holds at most 128 KiB.
	big := bytes.Repeat(exp, 200<<10/len(exp)+1)
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	zst := enc.EncodeAll(big, nil)
	for _, tt := range []struct {
		file, format string
		exp          []byte
	}{
		{"", "zstd", big},
		{"utf8.txt.lzma", "lzma", exp},
		{"utf8.txt.xz", "xz", exp},
	} {
		src := zst
		if tt.file != "" {
			if src, err = os.ReadFile(filepath.Join("..", "corpus", "testdata", tt.file)); err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		}
		const off = 0x123
		data := append(bytes.Repeat([]byte{0xFF}, off), src...)
		data = append(data, bytes.Repeat([]byte{0}, 64)...)
		var got []string
		for e, err := range walk.WalkBytes(context.Background(), data, walk.WithFormats(tt.format)) {
			if err != nil {
				t.Fatalf("%s: expected no error, got: %v", tt.format, err)
			}
			if e.Offset != off || e.Size != int64(len(src)) {
				t.Errorf("%s: expected a match at %#x of %d bytes, got: %#x of %d", tt.format, off, len(src), e.Offset, e.Size)
			}
			for m, err := range e.Members(context.Background()) {
				if err != nil {
					t.Fatalf("%s: expected no error, got: %v", tt.format, err)
				}
				r, _ := m.Open()
				b, _ := io.ReadAll(r)
				if !bytes.Equal(b, tt.exp) {
					t.Errorf("%s: expected %d bytes in %s, got: %d", tt.format, len(tt.exp), m.Name, len(b))
				}
				got = append(got, m.Name)
			}
		}
		const want = "decompressed.bin"
		if len(got) != 1 || got[0] != want {
			t.Errorf("%s: expected [%q], got: %q", tt.format, want, got)
		}
	}
}
