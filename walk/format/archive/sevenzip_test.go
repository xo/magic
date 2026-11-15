//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package archive

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// TestSevenZip makes 7-Zip archives with 7z, with the methods that the
// walker decodes without the decoders of compress, and compares the members
// with the files. The test in test/diff covers LZMA, LZMA2 and the headers
// that are compressed. It skips without 7z, and on Windows, where 7z stores
// a link as a reparse point of Windows and not as a link of Unix.
func TestSevenZip(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("7z on Windows stores links as reparse points")
	}
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("no 7z: install 7-Zip")
	}
	src := t.TempDir()
	for name, content := range map[string]string{
		"a.txt":       "hello, 7-zip\n",
		"d/n.txt":     strings.Repeat("0123456789\n", 30),
		"d/e/empty":   "",
		"run.sh":      "#!/bin/sh\n",
		"d/up/target": "t",
	} {
		p := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
	if err := os.Chmod(filepath.Join(src, "run.sh"), 0o755); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(src, "void"), 0o755); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	for link, target := range map[string]string{"ln": "a.txt", "d/bad": "../a.txt"} {
		if err := os.Symlink(target, filepath.Join(src, link)); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
	for _, method := range []string{"Copy", "Deflate", "BZip2"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			arc := filepath.Join(t.TempDir(), "a.7z")
			cmd := exec.Command("7z", "a", "-bd", "-snl", "-m0="+method, "-mhc=off", arc, ".")
			cmd.Dir = src
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("7z a: %v: %s", err, out)
			}
			b, err := os.ReadFile(arc)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			e, lines := walkOne(t, slices.Concat(noise(500), b, noise(50)), "7zip")
			if e.Offset != 500 || e.Size != int64(len(b)) || e.Confidence != walk.High {
				t.Errorf("expected %q, got: %q", fmt.Sprint(500, len(b), walk.High), fmt.Sprint(e.Offset, e.Size, e.Confidence))
			}
			if exp := fmt.Sprintf("7-zip archive data, version 0.4, total size: %d bytes", len(b)); e.Description != exp {
				t.Errorf("expected %q, got: %q", exp, e.Description)
			}
			slices.Sort(lines)
			// A link with a ".." part is an empty file, as 7zz writes it.
			check(t, lines,
				"a.txt -rw-r--r-- hello, 7-zip\n",
				"d drwxr-xr-x",
				"d/bad -rw-r--r-- ",
				"d/e drwxr-xr-x",
				"d/e/empty -rw-r--r-- ",
				"d/n.txt -rw-r--r-- "+strings.Repeat("0123456789\n", 30),
				"d/up drwxr-xr-x",
				"d/up/target -rw-r--r-- t",
				"ln Lrwxrwxrwx -> a.txt",
				"run.sh -rw-r--r-x #!/bin/sh\n",
				"void drwxr-xr-x",
			)
		})
	}
}

// TestSevenZipCRC holds that a next header whose CRC does not match is not
// a match.
func TestSevenZipCRC(t *testing.T) {
	t.Parallel()
	next := []byte{szHeaderID, szEnd}
	h := []byte("7z\xbc\xaf\x27\x1c\x00\x04")
	tail := binary.LittleEndian.AppendUint64(nil, 0)
	tail = binary.LittleEndian.AppendUint64(tail, uint64(len(next)))
	tail = binary.LittleEndian.AppendUint32(tail, binwalk.CRC32(next)^1)
	h = binary.LittleEndian.AppendUint32(h, binwalk.CRC32(tail))
	data := slices.Concat(noise(10), h, tail, next, noise(10))
	if _, err := parseSevenZip(data, 10); err == nil {
		t.Errorf("expected %q, got: %q", walk.ErrNoMatch, "a match")
	}
	binary.LittleEndian.PutUint32(tail[16:], binwalk.CRC32(next))
	binary.LittleEndian.PutUint32(h[8:], binwalk.CRC32(tail))
	data = slices.Concat(noise(10), h, tail, next, noise(10))
	m, err := parseSevenZip(data, 10)
	if err != nil || m.Size != 34 {
		t.Errorf("expected %q, got: %q", "34", fmt.Sprint(m.Size, err))
	}
}
