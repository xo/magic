package xz

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/xo/magic/compress"
)

// TestStream holds that a stream decoder finds the end of the stream, with
// data after it, and gives the data of the stream.
func TestStream(t *testing.T) {
	t.Parallel()
	for name, m := range map[string]compress.Method{
		"utf8.txt.xz":   compress.XZ,
		"utf8.txt.lzma": compress.LZMA,
	} {
		src, err := os.ReadFile(filepath.Join("..", "..", "test", "corpus", "testdata", name))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		exp, err := os.ReadFile(filepath.Join("..", "..", "testdata", "utf8.txt"))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		s, ok := compress.LookupStream(m)
		if !ok {
			t.Fatalf("%s: expected a stream decoder", name)
		}
		var out bytes.Buffer
		n, err := s(append(src, "trailing data"...), &out)
		if err != nil {
			t.Fatalf("%s: expected no error, got: %v", name, err)
		}
		if n != len(src) || !bytes.Equal(out.Bytes(), exp) {
			t.Errorf("%s: expected %d bytes giving %d, got: %d giving %d", name, len(src), len(exp), n, out.Len())
		}
		if _, err := s(src[:len(src)-3], &bytes.Buffer{}); err == nil {
			t.Errorf("%s: expected an error for a cut stream, got: nil", name)
		}
	}
}
