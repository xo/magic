//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package filesystem

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/xo/magic/compress"
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/lz4"
	"github.com/xo/magic/walk/internal/lzo"
)

// This file reads SquashFS 1.x, 2.x and 3.x, in either byte order, as
// sasquatch reads them: the layouts of squashfs_compat.h, and the functions
// of unsquash-1.c, unsquash-2.c, unsquash-3.c and unsquashfs.c of
// sasquatch 4.5.1 (the patches of onekey-sec). It copies their behavior,
// not their code.
//
// The inodes, the folders and their entries are packed structures of bit
// fields. A little endian image holds them as GCC lays them out on a little
// endian machine, and a big endian image as GCC lays them out on a big
// endian one. unsquashfs swaps the second with SQUASHFS_SWAP, and field
// reads a field in either from its position in bits.

// errStop is a fatal error of sasquatch: it stops, and binwalk keeps no file.
var errStop = errors.New("squashfs: sasquatch stops on the image")

// sqfs3 reads one image of SquashFS before 4.0.
type sqfs3 struct {
	img          []byte
	big          bool
	major, minor int
	blockSize    int
	blockLog     int
	check        bool // the flag SQUASHFS_CHECK: each metadata block has a byte more
	inodes       uint32
	rootRef      uint64
	inodeTable   int64
	dirTable     int64
	noUids       int
	noGuids      int
	frags        []frag3

	detected int        // the decompressor that worked, as detected_compressor_index
	adaptive lzmaParams // the properties that lzma-adaptive found
	lzma     lzmaDec

	seen   map[uint32]bool   // the inode numbers of folders, as inumber_lookup
	links  map[uint32]*link3 // the inode numbers of the other inodes, as lookup
	visits int               // the inodes read, which stops a loop of folders

	cache      map[int64]cached // the metadata blocks, as the hash tables of get_metadata
	lastFrag   []byte
	lastFragAt frag3
}

// cached is a metadata block that block read, and where the next starts.
type cached struct {
	b    []byte
	next int64
}

// frag3 is an entry of the table of fragments.
type frag3 struct {
	start int64
	size  uint32
}

// link3 is the first inode with a number, which a hard link copies.
type link3 struct {
	in *inode3
	ok bool // sasquatch made the inode
}

// field reads the bit field of n bits at the bit pos of b, as the compiler
// of each byte order lays it out.
func (s *sqfs3) field(b []byte, pos, n int) uint64 {
	first, shift := pos/8, pos%8
	nb := (n + shift + 7) / 8
	var v uint64
	if s.big {
		for i := range nb {
			v = v<<8 | uint64(b[first+i])
		}
		v >>= nb*8 - n - shift
	} else {
		for i := nb - 1; i >= 0; i-- {
			v = v<<8 | uint64(b[first+i])
		}
		v >>= shift
	}
	if n < 64 {
		v &= 1<<n - 1
	}
	return v
}

func (s *sqfs3) u16(b []byte) uint64 { return s.field(b, 0, 16) }
func (s *sqfs3) u32(b []byte) uint64 { return s.field(b, 0, 32) }
func (s *sqfs3) u64(b []byte) uint64 { return s.field(b, 0, 64) }

// bytes is read_fs_bytes: n bytes at off, in the image.
func (s *sqfs3) bytes(off, n int64) ([]byte, bool) {
	if off < 0 || n < 0 || off > int64(len(s.img)) || n > int64(len(s.img))-off {
		return nil, false
	}
	return s.img[off : off+n], true
}

