//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package firmware_test

import (
	"context"
	"testing"

	"github.com/xo/magic/walk"
	_ "github.com/xo/magic/walk/format/firmware"
)

// TestPCHROM finds a PCH ROM. binwalk finds one only in more than 16 MiB, so
// the samples of test/corpus/testdata/walk cannot hold one, and the test
// makes the image in memory. binwalk gives the expected values for the same
// bytes.
func TestPCHROM(t *testing.T) {
	t.Parallel()
	const (
		start = 8
		size  = 0x1000000 + 16
	)
	data := make([]byte, start+size+8)
	// The five region entries, the last of which is the magic, and then
	// FCBA 3, NC 1 and FRBA 4.
	copy(data[start:], "\x01\x00\x03\x00\x02\x00\x05\x00\x00\x00\x00\x00\x10\x00\x10\x00"+
		"\x5a\xa5\xf0\x0f\x03\x01\x04\x00")
	var got []string
	for e, err := range walk.WalkBytes(context.Background(), data) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if e.Format != "pchrom" {
			continue
		}
		if e.Offset != start || e.Size != size || e.Confidence != walk.Low {
			t.Errorf("expected offset %d, size %d and low confidence, got: %d, %d and %v", start, size, e.Offset, e.Size, e.Confidence)
		}
		got = append(got, e.Description)
	}
	if len(got) != 1 || got[0] != "Intel serial flash for PCH ROM" {
		t.Errorf("expected %q, got: %q", []string{"Intel serial flash for PCH ROM"}, got)
	}
}
