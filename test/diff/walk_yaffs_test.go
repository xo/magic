package diff

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xo/magic/walk"
)

// TestWalkYAFFS walks YAFFS2 images that yaffsWriter makes from a known
// tree, and compares the members with the tree. binwalk extracts YAFFS2
// with unyaffs, which the test does not run: the tree is what unyaffs
// writes from these images. unyaffs reads only little endian images of
// pages of 2 KiB or more, so binwalk finds the other images and does not
// extract them.
func TestWalkYAFFS(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		page  int
		spare int
		bad   bool
		big   bool
	}{
		{"2048-64", 2048, 64, false, false},
		{"4096-128-bad-block", 4096, 128, true, false},
		{"2048-64-big", 2048, 64, false, true},
		{"1024-32", 1024, 32, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var order byteOrder = binary.LittleEndian
			endian := "little"
			if tc.big {
				order, endian = binary.BigEndian, "big"
			}
			img, exp := yaffsSample(order, tc.page, tc.spare, tc.bad)
			rng := rand.New(rand.NewPCG(7, 8))
			const off = 0x2345
			data := make([]byte, off)
			for i := range data {
				data[i] = byte(rng.Uint32())
			}
			data = append(data, img...)
			// Erased pages after the image. binwalk needs room for two
			// chunks of the largest page after the start.
			data = append(data, bytes.Repeat([]byte{0xFF}, 16896)...)
			n := 0
			for e, err := range walk.WalkBytes(context.Background(), data, walk.WithFormats("yaffs")) {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				n++
				desc := fmt.Sprintf("YAFFSv2 filesystem, %s endian, page size: %d, spare size: %d, image size: %d bytes", endian, tc.page, tc.spare, len(img))
				if e.Offset != off || e.Size != int64(len(img)) || e.Description != desc {
					t.Errorf("expected %q at %#x, got: %q at %#x", desc, off, e.Description, e.Offset)
				}
				if tc.big || tc.page < 2048 {
					for _, err := range e.Members(context.Background()) {
						if !errors.Is(err, walk.ErrNotExtracted) {
							t.Errorf("expected %v, got: %v", walk.ErrNotExtracted, err)
						}
					}
					continue
				}
				if got := membersBelow(t, e, "yaffs-root/"); !slices.Equal(got, exp) {
					t.Errorf("expected %q,\n got: %q", exp, got)
				}
			}
			if n != 1 {
				t.Fatalf("expected one match, got: %d", n)
			}
		})
	}
}

// TestWalkYAFFSBinwalk extracts yaffs2.bin, the test input of binwalk. The
// image holds 13 files of the source of the tools of YAFFS in its root
// folder, each with data. It skips without the build of binwalk.
func TestWalkYAFFSBinwalk(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("..", ".binwalk", "src", "tests", "inputs", "yaffs2.bin"))
	if err != nil {
		t.Skipf("no yaffs2.bin: %v", err)
	}
	n := 0
	for e, err := range walk.WalkBytes(context.Background(), b, walk.WithFormats("yaffs")) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		n++
		files := 0
		for m, err := range e.Members(context.Background()) {
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if !m.Mode.IsRegular() {
				continue
			}
			files++
			if !strings.HasPrefix(m.Name, "yaffs-root/") || m.Size == 0 {
				t.Errorf("expected a file under yaffs-root, got: %q of %d bytes", m.Name, m.Size)
			}
		}
		if files != 13 {
			t.Errorf("expected 13 files, got: %d", files)
		}
	}
	if n != 1 {
		t.Fatalf("expected one match, got: %d", n)
	}
}

