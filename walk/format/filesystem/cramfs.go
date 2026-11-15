//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates the parser of src/formats/cramfs.rs of binwalk-ng.
// binwalk-ng extracts CramFS with 7z, from src/formats/sevenzip.rs, and
// extractCramFS writes the files that 7z writes. It copies the behavior of
// CramfsHandler.cpp of 7-Zip, not its code.

package filesystem

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"iter"

	"github.com/xo/magic/compress"
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// CramFS is the signature "cramfs" of binwalk. Its magic is the signature
// of the header, 16 bytes after the start of the image, which is the same
// in both byte orders.
var CramFS = &walk.Signature{
	Name:        "cramfs",
	Magic:       [][]byte{[]byte("Compressed ROMFS")},
	Description: "CramFS filesystem",
	Parse:       parseCramFS,
	Extract:     extractCramFS,
}

// The offsets in the header of CramFS.
const (
	cramfsSignatureOffset = 16
	cramfsCRCStart        = 32
	cramfsCRCEnd          = 36
)

// parseCramFS is cramfs_parser.
func parseCramFS(data []byte, off int) (walk.Match, error) {
	// binwalk-ng rejects a signature that starts before the image could.
	start := off - cramfsSignatureOffset
	if start < 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	h, ok := parseCramFSHeader(data[start:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	img, ok := binwalk.Range(data, start, start+h.size)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	// The checksum is the CRC of the image with the field of the checksum
	// set to 0.
	c := make([]byte, len(img))
	copy(c, img)
	for i := cramfsCRCStart; i < cramfsCRCEnd && i < len(c); i++ {
		c[i] = 0
	}
	m := walk.Match{Offset: start, Size: h.size, Confidence: walk.High}
	// An image with a wrong checksum can still extract, so binwalk only
	// lowers the confidence.
	errText := ""
	if binwalk.CRC32(c) != h.checksum {
		errText = " (checksum error)"
		m.Confidence = walk.Medium
	}
	m.Description = fmt.Sprintf("CramFS filesystem, %s, %d files, total size: %d bytes%s",
		h.endianness, h.fileCount, h.size, errText)
	return m, nil
}

var cramfsHeader = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "size", Type: "u32"},
	{Name: "flags", Type: "u32"},
	{Name: "future", Type: "u32"},
	{Name: "signature_p1", Type: "u64"},
	{Name: "signature_p2", Type: "u64"},
	{Name: "checksum", Type: "u32"},
	{Name: "edition", Type: "u32"},
	{Name: "block_count", Type: "u32"},
	{Name: "file_count", Type: "u32"},
}

// cramfsHeaderInfo is CramFSHeader.
type cramfsHeaderInfo struct {
	size       int
	checksum   uint32
	fileCount  int
	endianness string
}

// parseCramFSHeader is parse_cramfs_header. It reads the header in little
// endian, and reads it again in big endian when the magic says so.
func parseCramFSHeader(data []byte) (cramfsHeaderInfo, bool) {
	const (
		bigMagic    = 0x453DCD28
		littleMagic = 0x28CD3D45
	)
	h, ok := cramfsHeader.Parse(data, false)
	if !ok || (h["magic"] != bigMagic && h["magic"] != littleMagic) {
		return cramfsHeaderInfo{}, false
	}
	endianness := endianName(false)
	if h["magic"] == bigMagic {
		endianness = endianName(true)
		if h, ok = cramfsHeader.Parse(data, true); !ok {
			return cramfsHeaderInfo{}, false
		}
	}
	// The size of the image must be larger than the header.
	if h["size"] <= cramfsHeader.Size() {
		return cramfsHeaderInfo{}, false
	}
	return cramfsHeaderInfo{
		size:       h["size"],
		checksum:   uint32(h["checksum"]),
		fileCount:  h["file_count"],
		endianness: endianness,
	}, true
}

