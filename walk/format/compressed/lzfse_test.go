//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package compressed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/xo/magic/walk"
)

// TestLZFSE extracts the streams of compress/lzfse/testdata, which the
// reference encoder of Apple wrote, between other bytes. Each one decodes
// to decompressed.bin. binwalk-ng wrote the file of text.lzfse with the same
// hash, from test/corpus/testdata/walk/lzfse-3.bin.
func TestLZFSE(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		size int
		hash string
	}{
		{"tiny", 7, fmt.Sprintf("%x", sha256.Sum256([]byte("LZFSE!\n")))},
		{"text", 150000, "c572f6a4683076c821d9d340f276e7e8532179d530fbc667f455243b6dec1588"},
	} {
		src, err := os.ReadFile(filepath.Join("..", "..", "..", "compress", "lzfse", "testdata", tt.name+".lzfse"))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		const off = 0x40
		data := append(bytes.Repeat([]byte{0xFF}, off), src...)
		data = append(data, "after the stream"...)
		e := lzfseEntry(t, data)
		if e.Offset != off || e.Size != int64(len(src)) {
			t.Errorf("%s: expected a match at %#x of %d bytes, got: %#x of %d", tt.name, off, len(src), e.Offset, e.Size)
		}
		var got []string
		for m, err := range e.Members(context.Background()) {
			if err != nil {
				t.Fatalf("%s: expected no error, got: %v", tt.name, err)
			}
			r, err := m.Open()
			if err != nil {
				t.Fatalf("%s: expected no error, got: %v", tt.name, err)
			}
			b, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("%s: expected no error, got: %v", tt.name, err)
			}
			_ = r.Close()
			if hash := fmt.Sprintf("%x", sha256.Sum256(b)); len(b) != tt.size || hash != tt.hash {
				t.Errorf("%s: expected %q, got: %q", tt.name, fmt.Sprint(tt.size, " ", tt.hash), fmt.Sprint(len(b), " ", hash))
			}
			got = append(got, m.Name)
		}
		if len(got) != 1 || got[0] != "decompressed.bin" {
			t.Errorf("%s: expected %q, got: %q", tt.name, []string{"decompressed.bin"}, got)
		}
	}
}

// TestLZFSEFails holds that a stream that decodes to another size than its
// headers give has no file, as in binwalk-ng. The parser reads only the
// size of the payload of a block of LZVN, so it still finds the stream.
func TestLZFSEFails(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "compress", "lzfse", "testdata", "short.lzfse"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	for _, delta := range []int{-1, 1} {
		b := bytes.Clone(src)
		raw := int(binary.LittleEndian.Uint32(b[4:]))
		binary.LittleEndian.PutUint32(b[4:], uint32(raw+delta))
		e := lzfseEntry(t, append(b, make([]byte, 16)...))
		var got error
		for _, err := range e.Members(context.Background()) {
			got = err
		}
		if !errors.Is(got, walk.ErrNotExtracted) {
			t.Errorf("%d: expected %q, got: %q", delta, walk.ErrNotExtracted, fmt.Sprint(got))
		}
	}
}

// lzfseEntry walks data for lzfse, and returns the first match.
func lzfseEntry(t *testing.T, data []byte) *walk.Entry {
	t.Helper()
	for e, err := range walk.WalkBytes(context.Background(), data, walk.WithFormats("lzfse")) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if e.Format == "lzfse" {
			return e
		}
	}
	t.Fatalf("expected %q, got: %q", "lzfse", "nothing")
	return nil
}
