//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package filesystem

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/xo/magic/compress"
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/lz4"
	"github.com/xo/magic/walk/internal/lzo"
)

// The errors of the reader of SquashFS.
var (
	errSquashFS    = errors.New("squashfs: the image is not valid")
	errCompression = errors.New("squashfs: no decoder for the compression of the image")
)

// sqfs reads a SquashFS 4.0 image, in either byte order. sasquatch reads
// the versions before 4.0 too, which the walker does not read yet.
type sqfs struct {
	img        []byte
	order      binary.ByteOrder
	blockSize  int
	comp       int
	flags      int
	inodeTable int
	dirTable   int
	fragTable  int
	idTable    int
	fragCount  int
	rootRef    uint64

	visits int // the inodes read, which stops a loop of folders
}

// The flags of the superblock that the reader uses.
const (
	sqfsNoFragments = 0x0010
	sqfsCompOptions = 0x0400
)

// newSqfs reads the superblock of the image at the start of img.
func newSqfs(img []byte) (*sqfs, error) {
	if len(img) < 96 {
		return nil, errSquashFS
	}
	var order binary.ByteOrder = binary.LittleEndian
	switch string(img[:4]) {
	case "hsqs":
	case "sqsh":
		order = binary.BigEndian
	default:
		return nil, errSquashFS
	}
	if order.Uint16(img[28:]) != 4 {
		return nil, errSquashFS
	}
	s := &sqfs{
		img:        img,
		order:      order,
		blockSize:  int(order.Uint32(img[12:])),
		fragCount:  int(order.Uint32(img[16:])),
		comp:       int(order.Uint16(img[20:])),
		flags:      int(order.Uint16(img[24:])),
		rootRef:    order.Uint64(img[32:]),
		idTable:    int(order.Uint64(img[48:])),
		inodeTable: int(order.Uint64(img[64:])),
		dirTable:   int(order.Uint64(img[72:])),
		fragTable:  int(order.Uint64(img[80:])),
	}
	if s.blockSize <= 0 || s.blockSize > 1<<20 {
		return nil, errSquashFS
	}
	return s, nil
}

// decompress decodes one block of the image, of at most limit bytes.
func (s *sqfs) decompress(b []byte, limit int) ([]byte, error) {
	var out []byte
	var err error
	switch s.comp {
	case 1:
		var r io.ReadCloser
		if r, err = zlib.NewReader(bytes.NewReader(b)); err == nil {
			out, err = io.ReadAll(io.LimitReader(r, int64(limit)+1))
		}
	case 2, 4, 6:
		m := map[int]compress.Method{2: compress.LZMA, 4: compress.XZ, 6: compress.Zstd}[s.comp]
		dec, ok := compress.LookupStream(m)
		if !ok {
			return nil, errCompression
		}
		var buf bytes.Buffer
		_, err = dec(b, &buf)
		out = buf.Bytes()
	case 3:
		out, err = lzo.Decode(b, limit)
	case 5:
		out, err = lz4.Block(b, limit)
	default:
		return nil, errCompression
	}
	if err != nil {
		return nil, fmt.Errorf("squashfs: %w", err)
	}
	if len(out) > limit {
		return nil, errSquashFS
	}
	return out, nil
}

// meta reads metadata: blocks of at most 8 KiB, each after a header of 16
// bits whose top bit is set when the block is not compressed.
type meta struct {
	s   *sqfs
	pos int    // the header of the next block, in the image
	buf []byte // what is left of the current block
}

func (s *sqfs) meta(block, offset int) (*meta, error) {
	m := &meta{s: s, pos: block}
	if _, err := m.read(offset); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *meta) read(n int) ([]byte, error) {
	out := make([]byte, 0, n)
	for len(out) < n {
		if len(m.buf) == 0 {
			if m.pos < 0 || m.pos+2 > len(m.s.img) {
				return nil, errSquashFS
			}
			h := int(m.s.order.Uint16(m.s.img[m.pos:]))
			size := h & 0x7FFF
			start := m.pos + 2
			if start+size > len(m.s.img) || size == 0 {
				return nil, errSquashFS
			}
			b := m.s.img[start : start+size]
			m.pos = start + size
			if h&0x8000 == 0 {
				var err error
				if b, err = m.s.decompress(b, 8192); err != nil {
					return nil, err
				}
			}
			if len(b) == 0 {
				return nil, errSquashFS
			}
			m.buf = b
		}
		k := min(n-len(out), len(m.buf))
		out = append(out, m.buf[:k]...)
		m.buf = m.buf[k:]
	}
	return out, nil
}

