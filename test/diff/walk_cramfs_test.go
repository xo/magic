package diff

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xo/magic/walk"
)

// TestWalkCramFS extracts CramFS images that mkfs.cramfs makes, in both
// byte orders, and compares the files with the tree that made them. binwalk
// extracts CramFS with 7zz, so the test also compares the files with what
// 7-Zip writes, when 7zz or 7z is on PATH. It skips without mkfs.cramfs.
func TestWalkCramFS(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("mkfs.cramfs"); err != nil {
		t.Skip("no mkfs.cramfs: install util-linux")
	}
	src := filepath.Join(t.TempDir(), "src")
	makeSevenZipTree(t, src)
	exp, err := sevenZipExpected(src, false)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	for _, endian := range []string{"little", "big"} {
		t.Run(endian, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			img := filepath.Join(dir, "image.cramfs")
			if out, err := exec.Command("mkfs.cramfs", "-N", endian, src, img).CombinedOutput(); err != nil {
				t.Skipf("mkfs.cramfs -N %s: %v: %s", endian, err, out)
			}
			b, err := os.ReadFile(img)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			got := walkOne(t, b, "cramfs", 0x1235)
			if !slices.Equal(got, exp) {
				t.Errorf("expected %q,\n got: %q", exp, got)
			}
			if want, ok := sevenZipTree(t, img); ok && !slices.Equal(got, want) {
				t.Errorf("expected %q,\n got: %q", want, got)
			}
		})
	}
}

// walkOne puts b after off random bytes and before 512 more, walks it for
// format, and describes the members of the one match, which must start at
// off and hold b.
func walkOne(t *testing.T, b []byte, format string, off int) []string {
	t.Helper()
	rng := rand.New(rand.NewPCG(uint64(off), uint64(len(b))))
	data := make([]byte, off, off+len(b)+512)
	for i := range data {
		data[i] = byte(rng.UintN(256))
	}
	data = append(data, b...)
	data = append(data, bytes.Repeat([]byte{0xFF}, 512)...)
	var got []string
	n := 0
	for e, err := range walk.WalkBytes(context.Background(), data, walk.WithFormats(format)) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		n++
		if e.Offset != int64(off) || e.Size != int64(len(b)) {
			t.Errorf("expected a match at %#x of %d bytes, got: %#x of %d", off, len(b), e.Offset, e.Size)
		}
		got = sevenZipMembers(t, e)
	}
	if n != 1 {
		t.Fatalf("expected one match, got: %d", n)
	}
	return got
}

// makeSevenZipTree writes a tree of files: files of zero, small, and many
// blocks, an executable file, folders, an empty folder, and links. One link
// goes up with "..", which 7-Zip writes as an empty file.
func makeSevenZipTree(t *testing.T, root string) {
	t.Helper()
	rng := rand.New(rand.NewPCG(3, 4))
	big := make([]byte, 200<<10)
	for i := range big {
		big[i] = byte(rng.IntN(16))
	}
	files := map[string][]byte{
		"empty":                nil,
		"small.txt":            []byte("hello, 7-zip\n"),
		"bin/busybox":          big[:100<<10],
		"etc/config/a.conf":    []byte(strings.Repeat("key=value\n", 500)),
		"usr/share/big":        big,
		"usr/share/odd":        big[:4097],
		"usr/share/block":      big[:4096],
		"Long Mixed Name.Data": []byte("long name\n"),
	}
	for name, b := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
	if err := os.Chmod(filepath.Join(root, "bin/busybox"), 0o755); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	for _, d := range []string{"var/empty", "sbin"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
	for link, target := range map[string]string{
		"sbin/sh":    "/bin/busybox",
		"bin/ls":     "busybox",
		"etc/up":     "../usr/share/odd",
		"etc/broken": "/nowhere",
	} {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
}

// sevenZipExpected describes the tree under dir as 7-Zip extracts it from
// an image: a link whose target has a ".." part is an empty file. A Joliet
// image has no links and no modes, so with joliet set, links are left out
// and no file is executable.
func sevenZipExpected(dir string, joliet bool) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			if joliet {
				return nil
			}
			l, err := os.Readlink(p)
			if err != nil {
				return fmt.Errorf("reading link %s: %w", p, err)
			}
			if slices.Contains(strings.Split(l, "/"), "..") {
				out = append(out, sevenZipFileLine(rel, nil, false))
			} else {
				out = append(out, rel+" -> "+linkPath(rel, l))
			}
		case d.IsDir():
			out = append(out, rel+"/")
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return fmt.Errorf("reading %s: %w", p, err)
			}
			fi, err := d.Info()
			if err != nil {
				return fmt.Errorf("reading the mode of %s: %w", p, err)
			}
			out = append(out, sevenZipFileLine(rel, b, !joliet && fi.Mode()&0o111 != 0))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", dir, err)
	}
	slices.Sort(out)
	return out, nil
}

// sevenZipFileLine describes a file: the path, the size, the hash and
// whether any bit of execution is set.
func sevenZipFileLine(rel string, b []byte, exec bool) string {
	return fmt.Sprintf("%s %d %x %t", rel, len(b), sha256.Sum256(b), exec)
}

// sevenZipTree extracts img with 7zz, or with 7z, and describes the files
// that it writes, as sevenZipExpected does. 7-Zip writes an absolute link
// to the path inside its output. It reports false when neither is on PATH.
func sevenZipTree(t *testing.T, img string) ([]string, bool) {
	t.Helper()
	tool := ""
	for _, name := range []string{"7zz", "7z"} {
		if _, err := exec.LookPath(name); err == nil {
			tool = name
			break
		}
	}
	if tool == "" {
		return nil, false
	}
	dir := filepath.Join(t.TempDir(), "7z")
	out, err := exec.Command(tool, "x", "-y", "-o"+dir, "-p''", img).CombinedOutput()
	// 7-Zip exits with 2 after an error of an item, such as a dangerous
	// link, and binwalk accepts it.
	var ee *exec.ExitError
	if err != nil && (!errors.As(err, &ee) || ee.ExitCode() != 2) {
		t.Fatalf("%s x: %v: %s", tool, err, out)
	}
	var lines []string
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			l, err := os.Readlink(p)
			if err != nil {
				return fmt.Errorf("reading link %s: %w", p, err)
			}
			if r, ok := strings.CutPrefix(l, dir+"/"); ok {
				l = "/" + r
			}
			lines = append(lines, rel+" -> "+linkPath(rel, l))
		case d.IsDir():
			lines = append(lines, rel+"/")
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return fmt.Errorf("reading %s: %w", p, err)
			}
			fi, err := d.Info()
			if err != nil {
				return fmt.Errorf("reading the mode of %s: %w", p, err)
			}
			lines = append(lines, sevenZipFileLine(rel, b, fi.Mode()&0o111 != 0))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	slices.Sort(lines)
	return lines, true
}

// sevenZipMembers describes the members of e as sevenZipExpected describes
// files.
func sevenZipMembers(t *testing.T, e *walk.Entry) []string {
	t.Helper()
	var out []string
	for m, err := range e.Members(context.Background()) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		switch {
		case m.Mode&fs.ModeSymlink != 0:
			out = append(out, m.Name+" -> "+linkPath(m.Name, m.Link))
		case m.Mode.IsDir():
			out = append(out, m.Name+"/")
		default:
			r, err := m.Open()
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			b, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			out = append(out, sevenZipFileLine(m.Name, b, m.Mode&0o111 != 0))
		}
	}
	slices.Sort(out)
	return out
}