// extractCramFS writes the files of the image as 7z writes them. binwalk
// gives 7z the bytes of the match, so the image is the size of its header.
func extractCramFS(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := parseCramFS(data, off+cramfsSignatureOffset)
	if err != nil {
		return walk.Extraction{}
	}
	c, ok := openCramfs(data[off : off+m.Size])
	if !ok {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	return walk.Extraction{OK: writeSevenZip(out, c.items())}
}

// The constants of CramfsHandler.cpp.
const (
	cramfsHeaderSize   = 0x40
	cramfsNodeSize     = 12
	cramfsArcSizeMax   = (256 + 16) << 20
	cramfsNumFilesMax  = 1 << 19
	cramfsDirLevelsMax = 1 << 8
	cramfsBlockLog     = 12

	// cramfsLinkMax is the largest target of a link that the walker reads.
	// A target is at most PATH_MAX bytes on Linux. The walker writes a
	// larger link as a file, which is not checked against 7z.
	cramfsLinkMax = 4096

	cramfsFlagVer2   = 1
	cramfsMethodZlib = 1
	cramfsMethodLZMA = 2
)

// cramfs is an image of CramFS as CHandler of CramfsHandler.cpp opens it.
type cramfs struct {
	img      []byte
	be       bool
	method   int
	blockLog int
	nodes    []cramfsNode

	// fatal is set when a block decodes to more bytes than the buffer of
	// 7-Zip for a block holds. The buffer then fails with E_FAIL, which
	// stops 7z.
	fatal bool
}

// cramfsNode is CItem: the offset of the node of an item, and the index of
// its parent, or -1 for the root.
type cramfsNode struct {
	offset int
	parent int
}

// openCramfs is Open2 of CramfsHandler.cpp. 7-Zip fails to open an image
// whose tree it cannot read, and then 7z writes nothing.
func openCramfs(img []byte) (*cramfs, bool) {
	if len(img) < cramfsHeaderSize || !bytes.Equal(img[16:32], []byte("Compressed ROMFS")) {
		return nil, false
	}
	c := &cramfs{img: img, method: cramfsMethodZlib, blockLog: cramfsBlockLog}
	switch binary.LittleEndian.Uint32(img) {
	case 0x28CD3D45:
	case 0x453DCD28:
		c.be = true
	default:
		return nil, false
	}
	size, flags, numFiles := int(c.u32(4)), c.u32(8), int(c.u32(0x2C))
	if flags&cramfsFlagVer2 != 0 {
		// A method of 0 is zlib, as in the images of Linux.
		if m := int(flags>>14) & 3; m != 0 {
			c.method = m
		}
		c.blockLog += int(flags>>11) & 7
		if size < cramfsHeaderSize || size > cramfsArcSizeMax || numFiles > cramfsNumFilesMax {
			return nil, false
		}
		// 7-Zip reads the image up to the size of its header, and binwalk
		// gives it the bytes of that size.
		c.img = img[:min(size, len(img))]
	} else {
		c.img = img[:min(len(img), cramfsArcSizeMax)]
	}
	if len(c.img)-cramfsHeaderSize < cramfsNodeSize {
		return nil, false
	}
	if !c.openDir(-1, cramfsHeaderSize, 0) {
		return nil, false
	}
	return c, true
}

func (c *cramfs) u32(off int) uint32 {
	if c.be {
		return binary.BigEndian.Uint32(c.img[off:])
	}
	return binary.LittleEndian.Uint32(c.img[off:])
}

func (c *cramfs) mode(node int) int {
	if c.be {
		return int(binary.BigEndian.Uint16(c.img[node:]))
	}
	return int(binary.LittleEndian.Uint16(c.img[node:]))
}

func (c *cramfs) isDir(node int) bool {
	return c.mode(node)&0xF000 == 0x4000
}

// size is the size of a file, 24 bits.
func (c *cramfs) size(node int) int {
	if c.be {
		return int(binary.BigEndian.Uint32(c.img[node+4:]) >> 8)
	}
	return int(binary.LittleEndian.Uint32(c.img[node+4:]) & 0xFFFFFF)
}

// nameLen is the bytes of the name, a multiple of 4.
func (c *cramfs) nameLen(node int) int {
	if c.be {
		return int(c.img[node+8] & 0xFC)
	}
	return int(c.img[node+8]&0x3F) << 2
}

// dataOffset is the offset of the data of a file, or of the nodes of a
// folder.
func (c *cramfs) dataOffset(node int) int {
	if c.be {
		return int(binary.BigEndian.Uint32(c.img[node+8:])&0x03FFFFFF) << 2
	}
	return int(binary.LittleEndian.Uint32(c.img[node+8:])>>6) << 2
}

// openDir is OpenDir: it adds the items of the folder whose node is at
// node, and then the items of each folder among them.
func (c *cramfs) openDir(parent, node, level int) bool {
	if node+cramfsNodeSize > len(c.img) {
		return false
	}
	if !c.isDir(node) {
		return true
	}
	offset, size := c.dataOffset(node), c.size(node)
	if offset == 0 && size == 0 {
		return true
	}
	if offset < cramfsHeaderSize || offset+size > len(c.img) || level > cramfsDirLevelsMax {
		return false
	}
	start := len(c.nodes)
	for size != 0 {
		if size < cramfsNodeSize || len(c.nodes) >= cramfsNumFilesMax {
			return false
		}
		c.nodes = append(c.nodes, cramfsNode{offset: offset, parent: parent})
		n := cramfsNodeSize + c.nameLen(offset)
		if size < n {
			return false
		}
		offset += n
		size -= n
	}
	end := len(c.nodes)
	for i := start; i < end; i++ {
		if !c.openDir(i, c.nodes[i].offset, level+1) {
			return false
		}
	}
	return true
}

// path is GetPath: the names from the root to the item, each up to its
// first NUL.
func (c *cramfs) path(i int) string {
	var names []string
	for ; i >= 0; i = c.nodes[i].parent {
		node := c.nodes[i].offset
		names = append(names, binwalk.CString(c.img[node+cramfsNodeSize:node+cramfsNodeSize+c.nameLen(node)]))
	}
	var b bytes.Buffer
	for j := len(names) - 1; j >= 0; j-- {
		b.WriteString(names[j])
		if j > 0 {
			b.WriteByte('/')
		}
	}
	return b.String()
}

// items lists the items in the order that 7z extracts them. 7z reads a
// link from the data of the file, as the posix mode of the item says, and
// sets no mode on a link.
func (c *cramfs) items() []szItem {
	items := make([]szItem, 0, len(c.nodes))
	fatal := func() bool { return c.fatal }
	for i, n := range c.nodes {
		mode := c.mode(n.offset)
		it := szItem{path: c.path(i), mode: mode, hasMode: true, fatal: fatal}
		isLink := mode&0xF000 == 0xA000
		switch {
		case c.isDir(n.offset):
			it.dir = true
		case isLink && c.size(n.offset) <= cramfsLinkMax:
			it.linkData, it.linkSize = c.data(n.offset), c.size(n.offset)
		default:
			it.data = c.data(n.offset)
			it.hasMode = !isLink
		}
		items = append(items, it)
	}
	return items
}

// data is GetStream with the reads of ReadBlock: the blocks of the file at
// node, up to the first block that does not decode.
func (c *cramfs) data(node int) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		size, offset := c.size(node), c.dataOffset(node)
		// A file with no data, such as a device, has the offset 0.
		if offset < cramfsHeaderSize {
			return
		}
		bs := 1 << c.blockLog
		n := (size + bs - 1) >> c.blockLog
		table := offset + n*4
		if table > len(c.img) {
			return
		}
		prev := offset
		for i := range n {
			next := int(c.u32(offset + i*4))
			if next < prev || next > len(c.img) {
				return
			}
			prev = next
		}
		start := table
		for i := range n {
			end := int(c.u32(offset + i*4))
			if end < start {
				return
			}
			want := min(bs, size-i*bs)
			b, ok := c.block(c.img[start:end], want, bs)
			if !ok || !yield(b) {
				return
			}
			start = end
		}
	}
}

