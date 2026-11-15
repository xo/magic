//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package archive

import (
	"bytes"
	"math/rand/v2"
	"testing"
)

// TestSevenZipFilterChunks holds that each filter gives the same output
// when the data comes in small pieces as when it comes whole, because a
// decoder writes its output in pieces of any size. test/diff compares the
// output with 7z.
func TestSevenZipFilterChunks(t *testing.T) {
	t.Parallel()
	// Bytes that many filters convert, such as 0xE8, 0xEB, 0xF0 and 0x94.
	marks := []byte{0xE8, 0xE9, 0x00, 0xFF, 0xEB, 0xF0, 0xF8, 0x94, 0x90, 0x48, 0x40, 0x7F, 0x10}
	r := rand.New(rand.NewPCG(5, 6))
	data := make([]byte, 64<<10+13)
	for i := range data {
		data[i] = marks[r.IntN(len(marks))]
		if r.IntN(3) == 0 {
			data[i] = byte(r.Uint32())
		}
	}
	for id, mk := range szFilters {
		props := []byte(nil)
		if id == szIDDelta {
			props = []byte{3}
		}
		run := func(sizes func() int) []byte {
			c, ok := mk(props)
			if !ok {
				t.Fatalf("expected %q, got: %q", "properties accepted", "rejected")
			}
			var out bytes.Buffer
			fw := &szFilterWriter{c: c, w: &out}
			in := bytes.Clone(data)
			for len(in) > 0 {
				n := min(sizes(), len(in))
				if _, err := fw.Write(in[:n]); err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				in = in[n:]
			}
			if err := fw.flush(); err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			return out.Bytes()
		}
		whole := run(func() int { return len(data) })
		if bytes.Equal(whole, data) {
			t.Errorf("expected %q, got: %q", "changed data", "the same data")
		}
		pieces := run(func() int { return 1 + r.IntN(40) })
		if !bytes.Equal(whole, pieces) {
			t.Errorf("expected %q, got: %q", "the same output", "another output")
		}
	}
}
