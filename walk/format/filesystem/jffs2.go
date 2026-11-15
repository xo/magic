//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/jffs2.rs and src/structures/jffs2.rs
// of binwalk. binwalk extracts JFFS2 with jefferson, from
// src/extractors/jffs2.rs, and extractJFFS2 writes the files that jefferson
// writes.

package filesystem

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/xo/magic/compress"
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// JFFS2 is the signature "jffs2" of binwalk.
var JFFS2 = &walk.Signature{
	Name: "jffs2",
	// The first node is a folder entry, an inode or a clean marker, in
	// either byte order.
	Magic: [][]byte{
		[]byte("\x19\x85\xe0\x01"), []byte("\x19\x85\xe0\x02"), []byte("\x19\x85\x20\x03"),
		[]byte("\x85\x19\x01\xe0"), []byte("\x85\x19\x02\xe0"), []byte("\x85\x19\x03\x20"),
	},
	Description: "JFFS2 filesystem",
	Parse:       parseJFFS2,
	Extract:     extractJFFS2,
}

// jffs2NodeStruct is the header of every node.
var jffs2NodeStruct = binwalk.Struct{
	{Name: "magic", Type: "u16"},
	{Name: "type", Type: "u16"},
	{Name: "size", Type: "u32"},
	{Name: "crc", Type: "u32"},
}

// jffs2HeaderSize is JFFS2_NODE_STRUCT_SIZE, the bytes of a node header.
const jffs2HeaderSize = 12

// parseJFFS2 is jffs2_parser. It finds the nodes that follow the first one,
// each within 128 KiB of the end of the one before, and needs three of
// them. The size runs to the end of the last node, and can pass the end of
// the data, as in binwalk.
func parseJFFS2(data []byte, off int) (walk.Match, error) {
	const (
		maxPageSize  = 0x20000
		minNodeCount = 2
	)
	first, ok := jffs2NodeHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	eof := off + jffs2Pad(first.size)
	if eof >= len(data) {
		return walk.Match{}, walk.ErrNoMatch
	}
	magic, endian := []byte("\x85\x19"), "little"
	if first.big {
		magic, endian = []byte("\x19\x85"), "big"
	}
	count := 1
	// binwalk visits every match of the magic after the first node. A
	// match before the end of the last node is inside its data, so the
	// search goes on from that end.
	for p := eof; p < len(data); p++ {
		i := bytes.Index(data[p:], magic)
		if i < 0 {
			break
		}
		start := p + i
		if start < eof {
			p = eof - 1
			continue
		}
		p = start
		if start-eof > maxPageSize {
			break
		}
		hdr, ok := binwalk.Range(data, start, start+jffs2HeaderSize)
		if !ok {
			break
		}
		if h, ok := jffs2NodeHeader(hdr); ok {
			count++
			eof = start + jffs2Pad(h.size)
		}
	}
	if count <= minNodeCount {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := eof - off
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("JFFS2 filesystem, %s endian, nodes: %d, total size: %d bytes", endian, count, size),
	}, nil
}

// jffs2Header is a node header that parse_jffs2_node_header accepts.
type jffs2Header struct {
	size int
	typ  int
	big  bool
}

// jffs2NodeHeader is parse_jffs2_node_header. It reads the header as little
// endian, then as big endian when the magic does not match, and checks the
// CRC of the first eight bytes.
func jffs2NodeHeader(data []byte) (jffs2Header, bool) {
	const magic = 0x1985
	f, ok := jffs2NodeStruct.Parse(data, false)
	if !ok {
		return jffs2Header{}, false
	}
	big := false
	if f["magic"] != magic {
		if f, ok = jffs2NodeStruct.Parse(data, true); !ok {
			return jffs2Header{}, false
		}
		big = true
	}
	if f["magic"] != magic || int(jffs2CRC(data[:8])) != f["crc"] {
		return jffs2Header{}, false
	}
	return jffs2Header{size: f["size"], typ: f["type"], big: big}, true
}

// jffs2CRC is jffs2_node_crc, the CRC-32 of JFFS2: the CRC of zlib with no
// inversion at the start or at the end. jefferson computes the same value
// as mtd_crc.
func jffs2CRC(data []byte) uint32 {
	return ^crc32.Update(^uint32(0), crc32.IEEETable, data)
}

