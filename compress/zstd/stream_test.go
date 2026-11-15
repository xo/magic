package zstd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/xo/magic/compress"
)

// TestStream holds that the stream decoder finds the end of the frame, with
// data after it, and gives the data of the frame.
func TestStream(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("..", "..", "test", "corpus", "testdata", "utf8.txt.zst"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	exp, err := os.ReadFile(filepath.Join("..", "..", "testdata", "utf8.txt"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	s, ok := compress.LookupStream(compress.Zstd)
	if !ok {
		t.Fatalf("expected a stream decoder")
	}
	var out bytes.Buffer
	n, err := s(append(src, "trailing data"...), &out)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if n != len(src) || !bytes.Equal(out.Bytes(), exp) {
		t.Errorf("expected %d bytes giving %d, got: %d giving %d", len(src), len(exp), n, out.Len())
	}
	if _, err := s(src[:len(src)-3], &bytes.Buffer{}); err == nil {
		t.Errorf("expected an error for a cut frame, got: nil")
	}
}
