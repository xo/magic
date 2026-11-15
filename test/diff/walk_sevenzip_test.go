package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// TestWalkSevenZip makes 7-Zip archives with 7z, with each method that the
// walker decodes, and with the header compressed and not, and compares the
// files of the walker with what 7z writes. binwalk extracts 7-Zip with 7zz.
// The test needs the decoders of LZMA and xz, which the gates import. It
// skips without 7z.
func TestWalkSevenZip(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("no 7z: install p7zip")
	}
	src := filepath.Join(t.TempDir(), "src")
	makeSevenZipTree(t, src)
	for _, method := range []string{"Copy", "LZMA", "LZMA2", "Deflate", "BZip2"} {
		for _, hc := range []string{"off", "on"} {
			t.Run(method+"-hc-"+hc, func(t *testing.T) {
				t.Parallel()
				arc := filepath.Join(t.TempDir(), "a.7z")
				cmd := exec.Command("7z", "a", "-bd", "-snl", "-m0="+method, "-mhc="+hc, arc, ".")
				cmd.Dir = src
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("7z a: %v: %s", err, out)
				}
				b, err := os.ReadFile(arc)
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				got := walkOne(t, b, "7zip", 0x777)
				want, _ := sevenZipTree(t, arc)
				if !slices.Equal(got, want) {
					t.Errorf("expected %q,\n got: %q", want, got)
				}
			})
		}
	}
}