// jffs2Pad is roundup: nodes start at a multiple of four bytes.
func jffs2Pad(n int) int {
	return (n + 3) &^ 3
}

// The node types, compressions and modes that jefferson reads.
const (
	jffs2Dirent = 0xE001
	jffs2Inode  = 0xE002

	jffs2ComprNone       = 0x00
	jffs2ComprZero       = 0x01
	jffs2ComprRtime      = 0x02
	jffs2ComprZlib       = 0x06
	jffs2ComprLZO        = 0x07
	jffs2ComprLZMA       = 0x08
	jffs2ComprLZMANoSize = 0x15

	jffs2DirentSize = 40
	jffs2InodeSize  = 68

	sIFMT   = 0o170000
	sIFSOCK = 0o140000
	sIFLNK  = 0o120000
	sIFREG  = 0o100000
	sIFBLK  = 0o060000
	sIFDIR  = 0o040000
	sIFCHR  = 0o020000
	sIFIFO  = 0o010000
)

// The limits of the extractor. jefferson has none. A node of JFFS2 holds
// at most one page of data, 4 KiB on most systems, so a node that decodes
// to more than jffs2MaxNode is hostile.
const (
	jffs2MaxNode    = 1 << 20
	jffs2MaxDecoded = 256 << 20
)

var (
	// errJFFS2Stop is where jefferson stops with an exception. What it
	// wrote stays, and binwalk accepts its exit code.
	errJFFS2Stop = errors.New("jffs2: jefferson stops")
	// errJFFS2Fail fails the extraction: a limit of the walker, a decoder
	// that the program does not register, or an output that fails.
	errJFFS2Fail = errors.New("jffs2: the extraction fails")
)

