package diff

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xo/magic/walk"
)

// TestWalkSquashFS3 extracts images of SquashFS 1.x, 2.x and 3.x that
// sq3Image makes, in both byte orders, and compares the files with what
// sasquatch writes. binwalk extracts SquashFS with sasquatch, so it is the
// oracle of the files. The test skips without it.
func TestWalkSquashFS3(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("sasquatch"); err != nil {
		t.Skip("no sasquatch: install it from github.com/onekey-sec/sasquatch")
	}
	for _, v := range sq3Variants {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			b := sq3Image(v)
			dir := t.TempDir()
			img := filepath.Join(dir, "image.sqsh")
			if err := os.WriteFile(img, b, 0o600); err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			order := "-le"
			if v.big {
				order = "-be"
			}
			want := filepath.Join(dir, "want")
			cmd := exec.Command("sasquatch", order, "-d", want, img)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("sasquatch: %v: %s", err, out)
			}
			exp, err := linkTree(want)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if len(exp) < 10 {
				t.Fatalf("expected sasquatch to write the tree, got: %q", exp)
			}
			const off = 0x1234
			data := append(bytes.Repeat([]byte{0x5A}, off), b...)
			data = append(data, bytes.Repeat([]byte{0xFF}, 300)...)
			var got []string
			n := 0
			for e, err := range walk.WalkBytes(context.Background(), data, walk.WithFormats("squashfs")) {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				n++
				if e.Offset != off || e.Size != int64(len(b)) {
					t.Errorf("expected a match at %#x of %d bytes, got: %#x of %d", off, len(b), e.Offset, e.Size)
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

// sq3Variant is an image that sq3Image makes.
type sq3Variant struct {
	name         string
	major, minor int
	big          bool
	noFragments  bool
	check        bool // the flag SQUASHFS_CHECK, which adds a byte to each metadata header
	unsorted     bool // the entries of each folder in reverse order, which 1.x and 2.0 allow
}

var sq3Variants = []sq3Variant{
	{name: "1.0 little", major: 1, unsorted: true},
	{name: "1.0 big", major: 1, big: true},
	{name: "2.0 little", major: 2, unsorted: true},
	{name: "2.1 big", major: 2, minor: 1, big: true, check: true},
	{name: "2.1 little no fragments", major: 2, minor: 1, noFragments: true},
	{name: "3.0 little", major: 3},
	{name: "3.1 big", major: 3, minor: 1, big: true},
	{name: "3.1 big check", major: 3, minor: 1, big: true, check: true, noFragments: true},
}

// sq3Node is a file, a link or a folder of the tree of sq3Image.
type sq3Node struct {
	name     string
	data     []byte // a file
	exec     bool
	link     string // a link
	dir      []*sq3Node
	isDir    bool
	hardlink *sq3Node // in 3.x, an entry that names the inode of another file

	ref    uint64 // the inode, once written
	number uint32
}

// sq3Tree is the tree of every image: files of zero, small and many
// blocks, one whose data does not compress, one with a block of zeros, an
// executable file, links and folders.
func sq3Tree(v sq3Variant) *sq3Node {
	rng := rand.New(rand.NewPCG(3, 4))
	big := make([]byte, 3*4096+1234)
	for i := range big {
		big[i] = byte(rng.IntN(8))
	}
	noise := make([]byte, 5000)
	for i := range noise {
		noise[i] = byte(rng.Uint32())
	}
	holes := slices.Concat(bytes.Repeat([]byte("ab"), 2048), make([]byte, 4096), []byte("tail of the file"))
	busybox := &sq3Node{name: "busybox", data: big, exec: true}
	bin := &sq3Node{name: "bin", isDir: true, dir: []*sq3Node{
		busybox,
		{name: "ls", link: "busybox"},
		{name: "sh", link: "/bin/busybox"},
	}}
	etc := &sq3Node{name: "etc", isDir: true, dir: []*sq3Node{
		{name: "config", isDir: true, dir: []*sq3Node{
			{name: "a.conf", data: []byte(strings.Repeat("key=value\n", 300))},
			{name: "b.conf", data: []byte("b=1\n")},
		}},
		{name: "up", link: "../usr/share/odd"},
		{name: "broken", link: "/nowhere"},
	}}
	share := &sq3Node{name: "share", isDir: true, dir: []*sq3Node{
		{name: "noise", data: noise},
		{name: "odd", data: big[:4097]},
		{name: "holes", data: holes},
	}}
	if v.major >= 3 {
		share.dir = append(share.dir, &sq3Node{name: "same", hardlink: busybox})
	}
	root := &sq3Node{isDir: true, dir: []*sq3Node{
		bin, etc,
		{name: "empty", data: []byte{}},
		{name: "small.txt", data: []byte("hello, squashfs\n")},
		{name: "usr", isDir: true, dir: []*sq3Node{share}},
		{name: "var", isDir: true, dir: []*sq3Node{{name: "empty", isDir: true}}},
	}}
	return root
}

// sq3Writer writes an image of SquashFS before 4.0, as mksquashfs of each
// version lays it out: the data and the fragments, the table of inodes, the
// table of folders, the table of fragments and its index, and the tables of
// uids.
type sq3Writer struct {
	v         sq3Variant
	blockSize int
	img       []byte
	inodes    sq3Meta
	dirs      sq3Meta
	frag      []byte     // the fragment that fills
	frags     [][2]int64 // the fragments written: start and size
	count     uint32
}

// sq3Meta is a table of metadata: blocks of 8 KiB, each compressed when
// that makes it smaller.
type sq3Meta struct {
	out []byte // the blocks written
	buf []byte // the block that fills
}

// sq3Image writes the tree of v as an image.
func sq3Image(v sq3Variant) []byte {
	w := &sq3Writer{v: v, blockSize: 4096, img: make([]byte, 128)}
	root := sq3Tree(v)
	w.write(root, 0)
	w.inodes.flush(w)
	w.dirs.flush(w)

	inodeTable := int64(len(w.img))
	w.img = append(w.img, w.inodes.out...)
	dirTable := int64(len(w.img))
	w.img = append(w.img, w.dirs.out...)
	if len(w.frag) > 0 {
		w.flushFragment()
	}
	fragTable := int64(len(w.img))
	if len(w.frags) > 0 {
		// The entries of the fragments, in metadata, then their index.
		var t sq3Meta
		for _, f := range w.frags {
			if v.major == 2 {
				t.add(w, w.fields(32, uint64(f[0]), 32, uint64(f[1])))
			} else {
				t.add(w, w.fields(64, uint64(f[0]), 32, uint64(f[1]), 32, 0))
			}
		}
		t.flush(w)
		start := int64(len(w.img))
		w.img = append(w.img, t.out...)
		fragTable = int64(len(w.img))
		for i := 0; i < len(t.out); {
			n := int(w.u16(t.out[i:]) & 0x7FFF)
			ptr := uint64(64)
			if v.major == 2 {
				ptr = 32
			}
			w.img = append(w.img, w.fields(ptr, uint64(start+int64(i)))...)
			i += 2 + n
			if v.check {
				i++
			}
		}
	}
	uidStart := int64(len(w.img))
	w.img = append(w.img, w.fields(32, 0)...) // root, which binwalk reads as the first pointer
	used := int64(len(w.img))

	o := w.order()
	h := w.img[:128]
	o.PutUint32(h[0:], 0x73717368)
	o.PutUint32(h[4:], w.count)
	o.PutUint32(h[8:], uint32(used))
	o.PutUint32(h[12:], uint32(uidStart))
	o.PutUint32(h[20:], uint32(inodeTable))
	o.PutUint32(h[24:], uint32(dirTable))
	o.PutUint16(h[28:], uint16(v.major))
	o.PutUint16(h[30:], uint16(v.minor))
	o.PutUint16(h[32:], uint16(w.blockSize))
	o.PutUint16(h[34:], 12)
	var flags byte
	if v.check {
		flags |= 4
	}
	if v.noFragments {
		flags |= 0x10
	}
	h[36], h[37], h[38] = flags, 1, 0
	o.PutUint32(h[39:], 1234567890)
	o.PutUint64(h[43:], root.ref)
	o.PutUint32(h[51:], uint32(w.blockSize))
	o.PutUint32(h[55:], uint32(len(w.frags)))
	o.PutUint32(h[59:], uint32(fragTable))
	o.PutUint64(h[63:], uint64(used))
	o.PutUint64(h[71:], uint64(uidStart))
	o.PutUint64(h[87:], uint64(inodeTable))
	o.PutUint64(h[95:], uint64(dirTable))
	o.PutUint64(h[103:], uint64(fragTable))
	o.PutUint64(h[111:], 0xFFFFFFFFFFFFFFFF) // no table of inode numbers
	return w.img
}

func (w *sq3Writer) order() binary.ByteOrder {
	if w.v.big {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

func (w *sq3Writer) u16(b []byte) uint16 { return w.order().Uint16(b) }

// fields packs bit fields: pairs of a width in bits and a value, from the
// bit 0. A little endian image holds them from the low bit of each byte, as
// GCC lays out bit fields there, and a big endian image from the high bit.
func (w *sq3Writer) fields(f ...uint64) []byte {
	total := 0
	for i := 0; i < len(f); i += 2 {
		total += int(f[i])
	}
	b := make([]byte, (total+7)/8)
	pos := 0
	for i := 0; i < len(f); i += 2 {
		n, v := int(f[i]), f[i+1]
		for j := range n {
			bit := (v >> j) & 1 // the bit j of the value, from the low bit
			p := pos + j
			if w.v.big {
				p = pos + n - 1 - j
				b[p/8] |= byte(bit) << (7 - p%8)
			} else {
				b[p/8] |= byte(bit) << (p % 8)
			}
		}
		pos += n
	}
	return b
}

// compressZlib compresses b with zlib, and reports whether that made it smaller.
func compressZlib(b []byte) ([]byte, bool) {
	var buf bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Bytes(), buf.Len() < len(b)
}

func (m *sq3Meta) add(w *sq3Writer, b []byte) {
	m.buf = append(m.buf, b...)
	for len(m.buf) >= 8192 {
		m.block(w, m.buf[:8192])
		m.buf = m.buf[8192:]
	}
}

func (m *sq3Meta) flush(w *sq3Writer) {
	if len(m.buf) > 0 {
		m.block(w, m.buf)
		m.buf = nil
	}
}

func (m *sq3Meta) block(w *sq3Writer, b []byte) {
	c, ok := compressZlib(b)
	h := uint16(len(c))
	if !ok {
		c, h = b, uint16(len(b))|0x8000
	}
	hb := make([]byte, 2)
	w.order().PutUint16(hb, h)
	m.out = append(m.out, hb...)
	if w.v.check {
		m.out = append(m.out, 0xFF)
	}
	m.out = append(m.out, c...)
}

// pos is the reference of the next byte: the start of its block, and the
// offset in it.
func (m *sq3Meta) pos() uint64 {
	return uint64(len(m.out))<<16 | uint64(len(m.buf))
}

// write writes the inodes of the tree under n, children first, and returns
// the inode of n.
func (w *sq3Writer) write(n *sq3Node, parent uint32) {
	if n.hardlink != nil {
		n.ref, n.number = n.hardlink.ref, n.hardlink.number
		return
	}
	w.count++
	n.number = w.count
	if !n.isDir {
		w.writeInode(n)
		return
	}
	children := slices.Clone(n.dir)
	slices.SortFunc(children, func(a, b *sq3Node) int { return strings.Compare(a.name, b.name) })
	for _, c := range children {
		w.write(c, n.number)
	}
	if w.v.unsorted {
		slices.Reverse(children)
	}
	// One header for each entry, so that each names its own block of
	// inodes.
	at := w.dirs.pos()
	size := 0
	for _, c := range children {
		typ := uint64(3)
		switch {
		case c.isDir:
			typ = 1
		case c.link == "":
			typ = 2
		}
		var h, e []byte
		if w.v.major >= 3 {
			h = w.fields(8, 0, 32, c.ref>>16, 32, uint64(c.number))
			e = w.fields(13, c.ref&0xFFFF, 3, typ, 8, uint64(len(c.name)-1), 16, 0)
		} else {
			h = w.fields(8, 0, 24, c.ref>>16)
			e = w.fields(13, c.ref&0xFFFF, 3, typ, 8, uint64(len(c.name)-1))
		}
		w.dirs.add(w, slices.Concat(h, e, []byte(c.name)))
		size += len(h) + len(e) + len(c.name)
	}
	n.ref = w.inodes.pos()
	mode := uint64(0o755)
	start, offset := at>>16, at&0xFFFF
	switch w.v.major {
	case 1:
		w.inodes.add(w, w.fields(4, 1, 12, mode, 4, 0, 4, 15, 19, uint64(size), 13, offset, 32, 0, 24, start))
	case 2:
		w.inodes.add(w, w.fields(4, 1, 12, mode, 8, 0, 8, 255, 19, uint64(size), 13, offset, 32, 0, 24, start))
	default:
		w.inodes.add(w, w.fields(4, 1, 12, mode, 8, 0, 8, 255, 32, 0, 32, uint64(n.number),
			32, 2, 19, uint64(size+3), 13, offset, 32, start, 32, uint64(parent)))
	}
}

// writeInode writes the data and the inode of a file or a link.
func (w *sq3Writer) writeInode(n *sq3Node) {
	if n.link != "" {
		n.ref = w.inodes.pos()
		mode, size := uint64(0o777), uint64(len(n.link))
		var b []byte
		switch w.v.major {
		case 1:
			b = w.fields(4, 3, 12, mode, 4, 0, 4, 15, 16, size)
		case 2:
			b = w.fields(4, 3, 12, mode, 8, 0, 8, 255, 16, size)
		default:
			b = w.fields(4, 3, 12, mode, 8, 0, 8, 255, 32, 0, 32, uint64(n.number), 32, 1, 16, size)
		}
		w.inodes.add(w, append(b, n.link...))
		return
	}
	start := uint64(len(w.img))
	data := n.data
	// The tail of the file goes in a fragment, unless the image has none.
	// A version 1 image has no fragments.
	fragment, fragOffset := uint64(0xFFFFFFFF), uint64(0)
	full := len(data) / w.blockSize * w.blockSize
	if w.v.major == 1 || w.v.noFragments || len(data) == full {
		full = len(data)
	}
	var list []byte
	for i := 0; i < full; i += w.blockSize {
		b := data[i:min(i+w.blockSize, full)]
		var size uint64
		switch {
		case w.v.major >= 2 && !bytes.ContainsFunc(b, func(r rune) bool { return r != 0 }):
			size = 0 // a hole
		default:
			c, ok := compressZlib(b)
			if !ok {
				c = b
			}
			w.img = append(w.img, c...)
			size = uint64(len(c))
			if !ok {
				size |= map[bool]uint64{true: 0x8000, false: 1 << 24}[w.v.major == 1]
			}
		}
		if w.v.major == 1 {
			list = append(list, w.fields(16, size)...)
		} else {
			list = append(list, w.fields(32, size)...)
		}
	}
	if tail := data[full:]; len(tail) > 0 {
		if len(w.frag)+len(tail) > w.blockSize {
			w.flushFragment()
		}
		fragment, fragOffset = uint64(len(w.frags)), uint64(len(w.frag))
		w.frag = append(w.frag, tail...)
	}
	n.ref = w.inodes.pos()
	mode := uint64(0o644)
	if n.exec {
		mode = 0o755
	}
	size := uint64(len(data))
	var b []byte
	switch w.v.major {
	case 1:
		b = w.fields(4, 2, 12, mode, 4, 0, 4, 15, 32, 0, 32, start, 32, size)
	case 2:
		b = w.fields(4, 2, 12, mode, 8, 0, 8, 255, 32, 0, 32, start, 32, fragment, 32, fragOffset, 32, size)
	default:
		b = w.fields(4, 2, 12, mode, 8, 0, 8, 255, 32, 0, 32, uint64(n.number), 64, start, 32, fragment, 32, fragOffset, 32, size)
	}
	w.inodes.add(w, append(b, list...))
}

// flushFragment writes the fragment that fills.
func (w *sq3Writer) flushFragment() {
	start := int64(len(w.img))
	c, ok := compressZlib(w.frag)
	size := int64(len(c))
	if !ok {
		c, size = w.frag, int64(len(w.frag))|1<<24
	}
	w.img = append(w.img, c...)
	w.frags = append(w.frags, [2]int64{start, size})
	w.frag = nil
}

// TestSq3ImageSamples writes the images of the variants to the folder that
// SQ3_SAMPLES names, for test/corpus/testdata/walk. It skips without it.
func TestSq3ImageSamples(t *testing.T) {
	dir := os.Getenv("SQ3_SAMPLES")
	if dir == "" {
		t.Skip("SQ3_SAMPLES names no folder")
	}
	n := map[int]int{}
	for _, v := range sq3Variants {
		n[v.major]++
		name := filepath.Join(dir, fmt.Sprintf("squashfs-v%d-%d.bin", v.major, n[v.major]))
		if err := os.WriteFile(name, sq3Image(v), 0o644); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
}