// newSqfs3 is read_super_3, read_super_2 and read_super_1, with the checks
// of the block size in main.
func newSqfs3(img []byte, big bool) (*sqfs3, error) {
	s := &sqfs3{img: img, big: big, seen: map[uint32]bool{}, links: map[uint32]*link3{}, cache: map[int64]cached{}}
	if len(img) < 119 {
		return nil, errStop
	}
	o := binary.ByteOrder(binary.LittleEndian)
	if big {
		o = binary.BigEndian
	}
	s.major, s.minor = int(o.Uint16(img[28:])), int(o.Uint16(img[30:]))
	flags := img[36]
	s.check = flags&4 != 0
	s.noUids, s.noGuids = int(img[37]), int(img[38])
	s.inodes = o.Uint32(img[4:])
	s.rootRef = o.Uint64(img[43:])
	s.blockLog = int(o.Uint16(img[34:]))
	s.blockSize = int(o.Uint32(img[51:]))
	fragCount := int64(o.Uint32(img[55:]))
	bytesUsed := int64(o.Uint64(img[63:]))
	uidStart, guidStart := int64(o.Uint64(img[71:])), int64(o.Uint64(img[79:]))
	s.inodeTable, s.dirTable = int64(o.Uint64(img[87:])), int64(o.Uint64(img[95:]))
	fragTable, lookupTable := int64(o.Uint64(img[103:])), int64(o.Uint64(img[111:]))
	switch s.major {
	case 1:
		if string(img[:4]) != map[bool]string{false: "hsqs", true: "sqsh"}[big] || s.minor != 0 {
			return nil, errStop
		}
		s.blockSize, fragCount = int(o.Uint16(img[32:])), 0
	case 2:
		if s.minor > 1 {
			return nil, errStop
		}
	}
	if s.major <= 2 {
		bytesUsed = int64(o.Uint32(img[8:]))
		uidStart, guidStart = int64(o.Uint32(img[12:])), int64(o.Uint32(img[16:]))
		s.inodeTable, s.dirTable = int64(o.Uint32(img[20:])), int64(o.Uint32(img[24:]))
		fragTable, lookupTable = int64(o.Uint32(img[59:])), -1
	}
	if s.blockSize > 1<<20 || s.blockLog > 20 || s.blockSize < 4096 || s.blockSize != 1<<s.blockLog {
		return nil, errStop
	}
	if err := s.tables(bytesUsed, uidStart, guidStart, fragTable, lookupTable, fragCount); err != nil {
		return nil, err
	}
	return s, nil
}

// tables is read_filesystem_tables: it checks where the tables are, and
// reads the table of fragments.
func (s *sqfs3) tables(bytesUsed, uidStart, guidStart, fragTable, lookupTable, fragCount int64) error {
	ids := func(n int, start, end int64) bool {
		_, ok := s.bytes(start, int64(4*n))
		return int64(4*n) <= end-start && ok
	}
	end := bytesUsed
	if s.noGuids > 0 {
		if guidStart >= bytesUsed || (s.major == 1 && s.noGuids > 15) || !ids(s.noGuids, guidStart, bytesUsed) {
			return errStop
		}
		end = guidStart
	} else if guidStart != 0 {
		return errStop
	}
	if uidStart >= end || s.noUids == 0 || (s.major == 1 && s.noUids > 48) || !ids(s.noUids, uidStart, end) {
		return errStop
	}
	end = uidStart
	if s.major == 1 {
		return s.tableOrder(end)
	}
	if s.major >= 3 && lookupTable != -1 {
		// parse_exports_table: the index of the table of inode numbers.
		n := (int64(s.inodes)*8 + sqMetadataSize - 1) / sqMetadataSize * 8
		if lookupTable >= end || n != end-lookupTable {
			return errStop
		}
		b, ok := s.bytes(lookupTable, n)
		if !ok || n == 0 {
			return errStop
		}
		end = int64(s.u64(b))
	}
	if fragCount == 0 {
		if fragTable != end {
			return errStop
		}
		return s.tableOrder(end)
	}
	if fragTable >= end || fragCount > int64(s.inodes) {
		return errStop
	}
	entry, ptr := int64(16), int64(8)
	if s.major == 2 {
		entry, ptr = 8, 4
	}
	size := fragCount * entry
	indexes := (size + sqMetadataSize - 1) / sqMetadataSize
	if indexes*ptr != end-fragTable {
		return errStop
	}
	index, ok := s.bytes(fragTable, indexes*ptr)
	if !ok {
		return errStop
	}
	table := make([]byte, 0, size)
	for i := range indexes {
		expected := sqMetadataSize
		if i+1 == indexes {
			expected = int(size & (sqMetadataSize - 1))
		}
		at := int64(s.field(index[i*ptr:], 0, int(ptr*8)))
		b, _, err := s.block(at, expected)
		if err != nil {
			return err
		}
		table = append(table, b...)
	}
	if int64(len(table)) < size {
		return errStop
	}
	for i := range fragCount {
		e := table[i*entry:]
		if s.major == 2 {
			s.frags = append(s.frags, frag3{start: int64(s.u32(e)), size: uint32(s.u32(e[4:]))})
		} else {
			s.frags = append(s.frags, frag3{start: int64(s.u64(e)), size: uint32(s.u32(e[8:]))})
		}
	}
	return s.tableOrder(int64(s.field(index, 0, int(ptr*8))))
}

