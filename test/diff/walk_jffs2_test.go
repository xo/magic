package diff

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/ulikunitz/xz/lzma"
	"github.com/xo/magic/walk"
)

// TestWalkJFFS2 walks JFFS2 images that jffs2Writer makes from a known tree,
// in both byte orders, and compares the members with the tree. binwalk
// extracts JFFS2 with jefferson, which the test does not run: the tree is
// what jefferson writes from these images. The images use each compression
// that the writer has: none, zlib, rtime and lzma.
func TestWalkJFFS2(t *testing.T) {
	t.Parallel()
	for _, order := range []byteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(order.String(), func(t *testing.T) {
			t.Parallel()
			img, exp := jffs2Sample(order)
			rng := rand.New(rand.NewPCG(3, 4))
			const off = 0x1235 &^ 3
			data := make([]byte, off)
			for i := range data {
				data[i] = byte(rng.Uint32())
			}
			data = append(data, img...)
			data = append(data, bytes.Repeat([]byte{0xFF}, 1024)...)
			n := 0
			for e, err := range walk.WalkBytes(context.Background(), data, walk.WithFormats("jffs2")) {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				n++
				if e.Offset != off || e.Size != int64(len(img)) {
					t.Errorf("expected a match at %#x of %d bytes, got: %#x of %d", off, len(img), e.Offset, e.Size)
				}
				if got := membersBelow(t, e, "jffs2-root/"); !slices.Equal(got, exp) {
					t.Errorf("expected %q,\n got: %q", exp, got)
				}
			}
			if n != 1 {
				t.Fatalf("expected one match, got: %d", n)
			}
		})
	}
}

// jffs2Sample makes an image of the known tree, and returns the lines of
// membersBelow for it. A file has several data nodes, one of which a later
// version writes over, and the image has a hole, a device and links.
func jffs2Sample(order byteOrder) ([]byte, []string) {
	rng := rand.New(rand.NewPCG(5, 6))
	busybox := make([]byte, 10000)
	for i := range busybox {
		busybox[i] = byte(rng.IntN(8))
	}
	conf := []byte(strings.Repeat("key=value\n", 600))
	small := []byte("hello, jffs2\n")
	w := &jffs2Writer{order: order}
	w.cleanMarker()
	const (
		root = 1
		bin  = 2
		etc  = 3
		cfg  = 4
		dev  = 5
	)
	w.dirent(root, 1, bin, "bin", 4)
	w.inode(bin, 1, 0o040755, 0, nil, 0, 0)
	w.dirent(root, 1, etc, "etc", 4)
	w.inode(etc, 1, 0o040755, 0, nil, 0, 0)
	w.dirent(etc, 1, cfg, "config", 4)
	w.inode(cfg, 1, 0o040755, 0, nil, 0, 0)
	w.dirent(root, 1, dev, "dev", 4)
	w.inode(dev, 1, 0o040755, 0, nil, 0, 0)
	// bin/busybox: three nodes of zlib, and a later version of the middle
	// one in rtime.
	w.dirent(bin, 1, 10, "busybox", 8)
	for i, v := range []uint32{1, 2, 3} {
		lo, hi := i*4096, min((i+1)*4096, len(busybox))
		w.inode(10, v, 0o100755, lo, busybox[lo:hi], 0x06, hi-lo)
	}
	patched := slices.Clone(busybox)
	copy(patched[4096:], bytes.Repeat([]byte("ab"), 100))
	w.inode(10, 4, 0o100755, 4096, patched[4096:8192], 0x02, 4096)
	// small.txt: no compression. A first entry of version 1 named
	// "old.txt" loses to the entry of version 2.
	w.dirent(root, 1, 11, "old.txt", 8)
	w.dirent(root, 2, 11, "small.txt", 8)
	w.inode(11, 1, 0o100644, 0, small, 0x00, len(small))
	// etc/config/a.conf: two nodes of lzma, with a hole of zeros between.
	w.dirent(cfg, 1, 12, "a.conf", 8)
	w.inode(12, 1, 0o100644, 0, conf[:3000], 0x08, 3000)
	w.inode(12, 2, 0o100644, 5000, conf[3000:], 0x08, len(conf)-3000)
	hole := slices.Concat(conf[:3000], make([]byte, 2000), conf[3000:])
	// empty: an inode with no data.
	w.dirent(root, 1, 13, "empty", 8)
	w.inode(13, 1, 0o100644, 0, nil, 0, 0)
	// Links, with the target as the data of the inode.
	links := map[string]string{"bin/ls": "busybox", "sh": "/bin/busybox", "etc/up": "../small.txt"}
	ino := uint32(20)
	for _, name := range slices.Sorted(maps.Keys(links)) {
		dir, base := uint32(root), name
		if d, b, ok := strings.Cut(name, "/"); ok {
			base = b
			dir = map[string]uint32{"bin": bin, "etc": etc}[d]
		}
		w.dirent(dir, 1, ino, base, 10)
		w.inode(ino, 1, 0o120777, 0, []byte(links[name]), 0, len(links[name]))
		ino++
	}
	// dev/null: a character device of the new form of 32 bits.
	w.dirent(dev, 1, 30, "null", 2)
	w.inode(30, 1, 0o020666, 0, order.AppendUint32(nil, 1<<8|3), 0, 4)

	exp := []string{
		"bin/",
		fileLine("bin/busybox", patched, true),
		"bin/ls -> " + linkPath("bin/ls", "busybox"),
		"dev/",
		fileLine("dev/null", []byte("c 1 3"), false),
		fileLine("empty", nil, false),
		"etc/",
		"etc/config/",
		fileLine("etc/config/a.conf", hole, false),
		"etc/up -> " + linkPath("etc/up", "../small.txt"),
		"sh -> " + linkPath("sh", "/bin/busybox"),
		fileLine("small.txt", small, false),
	}
	slices.Sort(exp)
	return w.buf, exp
}