// block is ReadBlock: it decodes one block of want bytes into a buffer of
// bs bytes.
func (c *cramfs) block(src []byte, want, bs int) ([]byte, bool) {
	switch c.method {
	case cramfsMethodZlib:
		// 7-Zip wants a zlib stream that takes all of src and gives want
		// bytes. So a hole, a block of no bytes, fails.
		r := bytes.NewReader(src)
		z, err := zlib.NewReader(r)
		if err != nil {
			return nil, false
		}
		b, err := io.ReadAll(io.LimitReader(z, int64(bs)+1))
		if len(b) > bs {
			c.fatal = true
			return nil, false
		}
		if err != nil || len(b) != want || r.Len() != 0 {
			return nil, false
		}
		return b, true
	case cramfsMethodLZMA:
		return cramfsLZMA(src, want)
	}
	return nil, false
}

// cramfsLZMA decodes a block of LZMA: the 5 bytes of the properties, the
// size of the data in 32 bits, and the stream with no header. 7-Zip accepts
// a block that gives fewer bytes than the block, and leaves the rest of its
// buffer as it was. The walker fills the rest with zeros.
func cramfsLZMA(src []byte, want int) ([]byte, bool) {
	const props = 5
	if len(src) < props+4 {
		return nil, false
	}
	size := int(binary.LittleEndian.Uint32(src[props:]))
	if size > want {
		return nil, false
	}
	dec, ok := compress.LookupStream(compress.LZMA)
	if !ok {
		return nil, false
	}
	// The decoder reads the header of the format lzma_alone: the
	// properties, then the size in 64 bits.
	s := make([]byte, 0, props+8+len(src)-props-4)
	s = append(s, src[:props]...)
	s = binary.LittleEndian.AppendUint64(s, uint64(size))
	s = append(s, src[props+4:]...)
	var b bytes.Buffer
	if _, err := dec(s, &b); err != nil || b.Len() != size {
		return nil, false
	}
	out := make([]byte, want)
	copy(out, b.Bytes())
	return out, true
}
