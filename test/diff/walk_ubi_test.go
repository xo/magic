package diff

import (
	"bytes"
	"cmp"
	"compress/flate"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/xo/magic/walk"
)

// TestWalkUBI walks an UBIFS image and an UBI image that wraps it, each
// after random bytes, and compares the files of the walker with the tree
// that made the image. No tool to make these images is installed, so
// ubifsImage and ubiImage write them, with the CRC of each node and
// header. binwalk extracts them with ubireader_extract_files and
// ubireader_extract_images, which set no mode and make no device, so the
// tree has no executable file and no device.
func TestWalkUBI(t *testing.T) {
	t.Parallel()
	tree := ubiTestTree()
	fsImg := ubifsImage(tree, ubiLEB)
	want := tree.want()
	rng := rand.New(rand.NewPCG(3, 4))
	junk := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(rng.Uint32())
		}
		return b
	}
	t.Run("ubifs", func(t *testing.T) {
		t.Parallel()
		const off = 0x1234
		data := append(junk(off), fsImg...)
		data = append(data, junk(777)...)
		n := 0
		for e, err := range walk.WalkBytes(context.Background(), data, walk.WithFormats("ubi", "ubifs")) {
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			n++
			if e.Format != "ubifs" || e.Offset != off || e.Size != int64(len(fsImg)) {
				t.Errorf("expected %q, got: %q", fmt.Sprintf("ubifs at %#x of %d bytes", off, len(fsImg)), fmt.Sprintf("%s at %#x of %d bytes", e.Format, e.Offset, e.Size))
			}
			if got := ubiMemberTree(t, e, "ubifs-root/"); !slices.Equal(got, want) {
				t.Errorf("expected %q,\n got: %q", want, got)
			}
		}
		if n != 1 {
			t.Errorf("expected %q, got: %q", "1 match", fmt.Sprintf("%d matches", n))
		}
	})
	t.Run("ubi", func(t *testing.T) {
		t.Parallel()
		const off = 0x2345
		other := junk(2 * ubiLEB)
		img, size := ubiImage(fsImg, other)
		data := append(junk(off), img...)
		data = append(data, junk(555)...)
		dir := fmt.Sprintf("ubifs-root/ubi_%X.img/", off)
		var sawUBI, sawFS bool
		for e, err := range walk.WalkBytes(context.Background(), data, walk.WithFormats("ubi", "ubifs"), walk.WithDepth(1)) {
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			switch {
			case e.Depth == 0 && e.Format == "ubi":
				sawUBI = true
				if e.Offset != off || e.Size != int64(size) {
					t.Errorf("expected %q, got: %q", fmt.Sprintf("ubi at %#x of %d bytes", off, size), fmt.Sprintf("ubi at %#x of %d bytes", e.Offset, e.Size))
				}
				// The volume "data" has no logical block 1, which is 0xFF.
				missing := append(append(slices.Clone(other[:ubiLEB]), bytes.Repeat([]byte{0xFF}, ubiLEB)...), other[ubiLEB:]...)
				exp := []string{
					fmt.Sprintf("%simg-1_vol-data.ubifs %d %x", dir, len(missing), sha256.Sum256(missing)),
					fmt.Sprintf("%simg-1_vol-rootfs.ubifs %d %x", dir, len(fsImg), sha256.Sum256(fsImg)),
				}
				if got := ubiMemberFiles(t, e); !slices.Equal(got, exp) {
					t.Errorf("expected %q,\n got: %q", exp, got)
				}
			case e.Depth == 1 && e.Format == "ubifs" && e.Member == dir+"img-1_vol-rootfs.ubifs":
				sawFS = true
				if got := ubiMemberTree(t, e, "ubifs-root/"); !slices.Equal(got, want) {
					t.Errorf("expected %q,\n got: %q", want, got)
				}
			case e.Depth == 0:
				t.Errorf("expected %q, got: %q", "no other match", fmt.Sprintf("%s at %#x", e.Format, e.Offset))
			}
		}
		if !sawUBI || !sawFS {
			t.Errorf("expected %q, got: %q", "ubi true, ubifs true", fmt.Sprintf("ubi %t, ubifs %t", sawUBI, sawFS))
		}
	})
}