// inode is what the reader takes from an inode.
type inode struct {
	typ        int
	mode       int
	start      int // a folder: its block in the table of folders; a file: its first block
	offset     int // a folder: its offset in that block
	size       int
	frag       uint32
	fragOffset int
	blocks     []uint32
	target     string
	major      int
	minor      int
}

// The basic and the extended types of inode.
const (
	sqDir = iota + 1
	sqFile
	sqSymlink
	sqBlock
	sqChar
	sqFIFO
	sqSocket
)

// inode reads the inode that ref names: the block in the table of inodes, and
// the offset in it.
func (s *sqfs) inode(ref uint64) (*inode, error) {
	if s.visits++; s.visits > len(s.img)/8+16 {
		return nil, errSquashFS
	}
	m, err := s.meta(s.inodeTable+int(ref>>16), int(ref&0xFFFF))
	if err != nil {
		return nil, err
	}
	h, err := m.read(16)
	if err != nil {
		return nil, err
	}
	o := s.order
	in := &inode{typ: int(o.Uint16(h)), mode: int(o.Uint16(h[2:]))}
	u32 := func(n int) ([]uint32, error) {
		b, err := m.read(4 * n)
		if err != nil {
			return nil, err
		}
		v := make([]uint32, n)
		for i := range v {
			v[i] = o.Uint32(b[4*i:])
		}
		return v, nil
	}
	switch in.typ {
	case 1: // folder
		b, err := m.read(16)
		if err != nil {
			return nil, err
		}
		in.start, in.size, in.offset = int(o.Uint32(b)), int(o.Uint16(b[8:])), int(o.Uint16(b[10:]))
	case 8: // extended folder
		b, err := m.read(24)
		if err != nil {
			return nil, err
		}
		in.typ = sqDir
		in.size, in.start, in.offset = int(o.Uint32(b[4:])), int(o.Uint32(b[8:])), int(o.Uint16(b[18:]))
	case 2: // file
		b, err := m.read(16)
		if err != nil {
			return nil, err
		}
		in.start, in.frag, in.fragOffset, in.size = int(o.Uint32(b)), o.Uint32(b[4:]), int(o.Uint32(b[8:])), int(o.Uint32(b[12:]))
	case 9: // extended file
		b, err := m.read(40)
		if err != nil {
			return nil, err
		}
		in.typ = sqFile
		in.start, in.size = int(o.Uint64(b)), int(o.Uint64(b[8:]))
		in.frag, in.fragOffset = o.Uint32(b[28:]), int(o.Uint32(b[32:]))
	case 3, 10: // link
		b, err := m.read(8)
		if err != nil {
			return nil, err
		}
		n := int(o.Uint32(b[4:]))
		if n > 4096 {
			return nil, errSquashFS
		}
		t, err := m.read(n)
		if err != nil {
			return nil, err
		}
		in.typ, in.target = sqSymlink, string(t)
	case 4, 5, 11, 12: // device
		b, err := m.read(8)
		if err != nil {
			return nil, err
		}
		rdev := int(o.Uint32(b[4:]))
		in.major, in.minor = (rdev>>8)&0xFFF, (rdev&0xFF)|((rdev>>12)&0xFFF00)
		in.typ = map[int]int{4: sqBlock, 5: sqChar, 11: sqBlock, 12: sqChar}[in.typ]
	case 6, 7, 13, 14:
		in.typ = map[int]int{6: sqFIFO, 7: sqSocket, 13: sqFIFO, 14: sqSocket}[in.typ]
	default:
		return nil, errSquashFS
	}
	if in.typ == sqFile {
		if in.size < 0 || in.start < 0 {
			return nil, errSquashFS
		}
		n := in.size / s.blockSize
		if in.frag == 0xFFFFFFFF && in.size%s.blockSize != 0 {
			n++
		}
		if n > len(s.img) {
			return nil, errSquashFS
		}
		if in.blocks, err = u32(n); err != nil {
			return nil, err
		}
	}
	return in, nil
}

// fragment returns the data of fragment i.
func (s *sqfs) fragment(i uint32) ([]byte, error) {
	if int(i) >= s.fragCount || s.flags&sqfsNoFragments != 0 {
		return nil, errSquashFS
	}
	p := s.fragTable + 8*int(i/512)
	if p < 0 || p+8 > len(s.img) {
		return nil, errSquashFS
	}
	m, err := s.meta(int(s.order.Uint64(s.img[p:])), 16*int(i%512))
	if err != nil {
		return nil, err
	}
	e, err := m.read(16)
	if err != nil {
		return nil, err
	}
	return s.block(int(s.order.Uint64(e)), s.order.Uint32(e[8:]))
}

