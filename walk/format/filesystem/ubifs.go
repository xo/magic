//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package filesystem

import (
	"bytes"
	"cmp"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/xo/magic/compress"
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/lzo"
)

// The errors of the reader of UBIFS. errUBIFSFatal is an error that stops
// ubireader_extract_files with an exit code that is not 0, so that binwalk
// fails. errUBIFSStop is an exception that ubireader_extract_files logs:
// it stops the extraction, and keeps what it wrote.
var (
	errUBIFSFatal = errors.New("ubifs: the image is not valid")
	errUBIFSStop  = errors.New("ubifs: the extraction stops")
)

// The constants of UBIFS.
const (
	ubifsCommonSize = 24   // the common header of every node
	ubifsBlockSize  = 4096 // UBIFS_BLOCK_SIZE, the data of one data node
	ubifsKeyBits    = 29   // UBIFS_S_KEY_BLOCK_BITS
	ubifsHashMask   = 0x1FFFFFFF

	// The node types.
	ubifsInoNode  = 0
	ubifsDataNode = 1
	ubifsDentNode = 2
	ubifsXentNode = 3
	ubifsPadNode  = 5
	ubifsSBNode   = 6
	ubifsMstNode  = 7
	ubifsIdxNode  = 9

	// The sizes of the nodes after the common header, as ubi_reader reads them.
	ubifsInoSize  = 136
	ubifsDataSize = 24
	ubifsDentSize = 32
	ubifsMstSize  = 488

	// The types of an inode in a directory entry.
	ubifsTypeReg  = 0
	ubifsTypeDir  = 1
	ubifsTypeLnk  = 2
	ubifsTypeFIFO = 5

	// Python stops at a depth of about 1000 calls with an exception.
	ubifsMaxDepth = 990
)

// ubifsReader reads an UBIFS image, as the class ubifs of ubi_reader.
type ubifsReader struct {
	img     []byte
	lebSize int
	inodes  map[uint64]*ubifsInode
	visits  int
	badName bool // a name of an entry is not valid UTF-8
}

// ubifsInode is what the index gives for one inode number.
type ubifsInode struct {
	ino   *ubifsIno // the last inode node that the walk of the index found
	data  []ubifsData
	dents []ubifsDent
	hlink string // the path of the first hard link that was written
}

type ubifsIno struct {
	size  uint64
	nlink uint32
	data  []byte
}

type ubifsData struct {
	khash   uint32
	size    uint32
	compr   uint16
	payload []byte
}

type ubifsDent struct {
	inum uint64
	typ  uint8
	name string
}

// newUBIFS reads the superblock at the start of img, and the master node.
// ubi_reader reads the size of a logical erase block from the superblock.
func newUBIFS(img []byte) (*ubifsReader, uint32, uint32, error) {
	if len(img) < ubifsCommonSize+4072 || img[20] != ubifsSBNode {
		return nil, 0, 0, errUBIFSFatal
	}
	r := &ubifsReader{img: img, lebSize: int(binary.LittleEndian.Uint32(img[ubifsCommonSize+12:])), inodes: map[uint64]*ubifsInode{}}
	if r.lebSize <= 0 {
		return nil, 0, 0, errUBIFSFatal
	}
	lnum, offs, ok := r.master()
	if !ok {
		return nil, 0, 0, errUBIFSFatal
	}
	return r, lnum, offs, nil
}

// master reads the master nodes in the logical erase blocks 1 and 2, as
// the constructor of ubifs of ubi_reader. In each block, it takes the node
// with the largest commit number. It uses the node of block 1, or the node
// of block 2 when block 1 has none. It gives the place of the root of the
// index.
func (r *ubifsReader) master() (lnum, offs uint32, ok bool) {
	for i := 1; i <= 2; i++ {
		start := r.lebSize * i
		var best []byte
		var bestCmt uint64
		for off := start; off < start+r.lebSize; {
			if off+ubifsCommonSize > len(r.img) {
				break
			}
			n := int(binary.LittleEndian.Uint32(r.img[off+16:]))
			// ubi_reader loops for ever on a length under 8, and Python reads
			// to the end of the file for a length under 8.
			if n < 8 {
				break
			}
			crc := ubiCRC(r.img[min(off+8, len(r.img)):min(off+n, len(r.img))])
			if crc != binary.LittleEndian.Uint32(r.img[off+4:]) {
				off += n
				continue
			}
			switch r.img[off+20] {
			case ubifsMstNode:
				if off+ubifsCommonSize+ubifsMstSize > len(r.img) {
					off = start + r.lebSize
					continue
				}
				m := r.img[off+ubifsCommonSize:]
				if cmt := binary.LittleEndian.Uint64(m[8:]); best == nil || cmt > bestCmt {
					best, bestCmt = m, cmt
				}
			case ubifsPadNode:
				if off+ubifsCommonSize+4 > len(r.img) {
					off = start + r.lebSize
					continue
				}
				off += int(binary.LittleEndian.Uint32(r.img[off+ubifsCommonSize:]))
			default:
				off = start + r.lebSize
				continue
			}
			off += n
		}
		if best != nil {
			return binary.LittleEndian.Uint32(best[24:]), binary.LittleEndian.Uint32(best[28:]), true
		}
	}
	return 0, 0, false
}