// byteOrder reads and appends numbers in one byte order.
type byteOrder interface {
	binary.ByteOrder
	binary.AppendByteOrder
}

// jffs2Writer writes the nodes of a JFFS2 image, each with its CRCs, at a
// multiple of four bytes.
type jffs2Writer struct {
	order byteOrder
	buf   []byte
}

// jffs2TestCRC is the CRC-32 of JFFS2, with no inversion.
func jffs2TestCRC(b []byte) uint32 {
	return ^crc32.Update(^uint32(0), crc32.IEEETable, b)
}

// node writes a node of type typ: the header, then body, whose node CRC
// the caller has set, padded with 0xFF.
func (w *jffs2Writer) node(typ uint16, body []byte) {
	o := w.order
	h := o.AppendUint16(nil, 0x1985)
	h = o.AppendUint16(h, typ)
	h = o.AppendUint32(h, uint32(12+len(body)))
	h = o.AppendUint32(h, jffs2TestCRC(h))
	w.buf = append(append(w.buf, h...), body...)
	for len(w.buf)%4 != 0 {
		w.buf = append(w.buf, 0xFF)
	}
}

// cleanMarker writes a clean marker, the first node of an erase block.
func (w *jffs2Writer) cleanMarker() {
	w.node(0x2003, nil)
}

// header returns the 12 bytes of a node header, to compute a node CRC.
func (w *jffs2Writer) header(typ uint16, size int) []byte {
	o := w.order
	h := o.AppendUint16(nil, 0x1985)
	h = o.AppendUint16(h, typ)
	h = o.AppendUint32(h, uint32(size))
	return o.AppendUint32(h, jffs2TestCRC(h))
}

// dirent writes a folder entry.
func (w *jffs2Writer) dirent(pino, version, ino uint32, name string, dtype byte) {
	o := w.order
	b := o.AppendUint32(nil, pino)
	b = o.AppendUint32(b, version)
	b = o.AppendUint32(b, ino)
	b = o.AppendUint32(b, 0x5F000000)
	b = append(b, byte(len(name)), dtype, 0, 0)
	b = o.AppendUint32(b, jffs2TestCRC(append(w.header(0xE001, 40+len(name)), b...)))
	b = o.AppendUint32(b, jffs2TestCRC([]byte(name)))
	w.node(0xE001, append(b, name...))
}

// inode writes an inode node of the data at offset, compressed with compr.
func (w *jffs2Writer) inode(ino, version, mode uint32, offset int, data []byte, compr byte, dsize int) {
	var c []byte
	switch compr {
	case 0x00:
		c = data
	case 0x02:
		c = rtimeCompress(data)
	case 0x06:
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		if _, err := zw.Write(data); err != nil {
			panic(err)
		}
		if err := zw.Close(); err != nil {
			panic(err)
		}
		c = buf.Bytes()
	case 0x08:
		c = jffs2LZMA(data)
	default:
		panic(fmt.Sprintf("no compression %d", compr))
	}
	o := w.order
	b := o.AppendUint32(nil, ino)
	b = o.AppendUint32(b, version)
	b = o.AppendUint32(b, mode)
	b = o.AppendUint16(b, 0)
	b = o.AppendUint16(b, 0)
	b = o.AppendUint32(b, uint32(offset+dsize)) // isize
	for range 3 {
		b = o.AppendUint32(b, 0x5F000000)
	}
	b = o.AppendUint32(b, uint32(offset))
	b = o.AppendUint32(b, uint32(len(c)))
	b = o.AppendUint32(b, uint32(dsize))
	b = append(b, compr, 0, 0, 0)
	b = o.AppendUint32(b, jffs2TestCRC(c))
	b = o.AppendUint32(b, jffs2TestCRC(append(w.header(0xE002, 68+len(c)), b...)))
	w.node(0xE002, append(b, c...))
}

// rtimeCompress is rtime_compress of Linux.
func rtimeCompress(in []byte) []byte {
	var positions [256]int
	var out []byte
	pos := 0
	for pos < len(in) {
		value := in[pos]
		out = append(out, value)
		pos++
		backpos := positions[value]
		positions[value] = pos
		runlen := 0
		for backpos < pos && pos < len(in) && in[pos] == in[backpos] && runlen < 255 {
			backpos++
			pos++
			runlen++
		}
		out = append(out, byte(runlen))
	}
	return out
}

// jffs2LZMA compresses data as the lzma of JFFS2: lc, lp and pb of 0, a
// dictionary of 8 KiB, and no header.
func jffs2LZMA(data []byte) []byte {
	var buf bytes.Buffer
	zw, err := lzma.WriterConfig{
		Properties: &lzma.Properties{},
		DictCap:    0x2000,
		Size:       int64(len(data)),
	}.NewWriter(&buf)
	if err != nil {
		panic(err)
	}
	if _, err := zw.Write(data); err != nil {
		panic(err)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()[13:]
}

// membersBelow describes the members of e under the folder prefix as
// memberLine describes them, with the target of a link as the path from
// that folder that it names.
func membersBelow(t *testing.T, e *walk.Entry, prefix string) []string {
	t.Helper()
	var out []string
	for m, err := range e.Members(context.Background()) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		rel, ok := strings.CutPrefix(m.Name, prefix)
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
			out = append(out, fileLine(rel, b, m.Mode&0o111 != 0))
		}
	}
	slices.Sort(out)
	return out
}
