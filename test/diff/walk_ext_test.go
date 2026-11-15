package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestWalkEXT extracts EXT images that mkfs.ext4 makes from a tree, with
// extents and with the block maps of ext2 and ext3, and compares the files
// with the tree. binwalk extracts EXT with tsk_recover -a, which writes each
// allocated file that is not empty under rootfs, and makes only the folders
// that such a file needs. It needs e2fsprogs, and skips without it.
func TestWalkEXT(t *testing.T) {
	t.Parallel()
	mkfs, err := exec.LookPath("mkfs.ext4")
	if err != nil {
		t.Skip("no mkfs.ext4: install e2fsprogs")
	}
	src := filepath.Join(t.TempDir(), "src")
	makeTSKTree(t, src, true)
	exp := tskWant(t, src)
	for _, opts := range [][]string{
		{"-b", "4096"},
		{"-b", "1024"},
		{"-b", "1024", "-O", "^extent,^64bit"},
		{"-b", "4096", "-O", "^extent,^64bit,^has_journal"},
		{"-t", "ext2", "-b", "1024"},
		{"-t", "ext3", "-b", "2048"},
		{"-b", "4096", "-O", "^extent,^64bit,^filetype"},
		{"-b", "1024", "-I", "256", "-O", "inline_data"},
		{"-b", "4096", "-O", "64bit"},
	} {
		name := strings.Join(opts, " ")
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			img := filepath.Join(t.TempDir(), "image.ext")
			args := append([]string{"-q", "-F", "-E", "root_owner=0:0", "-d", src}, opts...)
			args = append(args, img, "8M")
			if out, err := exec.Command(mkfs, args...).CombinedOutput(); err != nil {
				t.Skipf("mkfs.ext4 %s: %v: %s", name, err, out)
			}
			b, err := os.ReadFile(img)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			const off = 0x2345
			got := tskWalk(t, tskPad(b, off), "ext", off, int64(len(b)))
			if !slices.Equal(got, exp) {
				t.Errorf("expected %q,\n got: %q", exp, got)
			}
		})
	}
}