// inode returns the entry of inode number n, which it makes when needed.
func (r *ubifsReader) inode(n uint64) *ubifsInode {
	in, ok := r.inodes[n]
	if !ok {
		in = &ubifsInode{}
		r.inodes[n] = in
	}
	return in
}

// index walks the index from the node at offset offs of the logical erase
// block lnum, as _index of ubi_reader. It does not check the magic or the
// CRC of a node. A node that is cut off is a fatal error.
func (r *ubifsReader) index(lnum, offs uint32, depth int) error {
	if depth > ubifsMaxDepth {
		return errUBIFSStop
	}
	// A valid index reads each node once.
	if r.visits++; r.visits > len(r.img)/ubifsCommonSize+16 {
		return errUBIFSFatal
	}
	pos := r.lebSize*int(lnum) + int(offs)
	if pos+ubifsCommonSize > len(r.img) {
		return errUBIFSFatal
	}
	n := int(binary.LittleEndian.Uint32(r.img[pos+16:])) - ubifsCommonSize
	if n < 0 || pos+ubifsCommonSize+n > len(r.img) {
		return errUBIFSFatal
	}
	buf := r.img[pos+ubifsCommonSize : pos+ubifsCommonSize+n]
	le := binary.LittleEndian
	switch r.img[pos+20] {
	case ubifsIdxNode:
		if len(buf) < 4 {
			return errUBIFSFatal
		}
		count := int(le.Uint16(buf))
		if count == 0 {
			return errUBIFSFatal
		}
		size := (len(buf) - 4) / count
		if size < 20 {
			return errUBIFSFatal
		}
		for i := range count {
			b := buf[4+size*i:]
			if err := r.index(le.Uint32(b), le.Uint32(b[4:]), depth+1); err != nil {
				return err
			}
		}
	case ubifsInoNode:
		if len(buf) < ubifsInoSize {
			return errUBIFSStop
		}
		r.inode(uint64(le.Uint32(buf) & ubifsHashMask)).ino = &ubifsIno{
			size:  le.Uint64(buf[24:]),
			nlink: le.Uint32(buf[68:]),
			data:  buf[ubifsInoSize:],
		}
	case ubifsDataNode:
		if len(buf) < ubifsDataSize {
			return errUBIFSStop
		}
		in := r.inode(uint64(le.Uint32(buf) & ubifsHashMask))
		in.data = append(in.data, ubifsData{
			khash:   le.Uint32(buf[4:]),
			size:    le.Uint32(buf[16:]),
			compr:   le.Uint16(buf[20:]),
			payload: buf[ubifsDataSize:],
		})
	case ubifsDentNode, ubifsXentNode:
		if len(buf) < ubifsDentSize {
			return errUBIFSStop
		}
		// The name is at the end of the node, before its NUL.
		nlen := int(le.Uint16(buf[26:]))
		name := buf[max(len(buf)-nlen-1, 0) : len(buf)-1]
		if r.img[pos+20] == ubifsXentNode {
			// ubi_reader decodes the name of an extended attribute when it
			// reads the node, and uses it only to keep the permissions.
			if !utf8.Valid(name) {
				return errUBIFSStop
			}
			return nil
		}
		// ubi_reader decodes the names of the entries after the walk of the
		// index.
		if !utf8.Valid(name) {
			r.badName = true
		}
		in := r.inode(uint64(le.Uint32(buf) & ubifsHashMask))
		in.dents = append(in.dents, ubifsDent{inum: le.Uint64(buf[16:]), typ: buf[25], name: string(name)})
	}
	return nil
}