// ubiMemberTree describes the members of e under prefix: the path, the
// type, the hash of a file and whether a bit of execution is set, and the
// target of a link.
func ubiMemberTree(t *testing.T, e *walk.Entry, prefix string) []string {
	t.Helper()
	var out []string
	for m, err := range e.Members(context.Background()) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		rel, ok := strings.CutPrefix(m.Name, prefix)
		if !ok || rel == "" {
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

// ubiMemberFiles describes the regular members of e.
func ubiMemberFiles(t *testing.T, e *walk.Entry) []string {
	t.Helper()
	var out []string
	for m, err := range e.Members(context.Background()) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if m.Mode.IsRegular() {
			r, _ := m.Open()
			b, _ := io.ReadAll(r)
			out = append(out, fmt.Sprintf("%s %d %x", m.Name, len(b), sha256.Sum256(b)))
		}
	}
	slices.Sort(out)
	return out
}

// The sizes of the test images: a logical erase block, and the headers of
// UBI before its data.
const (
	ubiLEB     = 8192
	ubiDataOff = 128
	ubiPEB     = ubiLEB + ubiDataOff
)

// ubiNode is one file, folder, link or special file of a test tree.
type ubiNode struct {
	path   string
	typ    uint8 // the type of a directory entry of UBIFS
	mode   uint32
	data   []byte // the data of a file, or the target of a link
	size   uint64 // the size of the inode, when it is more than the data
	compr  uint16 // the compression of the data nodes
	holes  []int  // the blocks of a file that have no data node
	linkTo string // a hard link to the file of this path
}

type ubiTree []ubiNode

// ubiTestTree is a tree with each case of the reader: files of zero, one
// and several blocks, with each compression, an executable file, a file
// with a hole, links, a hard link, a FIFO, a device, and folders.
func ubiTestTree() ubiTree {
	rng := rand.New(rand.NewPCG(5, 6))
	big := make([]byte, 13000)
	for i := range big {
		big[i] = byte(rng.IntN(16))
	}
	return ubiTree{
		{path: "bin", typ: 1},
		{path: "bin/busybox", mode: 0o755, data: big[:10000], compr: 2},
		{path: "bin/ls", typ: 2, data: []byte("busybox")},
		{path: "bin/hard", linkTo: "bin/busybox"},
		{path: "dev", typ: 1},
		{path: "dev/fifo", typ: 5},
		{path: "dev/null", typ: 4},
		{path: "empty", mode: 0o644},
		{path: "etc", typ: 1},
		{path: "etc/lzo.conf", mode: 0o644, data: []byte(strings.Repeat("key=value\n", 600)), compr: 1},
		{path: "etc/up", typ: 2, data: []byte("../usr/zst")},
		{path: "sbin", typ: 1},
		{path: "sbin/sh", typ: 2, data: []byte("/bin/busybox")},
		{path: "small.txt", mode: 0o644, data: []byte("hello, ubifs\n")},
		{path: "sparse", mode: 0o644, data: big[:3*4096], holes: []int{1}, size: 5*4096 + 100},
		{path: "usr", typ: 1},
		{path: "usr/zst", mode: 0o644, data: big[:9000], compr: 3},
		{path: "var", typ: 1},
		{path: "var/empty", typ: 1},
	}
}

// want describes the files that ubireader_extract_files writes for t, as
// ubiMemberTree describes members. A FIFO is a file that names it, as
// binwalk writes one.
func (t ubiTree) want() []string {
	var out []string
	data := map[string][]byte{}
	for _, n := range t {
		switch {
		case n.linkTo != "":
			b := data[n.linkTo]
			out = append(out, fmt.Sprintf("%s %d %x false", n.path, len(b), sha256.Sum256(b)))
		case n.typ == 1:
			out = append(out, n.path+"/")
		case n.typ == 2:
			out = append(out, n.path+" -> "+linkPath(n.path, string(n.data)))
		case n.typ == 5:
			out = append(out, fmt.Sprintf("%s 4 %x false", n.path, sha256.Sum256([]byte("fifo"))))
		case n.typ == 0:
			b := slices.Clone(n.data)
			for _, h := range n.holes {
				copy(b[h*4096:], make([]byte, 4096))
			}
			if uint64(len(b)) < n.size {
				b = append(b, make([]byte, n.size-uint64(len(b)))...)
			}
			data[n.path] = b
			out = append(out, fmt.Sprintf("%s %d %x false", n.path, len(b), sha256.Sum256(b)))
		}
	}
	slices.Sort(out)
	return out
}

// ubiCRC is the CRC of UBI and UBIFS: the CRC-32 of zlib with no final
// inversion.
func ubiCRC(b []byte) uint32 {
	return ^crc32.ChecksumIEEE(b)
}

// ubifsNode is a node of the main area, with the key that sorts it in the
// index.
type ubifsNode struct {
	key  [2]uint32
	body []byte // the node after its common header
	typ  uint8
}

// ubifsKey is a key of UBIFS: the inode number, and the type in the top
// three bits of the second word.
func ubifsKey(ino uint32, typ uint32, low uint32) [2]uint32 {
	return [2]uint32{ino, typ<<29 | low&0x1FFFFFFF}
}

func putKey(b []byte, k [2]uint32) {
	binary.LittleEndian.PutUint32(b, k[0])
	binary.LittleEndian.PutUint32(b[4:], k[1])
}

// ubifsNodeBytes is a node with its common header and its CRC.
func ubifsNodeBytes(typ uint8, sqnum uint64, body []byte) []byte {
	n := make([]byte, 24+len(body))
	binary.LittleEndian.PutUint32(n, 0x06101831)
	binary.LittleEndian.PutUint64(n[8:], sqnum)
	binary.LittleEndian.PutUint32(n[16:], uint32(len(n)))
	n[20] = typ
	copy(n[24:], body)
	binary.LittleEndian.PutUint32(n[4:], ubiCRC(n[8:]))
	return n
}

// ubifsCompress compresses one block of data as UBIFS stores it.
func ubifsCompress(compr uint16, b []byte) []byte {
	switch compr {
	case 1: // LZO1X: one run of literals, and the end of the stream.
		var out []byte
		if len(b) <= 238 {
			out = append(out, byte(17+len(b)))
		} else {
			rem := len(b) - 18
			k := (rem - 1) / 255
			out = append(out, 0)
			out = append(out, make([]byte, k)...)
			out = append(out, byte(rem-255*k))
		}
		out = append(out, b...)
		return append(out, 0x11, 0, 0)
	case 2: // raw deflate
		var buf bytes.Buffer
		w, _ := flate.NewWriter(&buf, flate.BestCompression)
		_, _ = w.Write(b)
		_ = w.Close()
		return buf.Bytes()
	case 3:
		enc, _ := zstd.NewWriter(nil)
		out := enc.EncodeAll(b, nil)
		_ = enc.Close()
		return out
	}
	return b
}

// ubifsImage writes an UBIFS image of the tree, with logical erase blocks
// of leb bytes: the superblock in block 0, the master node in blocks 1 and
// 2, and the nodes and one index node from block 3.
func ubifsImage(t ubiTree, leb int) []byte {
	inum := map[string]uint32{"": 1}
	nlink := map[uint32]uint32{1: 2}
	next := uint32(65)
	for _, n := range t {
		if n.linkTo != "" {
			nlink[inum[n.linkTo]]++
			inum[n.path] = inum[n.linkTo]
			continue
		}
		inum[n.path] = next
		nlink[next] = 1
		if n.typ == 1 {
			nlink[next] = 2
		}
		next++
	}
	var nodes []ubifsNode
	inode := func(ino uint32, mode uint32, size uint64, data []byte) {
		b := make([]byte, 136+len(data))
		putKey(b, ubifsKey(ino, 0, 0))
		binary.LittleEndian.PutUint64(b[24:], size)
		binary.LittleEndian.PutUint32(b[68:], nlink[ino])
		binary.LittleEndian.PutUint32(b[80:], mode)
		binary.LittleEndian.PutUint32(b[88:], uint32(len(data)))
		copy(b[136:], data)
		nodes = append(nodes, ubifsNode{key: ubifsKey(ino, 0, 0), body: b, typ: 0})
	}
	inode(1, 0o40755, 160, nil)
	for _, n := range t {
		parent := ""
		if i := strings.LastIndex(n.path, "/"); i >= 0 {
			parent = n.path[:i]
		}
		name := n.path[strings.LastIndex(n.path, "/")+1:]
		ino := inum[n.path]
		typ := n.typ
		if n.linkTo != "" {
			typ = 0
		}
		// The entry, with a hash of its name.
		h := crc32.ChecksumIEEE([]byte(name)) & 0x1FFFFFFF
		d := make([]byte, 32+len(name)+1)
		putKey(d, ubifsKey(inum[parent], 2, h))
		binary.LittleEndian.PutUint64(d[16:], uint64(ino))
		d[25] = typ
		binary.LittleEndian.PutUint16(d[26:], uint16(len(name)))
		copy(d[32:], name)
		nodes = append(nodes, ubifsNode{key: ubifsKey(inum[parent], 2, h), body: d, typ: 2})
		if n.linkTo != "" {
			continue
		}
		switch n.typ {
		case 1:
			inode(ino, 0o40755, 160, nil)
		case 2:
			inode(ino, 0o120777, uint64(len(n.data)), n.data)
		case 4:
			inode(ino, 0o20666, 0, binary.LittleEndian.AppendUint32(nil, 1<<8|3))
		case 5:
			inode(ino, 0o10644, 0, nil)
		case 0:
			size := max(n.size, uint64(len(n.data)))
			inode(ino, 0o100000|n.mode, size, nil)
			for blk := 0; blk*4096 < len(n.data); blk++ {
				if slices.Contains(n.holes, blk) {
					continue
				}
				chunk := n.data[blk*4096 : min(len(n.data), (blk+1)*4096)]
				c := ubifsCompress(n.compr, chunk)
				b := make([]byte, 24+len(c))
				putKey(b, ubifsKey(ino, 1, uint32(blk)))
				binary.LittleEndian.PutUint32(b[16:], uint32(len(chunk)))
				binary.LittleEndian.PutUint16(b[20:], n.compr)
				copy(b[24:], c)
				nodes = append(nodes, ubifsNode{key: ubifsKey(ino, 1, uint32(blk)), body: b, typ: 1})
			}
		}
	}
	slices.SortStableFunc(nodes, func(a, b ubifsNode) int {
		return cmp.Or(cmp.Compare(a.key[0], b.key[0]), cmp.Compare(a.key[1], b.key[1]))
	})
	// The main area, from block 3. A node does not cross a block.
	main := []byte{}
	lnum, offs := 3, 0
	type place struct{ lnum, offs, size int }
	places := make([]place, 0, len(nodes))
	sqnum := uint64(100)
	add := func(typ uint8, body []byte) place {
		n := ubifsNodeBytes(typ, sqnum, body)
		sqnum++
		size := (len(n) + 7) &^ 7
		if offs+size > leb {
			main = append(main, bytes.Repeat([]byte{0xFF}, leb-offs)...)
			lnum, offs = lnum+1, 0
		}
		p := place{lnum, offs, len(n)}
		main = append(main, n...)
		main = append(main, make([]byte, size-len(n))...)
		offs += size
		return p
	}
	for _, n := range nodes {
		places = append(places, add(n.typ, n.body))
	}
	idx := make([]byte, 4, 4+20*len(nodes))
	binary.LittleEndian.PutUint16(idx, uint16(len(nodes)))
	for i, n := range nodes {
		br := make([]byte, 20)
		binary.LittleEndian.PutUint32(br, uint32(places[i].lnum))
		binary.LittleEndian.PutUint32(br[4:], uint32(places[i].offs))
		binary.LittleEndian.PutUint32(br[8:], uint32(places[i].size))
		putKey(br[12:], n.key)
		idx = append(idx, br...)
	}
	root := add(9, idx)
	main = append(main, bytes.Repeat([]byte{0xFF}, leb-offs)...)
	count := lnum + 1

	img := bytes.Repeat([]byte{0xFF}, 3*leb)
	sb := make([]byte, 4096-24)
	binary.LittleEndian.PutUint32(sb[8:], 8)            // min_io_size
	binary.LittleEndian.PutUint32(sb[12:], uint32(leb)) // leb_size
	binary.LittleEndian.PutUint32(sb[16:], uint32(count))
	binary.LittleEndian.PutUint32(sb[20:], uint32(count))
	binary.LittleEndian.PutUint32(sb[56:], 4) // fmt_version
	binary.LittleEndian.PutUint16(sb[60:], 2) // default_compr
	binary.LittleEndian.PutUint32(sb[80:], 1) // time_gran
	copy(img, ubifsNodeBytes(6, 1, sb))
	mst := make([]byte, 512-24)
	binary.LittleEndian.PutUint64(mst, uint64(next))
	binary.LittleEndian.PutUint64(mst[8:], 1) // cmt_no
	binary.LittleEndian.PutUint32(mst[24:], uint32(root.lnum))
	binary.LittleEndian.PutUint32(mst[28:], uint32(root.offs))
	binary.LittleEndian.PutUint32(mst[32:], uint32(root.size))
	binary.LittleEndian.PutUint32(mst[140:], uint32(count))
	m := ubifsNodeBytes(7, 2, mst)
	copy(img[leb:], m)
	copy(img[2*leb:], m)
	return append(img, main...)
}

// ubiImage wraps the UBIFS image fsImg in an UBI image: two blocks of the
// layout volume, each block of fsImg in the volume "rootfs", the volume
// "data" with the halves of other as its logical blocks 0 and 2, and an
// erased block at the end, which binwalk does not count. With a nil other,
// the image has no blocks of the volume "data". It returns the
// image and the size that binwalk gives it.
func ubiImage(fsImg, other []byte) ([]byte, int) {
	var img []byte
	sqnum := uint64(1)
	block := func(volID, lnum uint32, data []byte, erased bool) {
		b := bytes.Repeat([]byte{0xFF}, ubiPEB)
		copy(b, "UBI#\x01")
		clear(b[5:60])
		binary.BigEndian.PutUint64(b[8:], 1)
		binary.BigEndian.PutUint32(b[16:], 64)
		binary.BigEndian.PutUint32(b[20:], ubiDataOff)
		binary.BigEndian.PutUint32(b[24:], 1) // image_seq
		binary.BigEndian.PutUint32(b[60:], ubiCRC(b[:60]))
		if erased {
			img = append(img, b...)
			return
		}
		v := b[64:128]
		clear(v)
		copy(v, "UBI!\x01")
		v[5] = 1 // dynamic
		if volID >= 0x7FFFEFFF {
			v[7] = 5
		}
		binary.BigEndian.PutUint32(v[8:], volID)
		binary.BigEndian.PutUint32(v[12:], lnum)
		binary.BigEndian.PutUint64(v[40:], sqnum)
		sqnum++
		binary.BigEndian.PutUint32(v[60:], ubiCRC(v[:60]))
		copy(b[ubiDataOff:], data)
		img = append(img, b...)
	}
	vtbl := make([]byte, 128*172)
	for i := range 128 {
		r := vtbl[i*172 : (i+1)*172]
		switch i {
		case 0:
			binary.BigEndian.PutUint32(r, uint32(len(fsImg)/ubiLEB))
			binary.BigEndian.PutUint32(r[4:], 1)
			r[12] = 1
			binary.BigEndian.PutUint16(r[14:], 6)
			copy(r[16:], "rootfs")
		case 1:
			binary.BigEndian.PutUint32(r, 3)
			binary.BigEndian.PutUint32(r[4:], 1)
			r[12] = 1
			binary.BigEndian.PutUint16(r[14:], 4)
			copy(r[16:], "data")
		}
		binary.BigEndian.PutUint32(r[168:], ubiCRC(r[:168]))
	}
	block(0x7FFFEFFF, 0, vtbl, false)
	block(0x7FFFEFFF, 1, vtbl, false)
	for i := 0; i*ubiLEB < len(fsImg); i++ {
		block(0, uint32(i), fsImg[i*ubiLEB:(i+1)*ubiLEB], false)
	}
	if other != nil {
		block(1, 0, other[:ubiLEB], false)
		block(1, 2, other[ubiLEB:], false)
	}
	size := len(img)
	block(0, 0, nil, true)
	return img, size
}