// tableOrder checks that the table of folders ends before end, and that the
// table of inodes comes before it.
func (s *sqfs3) tableOrder(end int64) error {
	if s.dirTable > end || s.inodeTable >= s.dirTable {
		return errStop
	}
	return nil
}

// block is read_block: a metadata block at start, with its header of 16
// bits, which the flag of check data makes 24. expected is the size that it
// must give, or 0 for any size up to 8 KiB.
func (s *sqfs3) block(start int64, expected int) ([]byte, int64, error) {
	h, ok := s.bytes(start, 2)
	if !ok {
		return nil, 0, errStop
	}
	c := int(s.u16(h))
	offset := int64(2)
	if s.check {
		offset = 3
	}
	size := c &^ 0x8000
	if size == 0 {
		size = 0x8000
	}
	outlen := expected
	if outlen == 0 {
		outlen = sqMetadataSize
	}
	if size > outlen {
		return nil, 0, errStop
	}
	b, ok := s.bytes(start+offset, int64(size))
	if !ok {
		return nil, 0, errStop
	}
	if c&0x8000 == 0 {
		if b, ok = s.uncompress(b, outlen); !ok {
			return nil, 0, errStop
		}
	}
	if len(b) == 0 || (expected != 0 && len(b) != expected) {
		return nil, 0, errStop
	}
	return b, start + offset + int64(size), nil
}

// The decompressors of sasquatch, in the order of the table compressor of
// compressor.c. gzip is the one of every image before 4.0.
const (
	sqGzip = iota
	sqLZO
	sqLZ4
	sqXZ
	sqZstd
	sqLZMA
	sqLZMAAdaptive
	sqLZMAAlt
	sqLZMAWRT
	sqDecoders
)

// uncompress is compressor_uncompress of sasquatch. It tries the
// decompressor that worked before, then gzip, then each other in turn, and
// keeps the first that gives data.
func (s *sqfs3) uncompress(src []byte, outsize int) ([]byte, bool) {
	if s.detected != sqGzip {
		if b, ok := s.decoder(s.detected, src, outsize); ok {
			return b, true
		}
	}
	if b, ok := s.decoder(sqGzip, src, outsize); ok {
		return b, true
	}
	for i := range sqDecoders {
		if i == sqGzip || i == s.detected {
			continue
		}
		if b, ok := s.decoder(i, src, outsize); ok {
			s.detected = i
			return b, true
		}
	}
	return nil, false
}

// decoder runs the decompressor i, and reports whether it gave data.
func (s *sqfs3) decoder(i int, src []byte, outsize int) ([]byte, bool) {
	var b []byte
	var ok bool
	switch i {
	case sqGzip:
		// uncompress of zlib: one whole stream, of at most outsize bytes.
		r, err := zlib.NewReader(bytes.NewReader(src))
		if err == nil {
			b, err = io.ReadAll(io.LimitReader(r, int64(outsize)+1))
			ok = err == nil && len(b) <= outsize
		}
	case sqLZO:
		var err error
		b, err = lzo.Decode(src, outsize)
		ok = err == nil
	case sqLZ4:
		var err error
		b, err = lz4.Block(src, outsize)
		ok = err == nil
	case sqXZ, sqZstd:
		// The decoder of the library takes the whole data as one stream.
		m := compress.XZ
		if i == sqZstd {
			m = compress.Zstd
		}
		dec, found := compress.LookupStream(m)
		if !found {
			return nil, false
		}
		w := &limitWriter{n: outsize}
		n, err := dec(src, w)
		b, ok = w.buf, err == nil && n == len(src)
	case sqLZMA:
		b, ok = s.lzma.lzmaStandard(src, outsize)
	case sqLZMAAdaptive:
		b, ok = s.lzmaAdaptive(src, outsize)
	case sqLZMAAlt:
		b, ok = s.lzma.lzmaAlt(src, outsize)
	case sqLZMAWRT:
		b, ok = s.lzma.lzmaWRT(src, outsize)
	}
	if !ok || len(b) == 0 {
		return nil, false
	}
	return slices.Clone(b), true
}