// yaffsSample makes an image of the known tree, and returns the lines of
// membersBelow for it.
func yaffsSample(order byteOrder, page, spare int, bad bool) ([]byte, []string) {
	rng := rand.New(rand.NewPCG(9, 10))
	busybox := make([]byte, 5000)
	for i := range busybox {
		busybox[i] = byte(rng.Uint32())
	}
	conf := []byte(strings.Repeat("key=value\n", 400))
	small := []byte("hello, yaffs\n")
	w := &yaffsWriter{order: order, page: page, spare: spare, bad: bad}
	const (
		root = 1
		bin  = 0x101
		etc  = 0x102
		cfg  = 0x103
		dev  = 0x10B
	)
	w.header(bin, root, 3, "bin", 0o40755, 0, 0, "", 0)
	w.header(etc, root, 3, "etc", 0o40755, 0, 0, "", 0)
	w.header(cfg, etc, 3, "config", 0o40755, 0, 0, "", 0)
	w.file(0x104, root, "small.txt", 0o100644, small)
	w.file(0x105, bin, "busybox", 0o100755, busybox)
	w.header(0x106, bin, 2, "ls", 0o120777, 0, 0, "busybox", 0)
	w.header(0x107, root, 2, "sh", 0o120777, 0, 0, "/bin/busybox", 0)
	w.file(0x108, cfg, "a.conf", 0o100644, conf)
	w.file(0x109, root, "empty", 0o100644, nil)
	w.header(0x10A, etc, 4, "hard", 0, 0, 0x104, "", 0)
	w.header(dev, root, 3, "dev", 0o40755, 0, 0, "", 0)
	w.header(0x10C, dev, 5, "null", 0o20666, 0, 0, "", 1<<8|3)
	w.header(0x10D, etc, 2, "up", 0o120777, 0, 0, "../small.txt", 0)
	exp := []string{
		"bin/",
		fileLine("bin/busybox", busybox, true),
		"bin/ls -> " + linkPath("bin/ls", "busybox"),
		"dev/",
		fileLine("dev/null", []byte("c 1 3"), false),
		fileLine("empty", nil, false),
		"etc/",
		"etc/config/",
		fileLine("etc/config/a.conf", conf, false),
		fileLine("etc/hard", small, false),
		"etc/up -> " + linkPath("etc/up", "../small.txt"),
		"sh -> " + linkPath("sh", "/bin/busybox"),
		fileLine("small.txt", small, false),
	}
	slices.Sort(exp)
	return w.buf, exp
}

// yaffsWriter writes the chunks of a YAFFS2 image as mkyaffs2image does:
// a page, then its spare with the packed tags, after the two bytes of the
// mark of a bad block when bad is set.
type yaffsWriter struct {
	order byteOrder
	page  int
	spare int
	bad   bool
	buf   []byte
}

// chunk writes a page of data, padded with 0xFF, and its tags.
func (w *yaffsWriter) chunk(data []byte, obj, chunkID, count uint32) {
	p := bytes.Repeat([]byte{0xFF}, w.page)
	copy(p, data)
	s := bytes.Repeat([]byte{0xFF}, w.spare)
	o := 0
	if w.bad {
		o = 2
	}
	t := w.order.AppendUint32(nil, 0x1000)
	t = w.order.AppendUint32(t, obj)
	t = w.order.AppendUint32(t, chunkID)
	t = w.order.AppendUint32(t, count)
	copy(s[o:], t)
	w.buf = append(append(w.buf, p...), s...)
}

// header writes the object header of an object.
func (w *yaffsWriter) header(id, parent, typ uint32, name string, mode uint32, size int, eq uint32, alias string, rdev uint32) {
	o := w.order
	h := make([]byte, 512)
	o.PutUint32(h, typ)
	o.PutUint32(h[4:], parent)
	o.PutUint16(h[8:], 0xFFFF)
	copy(h[10:266], name)
	o.PutUint32(h[268:], mode)
	o.PutUint32(h[292:], uint32(size))
	o.PutUint32(h[296:], eq)
	copy(h[300:460], alias)
	o.PutUint32(h[460:], rdev)
	w.chunk(h, id, 0, 0xFFFF)
}

// file writes the header of a file and its data chunks.
func (w *yaffsWriter) file(id, parent uint32, name string, mode uint32, data []byte) {
	w.header(id, parent, 1, name, mode, len(data), 0, "", 0)
	for i := 0; len(data) > 0; i++ {
		c := data[:min(w.page, len(data))]
		data = data[len(c):]
		w.chunk(c, id, uint32(i+1), uint32(len(c)))
	}
}
