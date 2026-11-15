//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package doc_test

import (
	"context"
	"encoding/hex"
	"io"
	"testing"

	"github.com/xo/magic/walk"
	_ "github.com/xo/magic/walk/format/doc"
)

// srecHeader is a header record that holds "HDR". The signature
// srecord_generic needs a header record at offset 0, and the tests of
// srec.rs of binwalk-ng call the parser and the extractor directly, with no
// header.
const srecHeader = "S00600004844521B\n"

// TestSRecordDecodes decodes the S-records of the tests of srec.rs of
// binwalk-ng, after a header record, and expects the same bytes.
func TestSRecordDecodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, srec, want string
	}{
		{
			"a gap between records",
			"S107000000010203F2\nS107000804050607DA\nS9030000FC\n",
			"000102030000000004050607",
		},
		{
			"a base address that is not zero",
			"S107001000010203E2\nS107001C04050607C6\nS9030000FC\n",
			"0000000000000000000000000000000000010203000000000000000004050607",
		},
		{
			"no terminal record",
			"S107000000010203F2\nS107000804050607DA\n",
			"000102030000000004050607",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := srecMember(t, []byte(srecHeader+test.srec))
			if got != test.want {
				t.Errorf("expected %q, got: %q", test.want, got)
			}
		})
	}
}

// TestSRecordRejects gives S-records that binwalk-ng rejects, after a header
// record: a bad checksum, a cut record, and the type S4, which the standard
// does not define.
func TestSRecordRejects(t *testing.T) {
	t.Parallel()
	for _, srec := range []string{
		"S107000000010203F3\nS9030000FC\n",
		"S1\nS9030000FC\n",
		"S407000000010203F2\nS9030000FC\n",
	} {
		for e, err := range walk.WalkBytes(context.Background(), []byte(srecHeader+srec), walk.WithFormats("srecord_generic")) {
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if e.Format == "srecord_generic" {
				t.Errorf("expected no match, got: %q", e.Description)
			}
		}
	}
}

// srecMember walks data, and returns the member s-record.bin of the match
// srecord_generic, in hex.
func srecMember(t *testing.T, data []byte) string {
	t.Helper()
	ctx := context.Background()
	for e, err := range walk.WalkBytes(ctx, data, walk.WithFormats("srecord_generic")) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if e.Format != "srecord_generic" {
			continue
		}
		if e.Size != int64(len(data)) {
			t.Errorf("expected size %d, got: %d", len(data), e.Size)
		}
		for m, err := range e.Members(ctx) {
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if m.Name != "s-record.bin" {
				continue
			}
			r, err := m.Open()
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			b, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			return hex.EncodeToString(b)
		}
	}
	t.Fatal("expected a member s-record.bin, got none")
	return ""
}