// meta3 reads metadata from a position, as read_metadata reads it.
type meta3 struct {
	s    *sqfs3
	next int64  // the next block
	buf  []byte // what is left of the current block
}

// meta starts to read metadata at the block at start, offset bytes in.
func (s *sqfs3) meta(start int64, offset int) (*meta3, error) {
	m := &meta3{s: s, next: start}
	if err := m.load(); err != nil {
		return nil, err
	}
	if offset >= len(m.buf) {
		return nil, errStop
	}
	m.buf = m.buf[offset:]
	return m, nil
}

func (m *meta3) load() error {
	c, ok := m.s.cache[m.next]
	if !ok {
		b, next, err := m.s.block(m.next, 0)
		if err != nil {
			return err
		}
		c = cached{b: b, next: next}
		m.s.cache[m.next] = c
	}
	m.buf, m.next = c.b, c.next
	return nil
}

// read reads n bytes. It reads the next block when the current one is
// used up, even for no byte, as read_metadata does.
func (m *meta3) read(n int) ([]byte, error) {
	out := make([]byte, 0, n)
	for {
		if len(m.buf) == 0 {
			if err := m.load(); err != nil {
				return nil, err
			}
		}
		k := min(n-len(out), len(m.buf))
		out = append(out, m.buf[:k]...)
		m.buf = m.buf[k:]
		if len(out) == n {
			return out, nil
		}
	}
}

// inode3 is the struct inode that read_inode fills.
type inode3 struct {
	typ       int
	mode      int
	number    uint32
	data      int64 // the size of a file, a folder or a link
	start     int64
	offset    uint32
	fragment  uint32
	fragBytes int64
	blocks    int64
	list      *meta3 // the list of blocks of a file
	symlink   string
}

// The types of inode. sqLDir and sqLReg are the folder and the file with
// more fields, of 3.x.
const (
	sqLDir = 8
	sqLReg = 9
)

// sqInvalidFrag is SQUASHFS_INVALID_FRAG.
const sqInvalidFrag = 0xFFFFFFFF

// inode is read_inode of the version of the image.
func (s *sqfs3) inode(block, offset uint32) (*inode3, error) {
	if s.visits++; s.visits > len(s.img)+64 {
		return nil, errStop
	}
	m, err := s.meta(s.inodeTable+int64(block), int(offset))
	if err != nil {
		return nil, err
	}
	switch s.major {
	case 1:
		return s.inode1(m)
	case 2:
		return s.inode2(m)
	}
	return s.inode3(m)
}

// fields reads the rest of a structure of n bytes whose first k bytes are
// in b.
func (m *meta3) fields(b []byte, n int) ([]byte, error) {
	rest, err := m.read(n - len(b))
	if err != nil {
		return nil, err
	}
	return append(slices.Clone(b), rest...), nil
}

// regular fills the fields of a file of size bytes.
func (s *sqfs3) regular(in *inode3, m *meta3, size int64, fragment uint32) {
	in.data, in.fragment, in.list = size, fragment, m
	if fragment == sqInvalidFrag {
		in.blocks = (size + int64(s.blockSize) - 1) >> s.blockLog
	} else {
		in.fragBytes = size % int64(s.blockSize)
		in.blocks = size >> s.blockLog
	}
}

// symlink reads the target of a link of n bytes.
func (in *inode3) readSymlink(m *meta3, n int) error {
	b, err := m.read(n)
	if err != nil {
		return err
	}
	in.symlink, in.data = string(b), int64(n)
	return nil
}

