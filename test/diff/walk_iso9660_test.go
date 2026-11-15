package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestWalkISO9660 extracts ISO 9660 images that xorrisofs and mkisofs make,
// with and without Rock Ridge and Joliet, and compares the files with the
// tree that made them. 7-Zip takes the names of Joliet when the image has
// them, and the names, modes and links of Rock Ridge otherwise. binwalk
// extracts ISO 9660 with 7zz, so the test also compares the files with
// what 7-Zip writes, when 7zz or 7z is on PATH. That is the only check of
// an image with neither, whose names are upper case, and of an image with
// a boot image of El Torito. It skips a tool that is not on PATH.
func TestWalkISO9660(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "src")
	makeSevenZipTree(t, src)
	// A boot image of El Torito, which 7-Zip writes to "[BOOT]".
	boot := make([]byte, 5000)
	for i := range boot {
		boot[i] = byte(i * 7)
	}
	if err := os.WriteFile(filepath.Join(src, "boot.img"), boot, 0o644); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	tests := []struct {
		tool   string
		args   []string
		joliet bool // the names are Joliet
		tree   bool // the files are the tree
	}{
		{"xorrisofs", []string{"-R"}, false, true},
		{"xorrisofs", []string{"-R", "-J"}, true, true},
		{"xorrisofs", []string{"-r", "-J", "-iso-level", "3"}, true, true},
		{"xorrisofs", []string{"-R", "-b", "boot.img", "-no-emul-boot", "-boot-load-size", "4"}, false, false},
		{"mkisofs", nil, false, false},
		{"mkisofs", []string{"-J"}, true, true},
		{"mkisofs", []string{"-R"}, false, true},
		// mkisofs puts each link in the Joliet tree as an empty file.
		{"mkisofs", []string{"-R", "-J"}, true, false},
		{"mkisofs", []string{"-iso-level", "2"}, false, false},
	}
	for _, test := range tests {
		name := strings.Join(append([]string{test.tool}, test.args...), " ")
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := exec.LookPath(test.tool); err != nil {
				t.Skipf("no %s: install xorriso or cdrtools", test.tool)
			}
			dir := t.TempDir()
			img := filepath.Join(dir, "image.iso")
			args := append([]string{"-quiet", "-o", img}, test.args...)
			if out, err := exec.Command(test.tool, append(args, src)...).CombinedOutput(); err != nil {
				t.Skipf("%s: %v: %s", name, err, out)
			}
			b, err := os.ReadFile(img)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			got := walkOne(t, b, "iso9660", 0x10001)
			if test.tree {
				exp, err := sevenZipExpected(src, test.joliet)
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				if !slices.Equal(got, exp) {
					t.Errorf("expected %q,\n got: %q", exp, got)
				}
			}
			if want, ok := sevenZipTree(t, img); ok && !slices.Equal(got, want) {
				t.Errorf("expected %q,\n got: %q", want, got)
			}
		})
	}
}
