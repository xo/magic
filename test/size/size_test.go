// Package size measures the bytes that the port adds to a program (D38).
package size

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The growth of a program with the cgo package v0.4.0 on linux/amd64, which
// D38 measured. The port must add no more.
const (
	baselineStripped = 995488
	baselineFull     = 1662342
)

// build builds the program in testdata/name with flags, and returns its
// size.
func build(t *testing.T, name string, flags ...string) int64 {
	t.Helper()
	out := filepath.Join(t.TempDir(), name)
	args := append([]string{"build", "-trimpath", "-o", out}, flags...)
	cmd := exec.Command("go", append(args, "./testdata/"+name)...)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building %s: %v\n%s", name, err, b)
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

// TestPortSize holds D38: a program that prints the MIME type of a file with
// the port grows by no more bytes than with the cgo package. The baseline is
// for linux/amd64, so the test runs only there.
func TestPortSize(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two programs")
	}
	if os.Getenv("GOOS") != "" && os.Getenv("GOOS") != "linux" || os.Getenv("GOARCH") != "" && os.Getenv("GOARCH") != "amd64" {
		t.Skip("the baseline is for linux/amd64")
	}
	for _, c := range []struct {
		name     string
		flags    []string
		baseline int64
	}{
		{"stripped", []string{"-ldflags=-s -w"}, baselineStripped},
		{"full", nil, baselineFull},
	} {
		growth := build(t, "port", c.flags...) - build(t, "base", c.flags...)
		t.Logf("%s: the port adds %d bytes, the cgo package %d", c.name, growth, c.baseline)
		if growth > c.baseline {
			t.Errorf("%s: expected at most %d bytes, got: %d", c.name, c.baseline, growth)
		}
	}
}