// The sizes of the structures of the inodes of 3.x and 2.x, by type. A FIFO
// and a socket have none that read_inode reads.
var (
	inodeSize3 = map[int]int{sqDir: 28, sqLDir: 31, sqFile: 32, sqLReg: 40, sqSymlink: 18, sqBlock: 18, sqChar: 18}
	inodeSize2 = map[int]int{sqDir: 15, sqLDir: 18, sqFile: 24, sqSymlink: 6, sqBlock: 6, sqChar: 6}
)

// inode3 is read_inode of unsquash-3.c.
func (s *sqfs3) inode3(m *meta3) (*inode3, error) {
	b, err := m.read(12)
	if err != nil {
		return nil, err
	}
	in := &inode3{typ: int(s.field(b, 0, 4)), mode: int(s.field(b, 4, 12)), number: uint32(s.field(b, 64, 32))}
	uid, guid := int(s.field(b, 16, 8)), int(s.field(b, 24, 8))
	if uid >= s.noUids || (guid != 255 && guid >= s.noGuids) || in.typ < 1 || in.typ > 9 ||
		in.number > s.inodes || in.number == 0 {
		return nil, errStop
	}
	size := inodeSize3[in.typ]
	if size == 0 {
		return in, nil // a FIFO or a socket
	}
	if b, err = m.fields(b, size); err != nil {
		return nil, err
	}
	switch in.typ {
	case sqDir:
		in.data, in.offset, in.start = int64(s.field(b, 128, 19)), uint32(s.field(b, 147, 13)), int64(s.field(b, 160, 32))
	case sqLDir:
		in.data, in.offset, in.start = int64(s.field(b, 128, 27)), uint32(s.field(b, 155, 13)), int64(s.field(b, 168, 32))
	case sqFile:
		in.start, in.offset = int64(s.field(b, 96, 64)), uint32(s.field(b, 192, 32))
		s.regular(in, m, int64(s.field(b, 224, 32)), uint32(s.field(b, 160, 32)))
	case sqLReg:
		in.start, in.offset = int64(s.field(b, 128, 64)), uint32(s.field(b, 224, 32))
		s.regular(in, m, int64(s.field(b, 256, 64)), uint32(s.field(b, 192, 32)))
	case sqSymlink:
		err = in.readSymlink(m, int(s.field(b, 128, 16)))
	}
	return in, err
}

// inode2 is read_inode of unsquash-2.c. An inode of 2.x has no number, and
// unsquashfs counts them.
func (s *sqfs3) inode2(m *meta3) (*inode3, error) {
	b, err := m.read(4)
	if err != nil {
		return nil, err
	}
	in := &inode3{typ: int(s.field(b, 0, 4)), mode: int(s.field(b, 4, 12))}
	uid, guid := int(s.field(b, 16, 8)), int(s.field(b, 24, 8))
	if uid >= s.noUids || (guid != 255 && guid >= s.noGuids) || in.typ < 1 || in.typ > 8 {
		return nil, errStop
	}
	size := inodeSize2[in.typ]
	if size == 0 {
		return in, nil
	}
	if b, err = m.fields(b, size); err != nil {
		return nil, err
	}
	switch in.typ {
	case sqDir:
		in.data, in.offset, in.start = int64(s.field(b, 32, 19)), uint32(s.field(b, 51, 13)), int64(s.field(b, 96, 24))
	case sqLDir:
		in.data, in.offset, in.start = int64(s.field(b, 32, 27)), uint32(s.field(b, 59, 13)), int64(s.field(b, 104, 24))
	case sqFile:
		in.start, in.offset = int64(s.field(b, 64, 32)), uint32(s.field(b, 128, 32))
		s.regular(in, m, int64(s.field(b, 160, 32)), uint32(s.field(b, 96, 32)))
	case sqSymlink:
		err = in.readSymlink(m, int(s.field(b, 32, 16)))
	}
	return in, err
}

