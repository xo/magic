package diff

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xo/magic/walk"
)

// TestWalkSquashFS extracts SquashFS images that mksquashfs makes, with
// each compression, and compares the files with what unsquashfs writes.
// binwalk extracts SquashFS with sasquatch, a version of unsquashfs, so
// unsquashfs is the oracle of the files. It needs squashfs-tools, and skips
// without it.
//
// The walker cleans the target of a link, so that it stays inside the
// output, and the test compares the file that each link names.
func TestWalkSquashFS(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"mksquashfs", "unsquashfs"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s: install squashfs-tools", tool)
		}
	}
	src := filepath.Join(t.TempDir(), "src")
	makeTree(t, src)
	for _, comp := range []string{"gzip", "lzma", "lzo", "xz", "lz4", "zstd"} {
		for _, opts := range [][]string{nil, {"-no-fragments"}, {"-always-use-fragments", "-b", "4096"}} {
			name := strings.Join(append([]string{comp}, opts...), " ")
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				img := filepath.Join(dir, "image.sqsh")
				args := append([]string{src, img, "-comp", comp, "-noappend", "-all-root", "-quiet", "-no-progress"}, opts...)
				if out, err := exec.Command("mksquashfs", args...).CombinedOutput(); err != nil {
					t.Skipf("mksquashfs %s: %v: %s", name, err, out)
				}
				want := filepath.Join(dir, "want")
				if out, err := exec.Command("unsquashfs", "-q", "-n", "-d", want, img).CombinedOutput(); err != nil {
					t.Fatalf("unsquashfs: %v: %s", err, out)
				}
				exp, err := linkTree(want)
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				b, err := os.ReadFile(img)
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				const off = 0x2345
				data := append(bytes.Repeat([]byte{0xA5}, off), b...)
				data = append(data, bytes.Repeat([]byte{0xFF}, 512)...)
				var got []string
				n := 0
				for e, err := range walk.WalkBytes(context.Background(), data, walk.WithFormats("squashfs")) {
					if err != nil {
						t.Fatalf("expected no error, got: %v", err)
					}
					n++
					// mksquashfs pads the file, and the image is the bytes that
					// its superblock counts.
					used := int64(binary.LittleEndian.Uint64(b[40:]))
					if e.Offset != off || e.Size != used {
						t.Errorf("expected a match at %#x of %d bytes, got: %#x of %d", off, used, e.Offset, e.Size)
					}
					got = memberTree(t, e)
				}
				if n != 1 {
					t.Fatalf("expected one match, got: %d", n)
				}
				if !slices.Equal(got, exp) {
					t.Errorf("expected %q,\n got: %q", exp, got)
				}
			})
		}
	}
}

// makeTree writes a tree of files with the cases of the reader: files of
// zero, small, and many blocks, an executable file, links, and folders.
func makeTree(t *testing.T, root string) {
	t.Helper()
	rng := rand.New(rand.NewPCG(1, 2))
	big := make([]byte, 300<<10)
	for i := range big {
		// Data that compresses, but not to nothing.
		big[i] = byte(rng.IntN(16))
	}
	files := map[string][]byte{
		"empty":             nil,
		"small.txt":         []byte("hello, squashfs\n"),
		"bin/busybox":       big[:200<<10],
		"etc/config/a.conf": []byte(strings.Repeat("key=value\n", 500)),
		"usr/share/big":     big,
		"usr/share/odd":     big[:4097],
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

// linkTree describes the files under dir: the path, the type, the hash of
// a file, whether any bit of execution is set, and for a link, the path in
// dir that it names.
func linkTree(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			l, err := os.Readlink(p)
			if err != nil {
				return err
			}
			out = append(out, rel+" -> "+linkPath(rel, l))
		case d.IsDir():
			out = append(out, rel+"/")
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			out = append(out, fmt.Sprintf("%s %d %x %t", rel, len(b), sha256.Sum256(b), fi.Mode()&0o111 != 0))
		}
		return nil
	})
	slices.Sort(out)
	return out, err
}

// linkPath is the path, from the root, that a link at name to target names.
func linkPath(name, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(path.Clean(target), "/")
	}
	return strings.TrimPrefix(path.Clean(path.Join("/", path.Dir(name), target)), "/")
}

// memberTree describes the members of e as linkTree describes files, with
// the folder squashfs-root as the root.
func memberTree(t *testing.T, e *walk.Entry) []string {
	t.Helper()
	var out []string
	for m, err := range e.Members(context.Background()) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		rel, ok := strings.CutPrefix(m.Name, "squashfs-root/")
		if !ok {
			continue
		}
		switch {
		case m.Mode&fs.ModeSymlink != 0:
			out = append(out, rel+" -> "+linkPath(rel, m.Link))
		case m.Mode.IsDir():
			out = append(out, rel+"/")
		default:
			r, _ := m.Open()
			b, _ := io.ReadAll(r)
			out = append(out, fmt.Sprintf("%s %d %x %t", rel, len(b), sha256.Sum256(b), m.Mode&0o111 != 0))
		}
	}
	slices.Sort(out)
	return out
}
