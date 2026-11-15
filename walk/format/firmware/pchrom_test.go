//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package firmware_test

import (
	"context"
	"testing"

	"github.com/xo/magic/walk"
	_ "github.com/xo/magic/walk/format/firmware"
)

// TestPCHROM finds a PCH ROM in an image that the test makes in memory.
// binwalk-ng gives the expected values for the same bytes.
func TestPCHROM(t *testing.T) {
	t.Parallel()
	const (
		start = 8
		size  = 0x20000 + 16
	)
	data := make([]byte, start+size+8)
	// The magic, then FCBA 3, NC 1 and FRBA 4.
	copy(data[start+16:], "\x5a\xa5\xf0\x0f\x03\x01\x04\x00")
	// The five region entries, at FCBA shifted left by 4. The first ends at
	// 0x20000. The third and the fifth have a base after their limit, and
	// give no region.
	copy(data[start+0x30:], "\x00\x00\x10\x00\x01\x00\x00\x00\xff\x1f\x00\x00"+
		"\x00\x00\x00\x00\xff\x7f\x00\x00")
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