// inode1 is read_inode of unsquash-1.c. The type of a 1.x inode also holds
// the high bits of the index of its uid, and a FIFO or a socket is one type
// of IPC with its own field of type.
func (s *sqfs3) inode1(m *meta3) (*inode3, error) {
	b, err := m.read(3)
	if err != nil {
		return nil, err
	}
	t := int(s.field(b, 0, 4))
	in := &inode3{mode: int(s.field(b, 4, 12))}
	low := int(s.field(b, 16, 4))
	// (t - 1) / 5 is 0 for the type 0 in C, which rounds toward zero.
	if (t-1)/5*16+low >= s.noUids {
		return nil, errStop
	}
	if t == 0 {
		if b, err = m.fields(b, 4); err != nil {
			return nil, err
		}
		in.typ = sqFIFO
		if s.field(b, 24, 4) == sqSocket {
			in.typ = sqSocket
		}
		if int(s.field(b, 28, 4))*16+low >= s.noUids {
			return nil, errStop
		}
	} else {
		in.typ = (t-1)%5 + 1
	}
	if guid := int(s.field(b, 20, 4)); guid != 15 && guid >= s.noGuids {
		return nil, errStop
	}
	switch in.typ {
	case sqDir:
		if b, err = m.fields(b, 14); err != nil {
			return nil, err
		}
		in.data, in.offset, in.start = int64(s.field(b, 24, 19)), uint32(s.field(b, 43, 13)), int64(s.field(b, 88, 24))
	case sqFile:
		if b, err = m.fields(b, 15); err != nil {
			return nil, err
		}
		in.start, in.data, in.list = int64(s.field(b, 56, 32)), int64(s.field(b, 88, 32)), m
		in.blocks = (in.data + int64(s.blockSize) - 1) >> s.blockLog
	case sqSymlink, sqBlock, sqChar:
		if b, err = m.fields(b, 5); err != nil {
			return nil, err
		}
		if in.typ == sqSymlink {
			err = in.readSymlink(m, int(s.field(b, 24, 16)))
		}
	}
	return in, err
}

// entry3 is an entry of a folder.
type entry3 struct {
	name          string
	block, offset uint32
	typ           int
}

// dir is squashfs_opendir of the version of the image: the entries of the
// folder in.
func (s *sqfs3) dir(in *inode3) ([]entry3, error) {
	size := in.data
	if s.major >= 3 {
		size -= 3
	}
	if size <= 0 {
		return nil, nil
	}
	m, err := s.meta(s.dirTable+in.start, int(in.offset))
	if err != nil {
		return nil, err
	}
	// The header of 3.x has a start of 32 bits, and an inode number. Its
	// entry has the difference of the inode number.
	hsize, esize, startBits := 4, 3, 24
	if s.major >= 3 {
		hsize, esize, startBits = 9, 5, 32
	}
	var out []entry3
	for read := int64(0); read < size; {
		h, err := m.read(hsize)
		if err != nil {
			return nil, err
		}
		read += int64(hsize)
		count := int(s.field(h, 0, 8)) + 1
		start := uint32(s.field(h, 8, startBits))
		for range count {
			e, err := m.read(esize)
			if err != nil {
				return nil, err
			}
			n := int(s.field(e, 16, 8)) + 1
			name, err := m.read(n)
			if err != nil {
				return nil, err
			}
			read += int64(esize + n)
			// check_name: no "/" and no NUL in a name, and no "." or "..".
			if string(name) == "." || string(name) == ".." || bytes.ContainsAny(name, "/\x00") {
				return nil, errStop
			}
			out = append(out, entry3{name: string(name), block: start, offset: uint32(s.field(e, 0, 13)), typ: int(s.field(e, 13, 3))})
		}
	}
	// An image of 1.x, or of 2.0, may hold its entries in any order. Every
	// image must hold no name twice.
	if s.major == 1 || (s.major == 2 && s.minor == 0) {
		slices.SortStableFunc(out, func(a, b entry3) int { return strings.Compare(a.name, b.name) })
	}
	for i := 1; i < len(out); i++ {
		if out[i-1].name >= out[i].name {
			return nil, errStop
		}
	}
	return out, nil
}