// extractJFFS2 writes what `jefferson -f -d jffs2-root` writes from the
// bytes of the match. jefferson finds every node, keeps the latest folder
// entry of each inode number, and writes each entry under the path of its
// parents, from all the data nodes of its inode.
//
// jefferson makes jffs2-root before it reads the image, and binwalk counts a
// folder as something extracted. So an image that jefferson cannot read
// gives an empty jffs2-root, as in binwalk.
func extractJFFS2(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := parseJFFS2(data, off)
	if err != nil {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	img, ok := binwalk.Range(data, off, off+m.Size)
	if !ok || !out.CreateDirectory("jffs2-root") {
		return walk.Extraction{}
	}
	fs, err := scanJFFS2(img)
	if err == nil {
		w := &jffs2Writer{out: out.Sub("jffs2-root"), order: fs.bo, made: map[string]*jffs2Made{}}
		err = w.dump(fs)
	}
	if errors.Is(err, errJFFS2Fail) {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true}
}

// jffs2Entry is a folder entry, Jffs2_raw_dirent of jefferson.
type jffs2Entry struct {
	pino, version, ino uint32
	name               []byte
}

// jffs2Data is an inode node, Jffs2_raw_inode of jefferson, with its data.
// A node that jefferson fills with zeros holds no slice, and zeros counts
// the bytes.
type jffs2Data struct {
	ino, version, mode, offset, dsize uint32

	data  []byte
	zeros int
}

// size is the bytes of the data of the node.
func (d *jffs2Data) size() int {
	if d.data == nil {
		return d.zeros
	}
	return len(d.data)
}

// head returns the first n bytes of the data, and false when it is shorter.
func (d *jffs2Data) head(n int) ([]byte, bool) {
	if d.size() < n {
		return nil, false
	}
	if d.data == nil {
		return make([]byte, n), true
	}
	return d.data[:n], true
}

// jffs2FS is the result of scan_fs: the folder entries by inode number, in
// the order that jefferson first saw each number, and the inode nodes.
type jffs2FS struct {
	order   []uint32
	entries map[uint32]*jffs2Entry
	inodes  map[uint32][]*jffs2Data
	bo      binary.ByteOrder
	decoded int
}

// scanJFFS2 is scan_fs. jefferson reads the byte order from the first two
// bytes of the image, and finds the magic of 1985 and the old magic of 1984.
// When both are ahead, it takes the one of 1985 even when the other comes
// first, as jefferson does.
func scanJFFS2(img []byte) (*jffs2FS, error) {
	var order binary.ByteOrder = binary.BigEndian
	magic, old := []byte{0x19, 0x85}, []byte{0x19, 0x84}
	if v := binary.LittleEndian.Uint16(img); v == 0x1984 || v == 0x1985 {
		order = binary.LittleEndian
		magic, old = []byte{0x85, 0x19}, []byte{0x84, 0x19}
	}
	fs := &jffs2FS{entries: map[uint32]*jffs2Entry{}, inodes: map[uint32][]*jffs2Data{}, bo: order}
	// find is content.find(m, pos, len(content) - 12). Each search keeps
	// its result while it is ahead of pos, and -1 stays -1.
	end := len(img) - jffs2HeaderSize
	pos := 0
	find := func(m []byte, last *int) int {
		if *last == -2 || (*last >= 0 && *last < pos) {
			*last = -1
			if pos <= end {
				if i := bytes.Index(img[pos:end], m); i >= 0 {
					*last = pos + i
				}
			}
		}
		return *last
	}
	nextNew, nextOld := -2, -2
	for {
		n, o := find(magic, &nextNew), find(old, &nextOld)
		if n == -1 && o == -1 {
			break
		}
		pos = n
		if n == -1 {
			pos = o
		}
		hdr := img[pos : pos+jffs2HeaderSize]
		typ := order.Uint16(hdr[2:])
		totlen := int(order.Uint32(hdr[4:]))
		if jffs2CRC(hdr[:8]) != order.Uint32(hdr[8:]) || totlen < jffs2HeaderSize {
			pos++
			continue
		}
		node := img[pos:min(pos+totlen, len(img))]
		pos += jffs2Pad(totlen)
		var err error
		switch typ {
		case jffs2Dirent:
			err = fs.dirent(node, order)
		case jffs2Inode:
			err = fs.inode(node, order)
		}
		if err != nil {
			return nil, err
		}
	}
	return fs, nil
}

// dirent is parse_dirent, and keeps the entry of the highest version of
// each inode number. A node too short for its structure stops jefferson.
func (fs *jffs2FS) dirent(node []byte, order binary.ByteOrder) error {
	if len(node) < jffs2DirentSize {
		return errJFFS2Stop
	}
	e := &jffs2Entry{
		pino:    order.Uint32(node[12:]),
		version: order.Uint32(node[16:]),
		ino:     order.Uint32(node[20:]),
	}
	e.name = node[jffs2DirentSize:min(jffs2DirentSize+int(node[28]), len(node))]
	if prev, ok := fs.entries[e.ino]; !ok {
		fs.order = append(fs.order, e.ino)
		fs.entries[e.ino] = e
	} else if e.version > prev.version {
		fs.entries[e.ino] = e
	}
	return nil
}

// inode is parse_inode. jefferson keeps every inode node, and does not
// reject a node whose CRC does not match.
func (fs *jffs2FS) inode(node []byte, order binary.ByteOrder) error {
	if len(node) < jffs2InodeSize {
		return errJFFS2Stop
	}
	d := &jffs2Data{
		ino:     order.Uint32(node[12:]),
		version: order.Uint32(node[16:]),
		mode:    order.Uint32(node[20:]),
		offset:  order.Uint32(node[44:]),
		dsize:   order.Uint32(node[52:]),
	}
	csize := int(order.Uint32(node[48:]))
	src := node[jffs2InodeSize:min(jffs2InodeSize+csize, len(node))]
	if err := fs.decode(d, node[56], src); err != nil {
		return err
	}
	fs.decoded += len(d.data)
	if fs.decoded > jffs2MaxDecoded {
		return errJFFS2Fail
	}
	fs.inodes[d.ino] = append(fs.inodes[d.ino], d)
	return nil
}

// decode sets the data of d, as parse_inode does. jefferson keeps the data
// of a compression that it does not know, such as rubin, as it is, and
// fills the node with zeros when its decoder fails.
func (fs *jffs2FS) decode(d *jffs2Data, compr byte, src []byte) error {
	var out []byte
	var err error
	switch compr {
	case jffs2ComprNone:
		d.data = src
		return nil
	case jffs2ComprZero:
		d.zeros = int(d.dsize)
		return nil
	case jffs2ComprZlib:
		out, err = jffs2Zlib(src)
	case jffs2ComprRtime:
		if d.dsize > jffs2MaxNode {
			return errJFFS2Fail
		}
		out, err = jffs2Rtime(src, int(d.dsize))
	case jffs2ComprLZMA:
		// jffs2_lzma adds the header of LZMA_alone: lc, lp and pb of 0, a
		// dictionary of 8 KiB, and the size of the data.
		h := binary.LittleEndian.AppendUint32([]byte{0}, 0x2000)
		h = binary.LittleEndian.AppendUint64(h, uint64(d.dsize))
		out, err = jffs2LZMA(append(h, src...))
	case jffs2ComprLZMANoSize:
		// The stream has its properties and dictionary, and no size.
		h := slices.Clone(src[:min(5, len(src))])
		h = binary.LittleEndian.AppendUint64(h, uint64(d.dsize))
		out, err = jffs2LZMA(append(h, src[min(5, len(src)):]...))
	case jffs2ComprLZO:
		out, err = lzo1x(src, jffs2MaxNode)
	default:
		d.data = src
		return nil
	}
	switch {
	case errors.Is(err, errJFFS2Fail):
		return err
	case err != nil:
		d.zeros = int(d.dsize)
	default:
		d.data = out
	}
	return nil
}

// jffs2Zlib is zlib.decompress of Python: it reads one zlib stream and
// checks its Adler-32, and ignores the bytes after it.
func jffs2Zlib(src []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("jffs2: %w", err)
	}
	out, err := io.ReadAll(io.LimitReader(r, jffs2MaxNode+1))
	if err != nil {
		return nil, fmt.Errorf("jffs2: %w", err)
	}
	if len(out) > jffs2MaxNode {
		return nil, errJFFS2Fail
	}
	return out, nil
}

