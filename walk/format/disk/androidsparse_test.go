//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package disk_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/xo/magic/walk"
	_ "github.com/xo/magic/walk/format/disk"
)

// sparseImage returns an Android sparse image with blocks of 4096 bytes,
// and the image that it unpacks to. It holds a raw block, a hole of 32 MiB,
// two blocks of a fill, a block of a fill of zeros, a raw block, and a hole
// of 1 MiB at the end.
func sparseImage() ([]byte, []byte) {
	const (
		blockSize = 4096
		raw       = 0xCAC1
		fill      = 0xCAC2
		dontCare  = 0xCAC3
	)
	type chunk struct {
		typ    uint16
		blocks uint32
		data   []byte
	}
	block := func(c byte) []byte { return bytes.Repeat([]byte{c}, blockSize) }
	chunks := []chunk{
		{raw, 1, block(0xA1)},
		{dontCare, 8192, nil},
		{fill, 2, []byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{fill, 1, []byte{0, 0, 0, 0}},
		{raw, 1, block(0xB2)},
		{dontCare, 256, nil},
	}
	var blocks uint32
	var img []byte
	for _, c := range chunks {
		blocks += c.blocks
		switch c.typ {
		case raw:
			img = append(img, c.data...)
		case fill:
			img = append(img, bytes.Repeat(c.data, int(c.blocks)*blockSize/4)...)
		default:
			img = append(img, make([]byte, int(c.blocks)*blockSize)...)
		}
	}
	le := binary.LittleEndian
	b := le.AppendUint32(nil, 0xED26FF3A)
	b = le.AppendUint16(b, 1)
	b = le.AppendUint16(b, 0)
	b = le.AppendUint16(b, 28)
	b = le.AppendUint16(b, 12)
	b = le.AppendUint32(b, blockSize)
	b = le.AppendUint32(b, blocks)
	b = le.AppendUint32(b, uint32(len(chunks)))
	b = le.AppendUint32(b, 0)
	for _, c := range chunks {
		b = le.AppendUint16(b, c.typ)
		b = le.AppendUint16(b, 0)
		b = le.AppendUint32(b, c.blocks)
		b = le.AppendUint32(b, uint32(12+len(c.data)))
		b = append(b, c.data...)
	}
	return b, img
}

// TestAndroidSparseHoles extracts an image of 33 MiB under a limit of
// 64 KiB, which counts only the 16 KiB of blocks that are not holes, as
// binwalk-ng writes them. The bytes match the image in memory and on disk,
// and the file on disk is sparse.
func TestAndroidSparseHoles(t *testing.T) {
	t.Parallel()
	const limit = 64 << 10
	ctx := context.Background()
	data, img := sparseImage()
	var e *walk.Entry
	for got, err := range walk.WalkBytes(ctx, data, walk.WithFormats("android_sparse"), walk.WithMaxOutput(limit)) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		e = got
	}
	if e == nil || e.Format != "android_sparse" || e.Size != int64(len(data)) {
		t.Fatalf("expected a match of %d bytes, got: %v", len(data), e)
	}
	n := 0
	for m, err := range e.Members(ctx) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		n++
		if m.Name != "unsparsed.img" || m.Size != int64(len(img)) {
			t.Errorf("expected unsparsed.img of %d bytes, got: %s of %d", len(img), m.Name, m.Size)
		}
		r, err := m.Open()
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if b, err := io.ReadAll(r); err != nil || !bytes.Equal(b, img) {
			t.Errorf("expected the bytes of the image, got: %d bytes, %v", len(b), err)
		}
	}
	if n != 1 {
		t.Errorf("expected 1 member, got: %d", n)
	}

	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer func() { _ = root.Close() }()
	if err := walk.Extract(ctx, root, e, walk.WithMaxOutput(limit)); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	p := filepath.Join(dir, "unsparsed.img")
	if b, err := os.ReadFile(p); err != nil || !bytes.Equal(b, img) {
		t.Errorf("expected the bytes of the image on disk, got: %d bytes, %v", len(b), err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	used, ok := allocated(fi)
	if !ok || used >= fi.Size() {
		t.Skipf("the file system of %s does not report a sparse file", dir)
	}
	if used > 1<<20 {
		t.Errorf("expected at most %d bytes on disk, got: %d", 1<<20, used)
	}
}