// extractUBIFS writes the files of the image, as ubireader_extract_files
// writes them. binwalk runs it on a copy of the match, and it writes the
// files in the folder "ubifs-root".
//
// ubireader_extract_files runs without the option that keeps the
// permissions, so it sets no mode: no file is executable. It runs as a user
// that cannot make a device, so it writes no device, and it writes no
// socket. It makes a FIFO, which the walker writes as binwalk writes one. It
// makes a hard link to the first file that it wrote of an inode, and the
// walker writes a copy of that file.
func extractUBIFS(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := parseUBIFS(data, off)
	if err != nil {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	img, ok := ubiRange(data, off, m.Size)
	if !ok {
		return walk.Extraction{}
	}
	r, lnum, offs, err := newUBIFS(img)
	if err != nil || !out.CreateDirectory("ubifs-root") {
		return walk.Extraction{}
	}
	if err := r.extract(out.Sub("ubifs-root"), lnum, offs); err != nil && !errors.Is(err, errUBIFSStop) {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true}
}

// ubiRange is the copy of the match that binwalk writes: it fails when the
// match is past the end of the data.
func ubiRange(data []byte, off, size int) ([]byte, bool) {
	if size < 0 || off+size > len(data) || off+size < off {
		return nil, false
	}
	return data[off : off+size], true
}

// extract is extract_files of ubi_reader.
func (r *ubifsReader) extract(out *walk.Output, lnum, offs uint32) error {
	if err := r.index(lnum, offs, 0); err != nil {
		return err
	}
	root, ok := r.inodes[1]
	if r.badName || len(r.inodes) < 2 || !ok || root.dents == nil {
		return errUBIFSStop
	}
	for _, d := range root.dents {
		if err := r.extractDent(out, "", d, 0); err != nil {
			return err
		}
	}
	return nil
}

// extractDent is extract_dents of ubi_reader: it writes one entry of a
// folder in the folder dir. The type of the entry decides what it writes,
// and an entry that fails is skipped.
func (r *ubifsReader) extractDent(out *walk.Output, dir string, d ubifsDent, depth int) error {
	if depth > ubifsMaxDepth {
		return errUBIFSStop
	}
	in, ok := r.inodes[d.inum]
	if !ok || d.name == ".." || strings.Contains(d.name, "/") {
		// ubi_reader skips an entry that leaves the folder.
		return nil
	}
	p := dir
	if d.name != "" && d.name != "." {
		p = path.Join(dir, d.name)
	}
	switch d.typ {
	case ubifsTypeDir:
		if p != "" && !out.CreateDirectory(p) {
			return nil
		}
		for _, c := range in.dents {
			if err := r.extractDent(out, p, c, depth+1); err != nil {
				return err
			}
		}
		// ubi_reader sets the time of the folder from its inode, outside
		// its check of errors.
		if in.ino == nil {
			return errUBIFSStop
		}
	case ubifsTypeReg:
		if in.ino == nil || p == dir {
			return nil
		}
		if in.ino.nlink > 1 && in.hlink == "" {
			in.hlink = p
		}
		if !out.CreateFile(p, nil) {
			return nil
		}
		return r.writeFile(out, p, in)
	case ubifsTypeLnk:
		if in.ino == nil || p == dir {
			return nil
		}
		t := in.ino.data
		if len(t) == 0 || !utf8.Valid(t) || bytes.IndexByte(t, 0) >= 0 {
			return nil
		}
		out.CreateSymlink(p, string(t))
	case ubifsTypeFIFO:
		if p != dir {
			out.CreateFIFO(p)
		}
	}
	return nil
}

// writeFile writes the data of a regular file, as process_reg_file of
// ubi_reader. The data nodes go in the order of their block number. A
// missing block is 4096 bytes of zeros. A block that does not decode ends
// the data. Zeros fill the data up to the size of the inode, and data past
// that size stays.
func (r *ubifsReader) writeFile(out *walk.Output, p string, in *ubifsInode) error {
	nodes := slices.Clone(in.data)
	slices.SortStableFunc(nodes, func(a, b ubifsData) int { return cmp.Compare(a.khash, b.khash) })
	zeros := make([]byte, ubifsBlockSize)
	written := uint64(0)
	put := func(b []byte) error {
		if len(b) > 0 && !out.AppendFile(p, b) {
			return errUBIFSFatal
		}
		written += uint64(len(b))
		return nil
	}
	last := int64(1)<<ubifsKeyBits - 1
	for _, d := range nodes {
		// ubi_reader loops for ever on a block number that does not grow.
		if int64(d.khash)-last < 1 {
			return errUBIFSFatal
		}
		for ; int64(d.khash)-last != 1; last++ {
			if err := put(zeros); err != nil {
				return err
			}
		}
		b, err := ubifsDecompress(d)
		if errors.Is(err, errCompression) {
			return errUBIFSFatal
		}
		if err != nil {
			break
		}
		if err := put(b); err != nil {
			return err
		}
		last = int64(d.khash)
	}
	big := make([]byte, 64<<10)
	for written < in.ino.size {
		if err := put(big[:min(uint64(len(big)), in.ino.size-written)]); err != nil {
			return err
		}
	}
	return nil
}

// ubifsDecompress decodes the data of a data node, as decompress of
// ubi_reader. A type that it does not know is data with no compression.
// zlib data is raw deflate. errCompression means that the walker has no
// decoder of zstd.
func ubifsDecompress(d ubifsData) ([]byte, error) {
	limit := max(int(d.size), ubifsBlockSize)
	switch d.compr {
	case 1:
		return lzo.Decode(d.payload, limit)
	case 2:
		b, err := io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(d.payload)), int64(limit)+1))
		if err != nil {
			return nil, fmt.Errorf("ubifs: %w", err)
		}
		if len(b) > limit {
			return nil, errUBIFSStop
		}
		return b, nil
	case 3:
		dec, ok := compress.LookupStream(compress.Zstd)
		if !ok {
			return nil, errCompression
		}
		w := &limitWriter{n: limit}
		if _, err := dec(d.payload, w); err != nil {
			return nil, err
		}
		return w.buf, nil
	}
	return d.payload, nil
}

// limitWriter keeps at most n bytes, and fails past them.
type limitWriter struct {
	buf []byte
	n   int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if len(w.buf)+len(p) > w.n {
		return 0, errUBIFSStop
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}