// extractSquashFS3 writes the files of an image before 4.0, as sasquatch
// writes them. A fatal error of sasquatch makes the extraction fail, as
// binwalk then keeps no file.
func extractSquashFS3(img []byte, big bool, out *walk.Output) walk.Extraction {
	s, err := newSqfs3(img, big)
	if err != nil {
		return walk.Extraction{}
	}
	block, offset := uint32(s.rootRef>>16), uint32(s.rootRef&0xFFFF)
	// pre_scan reads the folders and the files first, as binwalk runs
	// sasquatch without -q. It decompresses the metadata before any data.
	if err := s.preScan(block, offset, map[uint32]bool{}, 1); err != nil {
		return walk.Extraction{}
	}
	// The root of the output is squashfs-root, which scan makes once it
	// reads the root folder, and links stay inside it.
	if err := s.scan(out.Sub("squashfs-root"), "", block, offset, 1); err != nil {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true}
}

// errSkip is an error that sasquatch reports with EXIT_UNSQUASH_IGNORE: it
// stops, or it skips the file, as skip says.
var errSkip = errors.New("squashfs: sasquatch skips a file of the image")

// skip is EXIT_UNSQUASH_IGNORE. sasquatch stops unless -ignore-errors is
// set, but compressor_uncompress sets ignore_errors while it runs the
// decompressor that it detected, in another thread than the one that
// reports the error. Once that decompressor is not gzip, it runs for most of
// the time, and sasquatch skips the file. The walker skips it then too.
func (s *sqfs3) skip() error {
	if s.detected != sqGzip {
		return nil
	}
	return errStop
}

