package diff

import (
	"bytes"
	"context"
	"crypto/sha256"
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

// TestWalkFAT extracts FAT12, FAT16 and FAT32 images that mkfs.vfat and
// mcopy make, and compares the files with the tree that they hold. binwalk
// extracts FAT with tsk_recover -a, which writes each allocated file that
// is not empty under rootfs, and makes only the folders that such a file
// needs. It needs mtools and dosfstools, and skips without them.
func TestWalkFAT(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"mkfs.vfat", "mcopy"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s: install dosfstools and mtools", tool)
		}
	}
	src := filepath.Join(t.TempDir(), "src")
	makeTSKTree(t, src, false)
	exp := tskWant(t, src)
	for _, tc := range []struct {
		bits string
		kb   int
		opts []string
	}{
		{"12", 2048, []string{"-s", "4"}},
		{"16", 16 << 10, nil},
		{"32", 40 << 10, []string{"-s", "1"}},
	} {
		t.Run("FAT"+tc.bits, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			img := filepath.Join(dir, "image.fat")
			args := append([]string{"-C", "-F", tc.bits}, tc.opts...)
			args = append(args, img, fmt.Sprint(tc.kb))
			if out, err := exec.Command("mkfs.vfat", args...).CombinedOutput(); err != nil {
				t.Skipf("mkfs.vfat: %v: %s", err, out)
			}
			entries, err := os.ReadDir(src)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			margs := make([]string, 0, len(entries)+5)
			margs = append(margs, "-s", "-p", "-i", img)
			for _, e := range entries {
				margs = append(margs, filepath.Join(src, e.Name()))
			}
			margs = append(margs, "::/")
			cmd := exec.Command("mcopy", margs...)
			cmd.Env = append(os.Environ(), "MTOOLS_SKIP_CHECK=1")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("mcopy: %v: %s", err, out)
			}
			b, err := os.ReadFile(img)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			// fat is a short signature, which binwalk finds only at the
			// start of a file.
			rng := rand.New(rand.NewPCG(3, 4))
			tail := make([]byte, 4096)
			for i := range tail {
				tail[i] = byte(rng.Uint32())
			}
			data := append(slices.Clone(b), tail...)
			got := tskWalk(t, data, "fat", 0, int64(len(b)))
			if !slices.Equal(got, exp) {
				t.Errorf("expected %q,\n got: %q", exp, got)
			}
		})
	}
}

// makeTSKTree writes a tree of files with the cases of the readers of
// tsk_recover: empty files, small files, files of many clusters or blocks,
// an executable, long names, nested folders and an empty folder. With
// links, it adds links with a short target, which EXT keeps in the inode,
// and with a long target, which EXT keeps in a block.
func makeTSKTree(t *testing.T, root string, links bool) {
	t.Helper()
	rng := rand.New(rand.NewPCG(1, 2))
	big := make([]byte, 600<<10)
	for i := range big {
		big[i] = byte(rng.Uint32())
	}
	files := map[string][]byte{
		"empty":                                nil,
		"small.txt":                            []byte("hello, tsk_recover\n"),
		"bin/busybox":                          big[:200<<10],
		"etc/config/a.conf":                    []byte(strings.Repeat("key=value\n", 500)),
		"usr/share/big":                        big,
		"usr/share/odd":                        big[:4097],
		"A Long File Name With Spaces.txt":     []byte("long\n"),
		"Docs/Mixed Case Folder/ReadMe.Md":     []byte("# read me\n"),
		"deep/a/b/c/d/e/f/leaf":                big[:1025],
		"deep/a/b/c/d/a name that is long.bin": big[:70000],
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
	if err := os.MkdirAll(filepath.Join(root, "var/empty"), 0o755); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !links {
		return
	}
	for link, target := range map[string]string{
		"sbin/sh":  "/bin/busybox",
		"bin/ls":   "busybox",
		"etc/long": "../usr/share/" + strings.Repeat("a-long-target-", 6),
	} {
		p := filepath.Join(root, link)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if err := os.Symlink(target, p); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
}

// tskWant describes what tsk_recover -a writes for the tree under dir: each
// file that is not empty under rootfs, with no bit of execution, and the
// folders above it. A link is a file: tsk_recover reads its content, which
// is the target when EXT keeps it in a block, and zeros when EXT keeps it
// in the inode.
func tskWant(t *testing.T, dir string) []string {
	t.Helper()
	set := map[string]bool{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = "rootfs/" + filepath.ToSlash(rel)
		var b []byte
		if d.Type()&fs.ModeSymlink != 0 {
			l, err := os.Readlink(p)
			if err != nil {
				return fmt.Errorf("reading link %s: %w", p, err)
			}
			b = []byte(l)
			if len(l) < 60 {
				b = make([]byte, len(l))
			}
		} else if b, err = os.ReadFile(p); err != nil {
			return fmt.Errorf("reading %s: %w", p, err)
		}
		if len(b) == 0 {
			return nil
		}
		set[tskLine(rel, b, false)] = true
		for q := path.Dir(rel); q != "."; q = path.Dir(q) {
			set[q+"/"] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// tskLine describes a file: its path, its size, the hash of its bytes, and
// whether a bit of execution is set.
func tskLine(name string, b []byte, exec bool) string {
	return fmt.Sprintf("%s %d %x %t", name, len(b), sha256.Sum256(b), exec)
}

// tskWalk walks data with the format name, expects one match at off of
// size bytes, and describes its members as tskWant does.
func tskWalk(t *testing.T, data []byte, name string, off, size int64) []string {
	t.Helper()
	n := 0
	var out []string
	for e, err := range walk.WalkBytes(context.Background(), data, walk.WithFormats(name)) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		n++
		if e.Offset != off || e.Size != size {
			t.Errorf("expected a match at %#x of %d bytes, got: %#x of %d", off, size, e.Offset, e.Size)
		}
		for m, err := range e.Members(context.Background()) {
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			switch {
			case m.Mode&fs.ModeSymlink != 0:
				out = append(out, m.Name+" -> "+m.Link)
			case m.Mode.IsDir():
				out = append(out, m.Name+"/")
			default:
				r, _ := m.Open()
				b, _ := io.ReadAll(r)
				out = append(out, tskLine(m.Name, b, m.Mode&0o111 != 0))
			}
		}
	}
	if n != 1 {
		t.Fatalf("expected one match, got: %d", n)
	}
	slices.Sort(out)
	return out
}

// tskPad puts b after pad random bytes, and random bytes after it.
func tskPad(b []byte, pad int) []byte {
	rng := rand.New(rand.NewPCG(5, 6))
	out := make([]byte, pad, pad+len(b)+1000)
	for i := range out {
		out[i] = byte(rng.Uint32())
	}
	return append(append(out, b...), bytes.Repeat([]byte{0xA5}, 1000)...)
}