// block reads a block of data at pos whose size field is size: the bit 24
// is set when the block is not compressed.
func (s *sqfs) block(pos int, size uint32) ([]byte, error) {
	n := int(size & 0xFFFFFF)
	if pos < 0 || pos+n > len(s.img) {
		return nil, errSquashFS
	}
	b := s.img[pos : pos+n]
	if size&0x1000000 != 0 {
		return b, nil
	}
	return s.decompress(b, s.blockSize)
}

// data writes the data of a file to w.
func (s *sqfs) data(in *inode, w func([]byte) bool) error {
	pos, left := in.start, in.size
	for _, b := range in.blocks {
		n := int(b & 0xFFFFFF)
		var d []byte
		if n == 0 {
			d = make([]byte, min(s.blockSize, left)) // a sparse block
		} else {
			var err error
			if d, err = s.block(pos, b); err != nil {
				return err
			}
			pos += n
		}
		d = d[:min(len(d), left)]
		if !w(d) {
			return errSquashFS
		}
		left -= len(d)
	}
	if left > 0 && in.frag != 0xFFFFFFFF {
		f, err := s.fragment(in.frag)
		if err != nil {
			return err
		}
		if in.fragOffset < 0 || in.fragOffset+left > len(f) {
			return errSquashFS
		}
		if !w(f[in.fragOffset : in.fragOffset+left]) {
			return errSquashFS
		}
		left = 0
	}
	if left != 0 {
		return errSquashFS
	}
	return nil
}

// entry is one entry of a folder.
type entry struct {
	name string
	ref  uint64
}

// dir reads the entries of a folder.
func (s *sqfs) dir(in *inode) ([]entry, error) {
	// The size of a folder counts three bytes more than its entries.
	left := in.size - 3
	if left <= 0 {
		return nil, nil
	}
	m, err := s.meta(s.dirTable+in.start, in.offset)
	if err != nil {
		return nil, err
	}
	var out []entry
	for left > 0 {
		h, err := m.read(12)
		if err != nil {
			return nil, err
		}
		left -= 12
		count, start := int(s.order.Uint32(h))+1, int(s.order.Uint32(h[4:]))
		if count > 256 {
			return nil, errSquashFS
		}
		for range count {
			e, err := m.read(8)
			if err != nil {
				return nil, err
			}
			n := int(s.order.Uint16(e[6:])) + 1
			name, err := m.read(n)
			if err != nil {
				return nil, err
			}
			left -= 8 + n
			ref := uint64(start)<<16 | uint64(s.order.Uint16(e))
			out = append(out, entry{name: string(name), ref: ref})
		}
	}
	return out, nil
}

// extractSquashFS writes the files of the image, as sasquatch writes them,
// under squashfs-root. A device, a FIFO and a socket are files that name
// them, as binwalk writes them. A link that leaves the root is cleaned as
// binwalk cleans the links of its own extractors, so that it stays inside.
func extractSquashFS(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := parseSquashFS(data, off)
	if err != nil {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	s, err := newSqfs(data[off : off+m.Size])
	if err != nil {
		return walk.Extraction{}
	}
	root, err := s.inode(s.rootRef)
	if err != nil || root.typ != sqDir || !out.CreateDirectory("squashfs-root") {
		return walk.Extraction{}
	}
	sub := out.Sub("squashfs-root")
	if err := s.write(sub, "", root, 0); err != nil {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true}
}

// write writes the entries of the folder dir at the path p.
func (s *sqfs) write(out *walk.Output, p string, dir *inode, depth int) error {
	if depth > 256 {
		return errSquashFS
	}
	entries, err := s.dir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.name == "." || e.name == ".." || e.name == "" {
			continue
		}
		in, err := s.inode(e.ref)
		if err != nil {
			return err
		}
		name := path.Join(p, e.name)
		ok := true
		switch in.typ {
		case sqDir:
			if !out.CreateDirectory(name) {
				return errSquashFS
			}
			if err := s.write(out, name, in, depth+1); err != nil {
				return err
			}
		case sqFile:
			ok = out.CreateFile(name, nil)
			if ok {
				err = s.data(in, func(b []byte) bool { return len(b) == 0 || out.AppendFile(name, b) })
				ok = err == nil
			}
			if ok && in.mode&0o111 != 0 {
				ok = out.MakeExecutable(name)
			}
		case sqSymlink:
			ok = out.CreateSymlink(name, in.target)
		case sqChar:
			ok = out.CreateCharDevice(name, in.major, in.minor)
		case sqBlock:
			ok = out.CreateBlockDevice(name, in.major, in.minor)
		case sqFIFO:
			ok = out.CreateFIFO(name)
		case sqSocket:
			ok = out.CreateSocket(name)
		}
		if !ok {
			return errSquashFS
		}
	}
	return nil
}