// jffs2LZMA is lzma.decompress of Python on the data of LZMA_alone. It
// decodes the first stream, and ignores the bytes after it.
func jffs2LZMA(src []byte) ([]byte, error) {
	dec, ok := compress.LookupStream(compress.LZMA)
	if !ok {
		return nil, errJFFS2Fail
	}
	w := &jffs2Limit{n: jffs2MaxNode}
	if _, err := dec(src, w); err != nil {
		if w.over {
			return nil, errJFFS2Fail
		}
		return nil, fmt.Errorf("jffs2: %w", err)
	}
	return w.buf, nil
}

// jffs2Limit keeps at most n bytes, and fails past them.
type jffs2Limit struct {
	buf  []byte
	n    int
	over bool
}

func (w *jffs2Limit) Write(p []byte) (int, error) {
	if len(w.buf)+len(p) > w.n {
		w.over = true
		return 0, errJFFS2Fail
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}

// errRtime is the IndexError of rtime.decompress, which jefferson catches.
var errRtime = errors.New("jffs2: the rtime data is not valid")

// jffs2Rtime is rtime.decompress of jefferson. A copy that ends past the
// output makes the output longer, as the assignment of a slice of a
// bytearray in Python does.
func jffs2Rtime(in []byte, destlen int) ([]byte, error) {
	var positions [256]int
	out := make([]byte, destlen)
	outpos, pos := 0, 0
	for outpos < destlen {
		if pos+2 > len(in) {
			return nil, errRtime
		}
		value, repeat := in[pos], int(in[pos+1])
		pos += 2
		out[outpos] = value
		outpos++
		backoffs := positions[value]
		positions[value] = outpos
		if repeat == 0 {
			continue
		}
		if backoffs+repeat >= outpos {
			for ; repeat > 0; repeat-- {
				if outpos >= len(out) {
					return nil, errRtime
				}
				out[outpos] = out[backoffs]
				outpos++
				backoffs++
			}
			continue
		}
		src := slices.Clone(out[backoffs : backoffs+repeat])
		out = slices.Concat(out[:outpos], src, out[min(outpos+repeat, len(out)):])
		outpos += repeat
	}
	return out, nil
}

// jffs2Made is what jefferson made at a path: a folder, a file, a link
// with its target, or a device.
type jffs2Made struct {
	kind   int // one of sIFDIR, sIFREG, sIFLNK and sIFCHR
	target string
}

// jffs2Writer is dump_fs. made holds what it wrote, by the path under
// jffs2-root, so that it can do what the calls of os of jefferson do on
// what exists.
type jffs2Writer struct {
	out   *walk.Output
	order binary.ByteOrder
	made  map[string]*jffs2Made
}

// dump is dump_fs. Each entry gets the path of its parents, up to 100 of
// them. jefferson resolves the path with os.path.realpath, and skips an
// entry whose path leaves the output.
func (w *jffs2Writer) dump(fs *jffs2FS) error {
	for _, ino := range fs.order {
		e := fs.entries[ino]
		inodes := slices.Clone(fs.inodes[e.ino])
		slices.SortStableFunc(inodes, func(a, b *jffs2Data) int {
			switch {
			case a.version < b.version:
				return -1
			case a.version > b.version:
				return 1
			}
			return 0
		})
		names := [][]byte{e.name}
		pino := e.pino
		for range 100 {
			p, ok := fs.entries[pino]
			if !ok {
				break
			}
			pino = p.pino
			names = append(names, p.name)
		}
		slices.Reverse(names)
		var parts []string
		for _, n := range names {
			// bytes.decode() of Python raises on a name that is not UTF-8,
			// and realpath raises on a NUL. Both stop jefferson.
			if !utf8.Valid(n) || bytes.IndexByte(n, 0) >= 0 {
				return errJFFS2Stop
			}
			parts = append(parts, string(n))
		}
		p, ok := w.resolve(strings.Join(parts, "/"))
		if !ok {
			continue
		}
		if err := w.write(p, inodes); err != nil {
			return err
		}
	}
	return nil
}

// resolve is os.path.realpath of the path under the output, and the check
// of is_safe_path. It follows the links that jefferson made, and reports
// false for a path that leaves the output.
//
// realpath gives a path outside the output for an absolute link, and for a
// ".." above the output, even when the rest of the path comes back in. A
// loop of links leaves the path out too.
func (w *jffs2Writer) resolve(p string) (string, bool) {
	if strings.HasPrefix(p, "/") {
		// os.path.join drops the output before an absolute path.
		return "", false
	}
	queue := strings.Split(p, "/")
	var stack []string
	for hops := 0; len(queue) > 0; {
		c := queue[0]
		queue = queue[1:]
		switch c {
		case "", ".":
			continue
		case "..":
			if len(stack) == 0 {
				return "", false
			}
			stack = stack[:len(stack)-1]
			continue
		}
		cand := strings.Join(append(stack, c), "/")
		if m, ok := w.made[cand]; ok && m.kind == sIFLNK {
			hops++
			if hops > 40 || strings.HasPrefix(m.target, "/") {
				return "", false
			}
			queue = append(strings.Split(m.target, "/"), queue...)
			continue
		}
		stack = append(stack, c)
	}
	return strings.Join(stack, "/"), true
}

// kind is what exists at p: a folder for the output itself, 0 for nothing.
func (w *jffs2Writer) kind(p string) int {
	if p == "" {
		return sIFDIR
	}
	if m, ok := w.made[p]; ok {
		return m.kind
	}
	return 0
}

// parent is os.path.dirname of p.
func parent(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// mkdirs is os.makedirs. It fails on a part of the path that exists and is
// not a folder.
func (w *jffs2Writer) mkdirs(p string) (bool, error) {
	parts := strings.Split(p, "/")
	for i := range parts {
		q := strings.Join(parts[:i+1], "/")
		switch w.kind(q) {
		case sIFDIR:
			continue
		case 0:
			if !w.out.CreateDirectory(q) {
				return false, errJFFS2Fail
			}
			w.made[q] = &jffs2Made{kind: sIFDIR}
		default:
			return false, nil
		}
	}
	return true, nil
}

// write is the loop of dump_fs over the inodes of one entry. jefferson
// catches an OSError and goes on with the next inode. A device that it
// cannot read, or a link to a target with a NUL, raises another error,
// which stops it.
func (w *jffs2Writer) write(p string, inodes []*jffs2Data) error {
	for _, in := range inodes {
		switch in.mode & sIFMT {
		case sIFDIR:
			if w.kind(p) != sIFDIR {
				if _, err := w.mkdirs(p); err != nil {
					return err
				}
			}
		case sIFLNK:
			if w.kind(p) != 0 {
				continue
			}
			if in.zeros > 0 || bytes.IndexByte(in.data, 0) >= 0 {
				return errJFFS2Stop
			}
			if len(in.data) == 0 || w.kind(parent(p)) != sIFDIR {
				continue
			}
			if !w.out.CreateSymlink(p, string(in.data)) {
				return errJFFS2Fail
			}
			w.made[p] = &jffs2Made{kind: sIFLNK, target: string(in.data)}
		case sIFREG:
			if w.kind(p) == sIFREG {
				return nil
			}
			if w.kind(parent(p)) != sIFDIR {
				ok, err := w.mkdirs(parent(p))
				if err != nil {
					return err
				}
				if !ok {
					continue
				}
			}
			if w.kind(p) != 0 {
				continue
			}
			return w.file(p, inodes)
		case sIFCHR, sIFBLK:
			major, minor, ok, err := jffs2Device(in, w.order)
			if err != nil {
				return err
			}
			if !ok || w.kind(p) != 0 || w.kind(parent(p)) != sIFDIR {
				continue
			}
			made := w.out.CreateCharDevice
			if in.mode&sIFMT == sIFBLK {
				made = w.out.CreateBlockDevice
			}
			if !made(p, major, minor) {
				return errJFFS2Fail
			}
			w.made[p] = &jffs2Made{kind: sIFCHR}
		}
	}
	return nil
}

// file writes a regular file as jefferson does: each inode node in the
// order of its version, at its offset, so that a later version writes over
// an earlier one. The file ends at the end of the last data, with zeros in
// the holes. The mode of the newest node gives the bit of execution.
func (w *jffs2Writer) file(p string, inodes []*jffs2Data) error {
	size := 0
	for _, in := range inodes {
		if n := in.size(); n > 0 {
			size = max(size, int(in.offset)+n)
		}
	}
	if !w.out.CreateFile(p, nil) {
		return errJFFS2Fail
	}
	w.made[p] = &jffs2Made{kind: sIFREG}
	// The file is written in windows, so that a hole of 4 GiB is not in
	// memory at once, and the limit of the output stops it.
	const window = 64 << 10
	buf := make([]byte, window)
	for start := 0; start < size; start += window {
		b := buf[:min(window, size-start)]
		clear(b)
		for _, in := range inodes {
			lo, hi := int(in.offset), int(in.offset)+in.size()
			lo, hi = max(lo, start), min(hi, start+len(b))
			if lo >= hi {
				continue
			}
			if in.data == nil {
				clear(b[lo-start : hi-start])
			} else {
				copy(b[lo-start:hi-start], in.data[lo-int(in.offset):])
			}
		}
		if !w.out.AppendFile(p, b) {
			return errJFFS2Fail
		}
	}
	if inodes[len(inodes)-1].mode&0o111 != 0 && !w.out.MakeExecutable(p) {
		return errJFFS2Fail
	}
	return nil
}

// jffs2Device is get_device: the major and minor numbers of the new form of
// 32 bits, or of the old form of 16 bits, by the size of the data. It
// reports false for another size. The numbers are in the byte order of the
// image. Data shorter than its size stops
// jefferson.
func jffs2Device(in *jffs2Data, order binary.ByteOrder) (int, int, bool, error) {
	switch in.dsize {
	case 4:
		b, ok := in.head(4)
		if !ok {
			return 0, 0, false, errJFFS2Stop
		}
		id := int(order.Uint32(b))
		return (id & 0xFFF00) >> 8, (id & 0xFF) | ((id >> 12) & 0xFFF00), true, nil
	case 2:
		b, ok := in.head(2)
		if !ok {
			return 0, 0, false, errJFFS2Stop
		}
		id := int(order.Uint16(b))
		return (id >> 8) & 0xFF, id & 0xFF, true, nil
	}
	return 0, 0, false, nil
}
