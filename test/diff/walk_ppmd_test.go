package diff

import (
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestWalkPPMd makes 7-Zip archives with the method PPMd, of several orders
// and sizes of memory, and compares the files of the walker with what 7z
// writes. The files hold text and binary data, and one file is larger than
// the memory of the model, so that the model fills and starts again. It
// skips without 7z.
func TestWalkPPMd(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("no 7z: install p7zip")
	}
	src := filepath.Join(t.TempDir(), "src")
	makeSevenZipTree(t, src)
	makePPMdFiles(t, src)
	for _, method := range []string{
		"PPMd",
		"PPMd:o=6:mem=16m",
		"PPMd:o=32:mem=1m",
		"PPMd:o=2:mem=64k",
		"PPMd:o=32:mem=64k",
		"PPMd:o=16:mem=256k",
	} {
		t.Run(strings.ReplaceAll(method, ":", "-"), func(t *testing.T) {
			t.Parallel()
			arc := filepath.Join(t.TempDir(), "a.7z")
			cmd := exec.Command("7z", "a", "-bd", "-snl", "-m0="+method, "-mhc=on", arc, ".")
			cmd.Dir = src
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("7z a: %v: %s", err, out)
			}
			b, err := os.ReadFile(arc)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			got := walkOne(t, b, "7zip", 0x333)
			want, _ := sevenZipTree(t, arc)
			if !slices.Equal(got, want) {
				t.Errorf("expected %q,\n got: %q", want, got)
			}
		})
	}
}

// makePPMdFiles adds to the tree under root a text of 2 MiB, with words
// that repeat as in a real text, and 600 KiB of binary data of each kind:
// bytes of every value, and a pattern that repeats with changes.
func makePPMdFiles(t *testing.T, root string) {
	t.Helper()
	rng := rand.New(rand.NewPCG(5, 6))
	words := strings.Fields("the a of to and in is it that for on was with as by at this from " +
		"magic walker archive model context symbol order memory frequency escape PPMd 7-Zip " +
		"Ünïcödé 日本語 ελληνικά")
	var text strings.Builder
	for text.Len() < 2<<20 {
		text.WriteString(words[rng.IntN(len(words))])
		if rng.IntN(12) == 0 {
			text.WriteString(".\n")
		} else {
			text.WriteByte(' ')
		}
	}
	noise := make([]byte, 600<<10)
	for i := range noise {
		noise[i] = byte(rng.UintN(256))
	}
	pattern := make([]byte, 600<<10)
	for i := range pattern {
		pattern[i] = byte(i*i>>7) ^ byte(rng.IntN(4))
	}
	for name, b := range map[string][]byte{
		"ppmd/text.txt":    []byte(text.String()),
		"ppmd/noise.bin":   noise,
		"ppmd/pattern.bin": pattern,
		"ppmd/one":         {'x'},
		"ppmd/zeros":       make([]byte, 300<<10),
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
}