// preScan is pre_scan: it reads each folder and the inode of each file. A
// folder that it cannot read it leaves to scan.
func (s *sqfs3) preScan(block, offset uint32, seen map[uint32]bool, depth int) error {
	if depth > 1024 {
		return errStop
	}
	in, err := s.inode(block, offset)
	if err != nil {
		return err
	}
	entries, derr := s.dir(in)
	if derr != nil {
		return nil //nolint:nilerr // pre_scan goes on, and dir_scan reports the folder
	}
	if s.major >= 3 {
		if seen[in.number] {
			return nil
		}
		seen[in.number] = true
	}
	for _, e := range entries {
		switch e.typ {
		case sqDir:
			err = s.preScan(e.block, e.offset, seen, depth+1)
		case sqFile:
			_, err = s.inode(e.block, e.offset)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// scan is dir_scan: it makes the folder at the inode, and writes its
// entries. A folder whose inode number came before is a loop, which
// sasquatch skips.
func (s *sqfs3) scan(out *walk.Output, p string, block, offset uint32, depth int) error {
	if depth > 1024 {
		return errStop
	}
	in, err := s.inode(block, offset)
	if err != nil {
		return err
	}
	entries, err := s.dir(in)
	if err != nil {
		return s.skip()
	}
	if s.major >= 3 {
		if s.seen[in.number] {
			return nil
		}
		s.seen[in.number] = true
	}
	if !out.CreateDirectory(p) {
		return errStop
	}
	for _, e := range entries {
		name := path.Join(p, e.name)
		if e.typ == sqDir {
			if err := s.scan(out, name, e.block, e.offset, depth+1); err != nil {
				return err
			}
			continue
		}
		in, err := s.inode(e.block, e.offset)
		if err != nil {
			return err
		}
		if err := s.create(out, name, in); err != nil {
			return err
		}
	}
	return nil
}

// create is create_inode. Two inodes of 3.x with one number are a hard
// link, and the second is a copy of the first. sasquatch runs as a user,
// and makes no device. The walker makes no FIFO or socket, which the gate
// skips.
func (s *sqfs3) create(out *walk.Output, name string, in *inode3) error {
	if s.major >= 3 {
		if l := s.links[in.number]; l != nil {
			if !l.ok {
				return s.skip() // link fails: the first is missing
			}
			in = l.in
		}
	}
	ok := true
	switch in.typ {
	case sqFile, sqLReg:
		// The writer removes a file whose data it cannot read, so the
		// walker reads all of it first.
		chunks, err := s.data(in)
		switch {
		case errors.Is(err, errSkip):
			ok = false
			if err := s.skip(); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if !out.CreateFile(name, nil) {
				return errStop
			}
			for _, c := range chunks {
				if c.b == nil {
					c.b = make([]byte, c.zero)
				}
				if len(c.b) > 0 && !out.AppendFile(name, c.b) {
					return errStop
				}
			}
			if in.mode&0o001 != 0 && !out.MakeExecutable(name) {
				return errStop
			}
		}
	case sqSymlink:
		if !out.CreateSymlink(name, in.symlink) {
			return errStop
		}
	case sqBlock, sqChar:
		ok = false
	case sqFIFO, sqSocket:
	default:
		return nil // a folder that an entry names as a file: sasquatch skips it
	}
	if s.major >= 3 && s.links[in.number] == nil {
		s.links[in.number] = &link3{in: in, ok: ok}
	}
	return nil
}

// chunk is a part of a file: its bytes, or a hole of zero bytes.
type chunk struct {
	b    []byte
	zero int64
}

// data is write_file and the writer: the blocks of a file, and its
// fragment. A block of size 0 is a hole of zeros. An error of the list of
// blocks stops sasquatch, and an error of the data is errSkip.
func (s *sqfs3) data(in *inode3) ([]chunk, error) {
	if in.blocks < 0 || in.data < 0 {
		return nil, errStop
	}
	// The list of blocks: shorts in 1.x, with the bit 15 for a block that is
	// not compressed, and ints after.
	list := *in.list
	width := 4
	if s.major == 1 {
		width = 2
	}
	sizes := make([]uint32, 0, min(in.blocks, 1<<16))
	for range in.blocks {
		b, err := list.read(width)
		if err != nil {
			return nil, errStop
		}
		if width == 4 {
			sizes = append(sizes, uint32(s.u32(b)))
			continue
		}
		v := uint32(s.u16(b))
		size := v &^ 0x8000
		if size == 0 {
			size = 0x8000
		}
		if v&0x8000 != 0 {
			size |= 1 << 24
		}
		sizes = append(sizes, size)
	}
	var out []chunk
	end := in.data / int64(s.blockSize)
	pos := in.start
	for i, size := range sizes {
		n := int64(s.blockSize)
		if int64(i) == end {
			n = in.data & int64(s.blockSize-1)
		}
		if size == 0 {
			out = append(out, chunk{zero: n})
			continue
		}
		d, err := s.dataBlock(pos, size)
		if err != nil {
			return nil, err
		}
		pos += int64(size &^ (1 << 24))
		if int64(len(d)) < n {
			return nil, errSkip
		}
		out = append(out, chunk{b: d[:n]})
	}
	if in.fragBytes == 0 {
		return out, nil
	}
	if int64(in.fragment) >= int64(len(s.frags)) {
		return nil, errSkip
	}
	f := s.frags[in.fragment]
	d, err := s.fragment(f)
	if err != nil {
		return nil, err
	}
	if int64(in.offset)+in.fragBytes > int64(len(d)) {
		return nil, errSkip
	}
	return append(out, chunk{b: d[in.offset : int64(in.offset)+in.fragBytes]}), nil
}

// fragment reads the block of a fragment. It keeps the last one, as files
// that follow each other share it, and the cache of fragments of sasquatch
// decompresses it once.
func (s *sqfs3) fragment(f frag3) ([]byte, error) {
	if s.lastFrag != nil && s.lastFragAt == f {
		return s.lastFrag, nil
	}
	d, err := s.dataBlock(f.start, f.size)
	if err != nil {
		return nil, err
	}
	s.lastFrag, s.lastFragAt = d, f
	return d, nil
}

// dataBlock reads a block of data at pos whose size field is size: the bit
// 24 is set when the block is not compressed.
func (s *sqfs3) dataBlock(pos int64, size uint32) ([]byte, error) {
	n := int64(size &^ (1 << 24))
	if n > int64(s.blockSize) {
		return nil, errSkip
	}
	b, ok := s.bytes(pos, n)
	if !ok {
		return nil, errSkip
	}
	if size&(1<<24) != 0 {
		return b, nil
	}
	if b, ok = s.uncompress(b, s.blockSize); !ok {
		return nil, errSkip
	}
	return b, nil
}
