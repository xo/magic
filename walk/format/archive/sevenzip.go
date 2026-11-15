//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/sevenzip.rs and
// src/structures/sevenzip.rs of binwalk. binwalk extracts a 7-Zip archive
// with 7zz, from src/extractors/sevenzip.rs, and extractSevenZip writes the
// files that 7zz writes, for the folders that the walker can decode.

package archive

import (
	"bytes"
	"compress/bzip2"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/xo/magic/compress"
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// SevenZip is the signature "7zip" of binwalk.
var SevenZip = &walk.Signature{
	Name:        "7zip",
	Magic:       [][]byte{[]byte("7z\xbc\xaf\x27\x1c")},
	Description: "7-zip archive data",
	Parse:       parseSevenZip,
	Extract:     extractSevenZip,
}

var sevenZipStruct = binwalk.Struct{
	{Name: "magic_p1", Type: "u16"},
	{Name: "magic_p2", Type: "u32"},
	{Name: "major_version", Type: "u8"},
	{Name: "minor_version", Type: "u8"},
	{Name: "header_crc", Type: "u32"},
	{Name: "next_header_offset", Type: "u64"},
	{Name: "next_header_size", Type: "u64"},
	{Name: "next_header_crc", Type: "u32"},
}

// The constants of structures/sevenzip.rs. The CRC of the header covers
// the bytes from 12 to 32.
const (
	sevenZipCRCStart   = 12
	sevenZipHeaderSize = 32
)

// sevenZipHeader is SevenZipHeader of binwalk.
type sevenZipHeader struct {
	major, minor int
	nextCRC      int
	nextSize     int
	nextOffset   int
}

// parseSevenZipHeader is parse_7z_header.
func parseSevenZipHeader(b []byte) (sevenZipHeader, bool) {
	h, ok := sevenZipStruct.Parse(b, false)
	if !ok {
		return sevenZipHeader{}, false
	}
	crcData, ok := binwalk.Range(b, sevenZipCRCStart, sevenZipHeaderSize)
	if !ok || binwalk.CRC32(crcData) != uint32(h["header_crc"]) {
		return sevenZipHeader{}, false
	}
	return sevenZipHeader{
		major:      h["major_version"],
		minor:      h["minor_version"],
		nextCRC:    h["next_header_crc"],
		nextSize:   h["next_header_size"],
		nextOffset: h["next_header_offset"],
	}, true
}

// parseSevenZip is sevenzip_parser: the CRC of the next header must match.
func parseSevenZip(data []byte, off int) (walk.Match, error) {
	h, ok := parseSevenZipHeader(data[off:])
	// binwalk adds the offset and the size of the next header in a usize. A
	// u64 above the largest int is negative here, and a sum past the
	// largest int wraps below 0, so binwalk.Range rejects both.
	if !ok || h.nextOffset < 0 || h.nextSize < 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off + sevenZipHeaderSize + h.nextOffset
	crcData, ok := binwalk.Range(data, start, start+h.nextSize)
	if !ok || int(binwalk.CRC32(crcData)) != h.nextCRC {
		return walk.Match{}, walk.ErrNoMatch
	}
	total := sevenZipHeaderSize + h.nextOffset + h.nextSize
	return walk.Match{
		Offset:     off,
		Size:       total,
		Confidence: walk.High,
		Description: fmt.Sprintf("7-zip archive data, version %d.%d, total size: %d bytes",
			h.major, h.minor, total),
	}, nil
}

// The property identifiers of the header of 7-Zip.
const (
	szEnd             = 0x00
	szHeaderID        = 0x01
	szArchiveProps    = 0x02
	szAdditional      = 0x03
	szMainStreams     = 0x04
	szFilesInfo       = 0x05
	szPackInfo        = 0x06
	szUnpackInfo      = 0x07
	szSubStreamsInfo  = 0x08
	szSize            = 0x09
	szCRC             = 0x0A
	szFolderID        = 0x0B
	szCodersUnpack    = 0x0C
	szNumUnpackStream = 0x0D
	szEmptyStream     = 0x0E
	szEmptyFile       = 0x0F
	szAnti            = 0x10
	szName            = 0x11
	szWinAttributes   = 0x15
	szEncodedHeader   = 0x17
)

// The identifiers of the coders that the walker decodes.
const (
	szCopy    = "\x00"
	szLZMA    = "\x03\x01\x01"
	szLZMA2   = "\x21"
	szDeflate = "\x04\x01\x08"
	szBZip2   = "\x04\x02\x02"
)

// The attributes of a file of 7-Zip. When the extension bit is set, the
// high 16 bits are the mode of Unix.
const (
	szAttrDir      = 0x10
	szAttrUnixExt  = 0x8000
	szUnixTypeMask = 0o170000
	szUnixLink     = 0o120000
	szUnixDir      = 0o040000
)

// szMaxHeader is the most bytes that the walker decodes for a header that
// is compressed.
const szMaxHeader = 64 << 20

var errSevenZip = errors.New("archive: 7-Zip header is not valid")

// szReader reads the fields of a header of 7-Zip. A read past the end sets
// bad and gives zeros.
type szReader struct {
	b   []byte
	p   int
	bad bool
}

func (r *szReader) next(n int) []byte {
	if r.bad || n < 0 || n > len(r.b)-r.p {
		r.bad = true
		return nil
	}
	b := r.b[r.p : r.p+n]
	r.p += n
	return b
}

func (r *szReader) byte() byte {
	if b := r.next(1); b != nil {
		return b[0]
	}
	return 0
}

func (r *szReader) uint32() uint32 {
	if b := r.next(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}

// number is ReadNumber of 7-Zip: the count of the high bits that are set
// in the first byte is the count of bytes that follow, in little-endian
// order, and the rest of the first byte is the high part.
func (r *szReader) number() uint64 {
	first := r.byte()
	var v uint64
	mask := byte(0x80)
	for i := range 8 {
		if first&mask == 0 {
			return v | uint64(first&(mask-1))<<(8*i)
		}
		v |= uint64(r.byte()) << (8 * i)
		mask >>= 1
	}
	return v
}

// count reads a number that counts items of at least one byte each, and
// sets bad when the header cannot hold that many.
func (r *szReader) count() int {
	n := r.number()
	if n > uint64(len(r.b)) {
		r.bad = true
		return 0
	}
	return int(n)
}

// size reads a number that is a size of data, and sets bad when it does
// not fit in an int.
func (r *szReader) size() int {
	n := r.number()
	if n > 1<<62 {
		r.bad = true
		return 0
	}
	return int(n)
}

// bits reads a vector of n bits, the high bit first.
func (r *szReader) bits(n int) []bool {
	if n > 8*len(r.b) {
		r.bad = true
		return nil
	}
	v := make([]bool, n)
	var c byte
	for i := range v {
		if i%8 == 0 {
			c = r.byte()
		}
		v[i] = c&(0x80>>(i%8)) != 0
	}
	return v
}

// defined reads a vector of n bits that starts with a byte that is not 0
// when every bit is set.
func (r *szReader) defined(n int) []bool {
	if r.byte() == 0 {
		return r.bits(n)
	}
	if n > 8*len(r.b) {
		r.bad = true
		return nil
	}
	v := make([]bool, n)
	for i := range v {
		v[i] = true
	}
	return v
}

// digests reads the CRCs of n streams, and reports which are defined.
func (r *szReader) digests(n int) []bool {
	v := r.defined(n)
	for _, ok := range v {
		if ok {
			r.uint32()
		}
	}
	return v
}

// szCoder is one coder of a folder.
type szCoder struct {
	id           string
	numIn, numOt int
	props        []byte
}

// szFolder is a folder: coders that give one stream from packed streams.
type szFolder struct {
	coders     []szCoder
	numPacked  int
	bound      []int // the output streams that a bind pair takes
	sizes      []int // the size of each output stream
	crcDefined bool
	streams    int // the files that the folder holds
}

// size is the size of the output of the folder: the size of the output
// stream that no bind pair takes.
func (f *szFolder) size() int {
	for i, s := range f.sizes {
		bound := false
		for _, b := range f.bound {
			bound = bound || b == i
		}
		if !bound {
			return s
		}
	}
	return 0
}

// szStreams is the streams info of a header.
type szStreams struct {
	packPos   int
	packSizes []int
	folders   []szFolder
	sizes     []int // the size of each file that has data, in order
}

// readFolder reads a folder.
func (r *szReader) readFolder() szFolder {
	var f szFolder
	numCoders := r.count()
	numIn, numOut := 0, 0
	for range numCoders {
		flag := r.byte()
		c := szCoder{id: string(r.next(int(flag & 0x0F))), numIn: 1, numOt: 1}
		if flag&0x10 != 0 {
			c.numIn, c.numOt = r.count(), r.count()
		}
		if flag&0x20 != 0 {
			c.props = r.next(r.count())
		}
		if flag&0x80 != 0 || r.bad {
			r.bad = true
			return f
		}
		numIn += c.numIn
		numOut += c.numOt
		f.coders = append(f.coders, c)
	}
	// Each stream of a folder takes at least one byte of the header.
	if numOut == 0 || numOut > len(r.b) || numIn > len(r.b) {
		r.bad = true
		return f
	}
	for range numOut - 1 {
		r.number()
		f.bound = append(f.bound, r.count())
	}
	f.numPacked = numIn - (numOut - 1)
	if f.numPacked > 1 {
		for range f.numPacked {
			r.number()
		}
	}
	return f
}

// readStreams reads the streams info that follows the identifier
// szMainStreams or szEncodedHeader.
func (r *szReader) readStreams() szStreams {
	var s szStreams
	id := r.byte()
	if id == szPackInfo {
		s.packPos = r.size()
		s.packSizes = make([]int, r.count())
		for id = r.byte(); id != szEnd && !r.bad; id = r.byte() {
			switch id {
			case szSize:
				for i := range s.packSizes {
					s.packSizes[i] = r.size()
				}
			case szCRC:
				r.digests(len(s.packSizes))
			default:
				r.bad = true
			}
		}
		id = r.byte()
	}
	if id == szUnpackInfo {
		if r.byte() != szFolderID || r.bad {
			r.bad = true
			return s
		}
		s.folders = make([]szFolder, r.count())
		if r.byte() != 0 {
			// The folders are in an additional stream, which 7-Zip does
			// not write.
			r.bad = true
			return s
		}
		for i := range s.folders {
			if r.bad {
				return s
			}
			s.folders[i] = r.readFolder()
			s.folders[i].streams = 1
		}
		if r.byte() != szCodersUnpack {
			r.bad = true
			return s
		}
		for i := range s.folders {
			if r.bad {
				return s
			}
			f := &s.folders[i]
			f.sizes = make([]int, 0, 1)
			for _, c := range f.coders {
				for range c.numOt {
					f.sizes = append(f.sizes, r.size())
				}
			}
		}
		for id = r.byte(); id != szEnd && !r.bad; id = r.byte() {
			if id != szCRC {
				r.bad = true
				break
			}
			for i, ok := range r.digests(len(s.folders)) {
				s.folders[i].crcDefined = ok
			}
		}
		id = r.byte()
	}
	if id == szSubStreamsInfo {
		id = r.byte()
		if id == szNumUnpackStream {
			total := 0
			for i := range s.folders {
				s.folders[i].streams = r.count()
				total += s.folders[i].streams
			}
			// A size or a CRC of each stream follows, of at least one
			// byte each.
			if total > len(r.b) {
				r.bad = true
				return s
			}
			id = r.byte()
		}
		for i := range s.folders {
			f := &s.folders[i]
			if f.streams == 0 {
				continue
			}
			left := f.size()
			for range f.streams - 1 {
				n := 0
				if id == szSize {
					n = r.size()
				}
				s.sizes = append(s.sizes, n)
				left -= n
			}
			if left < 0 {
				r.bad = true
				return s
			}
			s.sizes = append(s.sizes, left)
		}
		if id == szSize {
			id = r.byte()
		}
		for ; id != szEnd && !r.bad; id = r.byte() {
			if id != szCRC {
				r.bad = true
				break
			}
			n := 0
			for _, f := range s.folders {
				if f.streams != 1 || !f.crcDefined {
					n += f.streams
				}
			}
			r.digests(n)
		}
		id = r.byte()
	} else {
		for _, f := range s.folders {
			s.sizes = append(s.sizes, f.size())
		}
	}
	if id != szEnd {
		r.bad = true
	}
	return s
}

// szFile is one file of the header.
type szFile struct {
	name      string
	hasStream bool
	dir       bool
	anti      bool
	mode      fs.FileMode
	link      bool
}

// readFiles reads the files info.
func (r *szReader) readFiles() []szFile {
	files := make([]szFile, r.count())
	for i := range files {
		files[i].hasStream = true
	}
	var empty []int // the files that have no stream
	for {
		id := r.number()
		if id == szEnd || r.bad {
			break
		}
		size := r.count()
		p := r.p
		switch id {
		case szEmptyStream:
			empty = empty[:0]
			for i, ok := range r.bits(len(files)) {
				files[i].hasStream = !ok
				files[i].dir = ok
				if ok {
					empty = append(empty, i)
				}
			}
		case szEmptyFile:
			for i, ok := range r.bits(len(empty)) {
				files[empty[i]].dir = !ok
			}
		case szAnti:
			for i, ok := range r.bits(len(empty)) {
				files[empty[i]].anti = ok
			}
		case szName:
			if r.byte() != 0 {
				r.bad = true
				break
			}
			names := r.next(size - 1)
			for i := range files {
				var u []uint16
				for len(names) >= 2 {
					c := binary.LittleEndian.Uint16(names)
					names = names[2:]
					if c == 0 {
						break
					}
					u = append(u, c)
				}
				files[i].name = string(utf16.Decode(u))
			}
		case szWinAttributes:
			v := r.defined(len(files))
			if r.byte() != 0 {
				r.bad = true
				break
			}
			for i, ok := range v {
				if !ok {
					continue
				}
				a := r.uint32()
				if a&szAttrDir != 0 {
					files[i].dir = true
				}
				if a&szAttrUnixExt == 0 {
					continue
				}
				unix := a >> 16
				files[i].mode = fs.FileMode(unix & 0o777)
				switch unix & szUnixTypeMask {
				case szUnixLink:
					files[i].link = true
				case szUnixDir:
					files[i].dir = true
				}
			}
		}
		// Each property has its size, so the reader goes to its end,
		// whatever it read of it.
		r.p = p
		r.next(size)
	}
	return files
}

// szDecode decodes the folder i of s, from the archive arc, to w. It
// decodes the folders of one coder, of the methods copy, LZMA, LZMA2,
// deflate and bzip2. LZMA and LZMA2 need the stream decoders of LZMA and
// xz, which github.com/xo/magic/compress/xz registers.
func szDecode(arc []byte, s *szStreams, i int, w io.Writer) error {
	f := &s.folders[i]
	if len(f.coders) != 1 || f.numPacked != 1 || f.coders[0].numIn != 1 || f.coders[0].numOt != 1 {
		return errSevenZip
	}
	pack := 0
	for j := range i {
		pack += s.folders[j].numPacked
	}
	if pack >= len(s.packSizes) {
		return errSevenZip
	}
	start := sevenZipHeaderSize + s.packPos
	for _, n := range s.packSizes[:pack] {
		start += n
	}
	src, ok := binwalk.Range(arc, start, start+s.packSizes[pack])
	if !ok {
		return errSevenZip
	}
	size := f.size()
	c := f.coders[0]
	var err error
	switch c.id {
	case szCopy:
		if size > len(src) {
			return errSevenZip
		}
		_, err = w.Write(src[:size])
	case szLZMA:
		dec, ok := compress.LookupStream(compress.LZMA)
		if !ok || len(c.props) != 5 {
			return errSevenZip
		}
		// The header of a .lzma file: the properties, and the size of the
		// data.
		b := make([]byte, 0, 13+len(src))
		b = append(b, c.props...)
		b = binary.LittleEndian.AppendUint64(b, uint64(size))
		_, err = dec(append(b, src...), w)
	case szLZMA2:
		dec, ok := compress.LookupStream(compress.XZ)
		if !ok || len(c.props) != 1 {
			return errSevenZip
		}
		_, err = dec(szXZ(c.props[0], src, size), w)
	case szDeflate:
		fr := flate.NewReader(bytes.NewReader(src))
		_, err = io.Copy(w, io.LimitReader(fr, int64(size)))
		_ = fr.Close()
	case szBZip2:
		_, err = io.Copy(w, io.LimitReader(bzip2.NewReader(bytes.NewReader(src)), int64(size)))
	default:
		return errSevenZip
	}
	if err != nil {
		return fmt.Errorf("archive: decoding 7-Zip folder %d: %w", i, err)
	}
	return nil
}

// szXZ wraps the LZMA2 data of a folder in an xz stream of one block with
// no check, so that the stream decoder of xz decodes it.
func szXZ(dict byte, data []byte, size int) []byte {
	appendCRC := func(b []byte, from int) []byte {
		return binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b[from:]))
	}
	b := []byte("\xFD7zXZ\x00\x00\x00")
	b = appendCRC(b, 6)
	// The block header: its size in words less one, the flags of one
	// filter and no sizes, the filter LZMA2 with its one byte of
	// properties, padding, and its CRC.
	block := len(b)
	b = append(b, 2, 0, 0x21, 1, dict, 0, 0, 0)
	b = appendCRC(b, block)
	headerSize := len(b) - block
	b = append(b, data...)
	for len(data)%4 != 0 {
		data = append(data[:len(data):len(data)], 0)
		b = append(b, 0)
	}
	// The index: one record of the unpadded size and the size of the data.
	index := len(b)
	b = append(b, 0)
	b = binary.AppendUvarint(b, 1)
	b = binary.AppendUvarint(b, uint64(headerSize+len(data)))
	b = binary.AppendUvarint(b, uint64(size))
	for (len(b)-index)%4 != 0 {
		b = append(b, 0)
	}
	b = appendCRC(b, index)
	footer := binary.LittleEndian.AppendUint32(nil, uint32((len(b)-index)/4-1))
	footer = append(footer, 0, 0)
	b = binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(footer))
	b = append(b, footer...)
	return append(b, "YZ"...)
}

// szHeader reads the header of the archive arc, and decodes it first when
// it is compressed.
func szHeader(arc, b []byte) (szStreams, []szFile, error) {
	for range 4 {
		r := &szReader{b: b}
		switch r.byte() {
		case szHeaderID:
			var s szStreams
			var files []szFile
			id := r.byte()
			if id == szArchiveProps {
				for t := r.number(); t != szEnd && !r.bad; t = r.number() {
					r.next(r.count())
				}
				id = r.byte()
			}
			if id == szAdditional {
				return s, nil, errSevenZip
			}
			if id == szMainStreams {
				s = r.readStreams()
				id = r.byte()
			}
			if id == szFilesInfo {
				files = r.readFiles()
				id = r.byte()
			}
			if r.bad || id != szEnd {
				return s, nil, errSevenZip
			}
			return s, files, nil
		case szEncodedHeader:
			s := r.readStreams()
			if r.bad || len(s.folders) == 0 || s.folders[0].size() > szMaxHeader {
				return s, nil, errSevenZip
			}
			buf := &szBuffer{max: s.folders[0].size()}
			if err := szDecode(arc, &s, 0, buf); err != nil {
				return s, nil, err
			}
			b = buf.b
		default:
			return szStreams{}, nil, errSevenZip
		}
	}
	return szStreams{}, nil, errSevenZip
}

// szBuffer holds the data of a header, and fails past its size.
type szBuffer struct {
	b   []byte
	max int
}

// Write is the method of io.Writer.
func (b *szBuffer) Write(p []byte) (int, error) {
	if len(b.b)+len(p) > b.max {
		return 0, errSevenZip
	}
	b.b = append(b.b, p...)
	return len(p), nil
}

// szSink takes the data of a folder in order, and writes it to the files
// that the folder holds.
type szSink struct {
	w     *writer
	files []szFile
	sizes []int
	i     int    // the file that takes the next bytes
	left  int    // the bytes that the file still takes
	open  bool   // the file was created
	link  []byte // the target of a link
}

// start makes the next files, and closes those that take no bytes.
func (s *szSink) start() {
	for s.left == 0 && s.i < len(s.files) {
		f := s.files[s.i]
		s.left = s.sizes[s.i]
		s.link = s.link[:0]
		s.open = f.link || s.w.out.CreateFile(f.name, nil)
		if s.left == 0 {
			s.close()
		}
	}
}

// close writes the link or the mode of the file that took its bytes.
func (s *szSink) close() {
	f := s.files[s.i]
	s.i++
	if !s.open {
		return
	}
	if f.link {
		// 7zz makes a link only when its target has no ".." part, and
		// writes an empty file in its place otherwise. It skips a link
		// whose target is only slashes.
		target := string(s.link)
		switch t := strings.TrimLeft(target, "/"); {
		case t == "":
		case slices.Contains(strings.Split(t, "/"), ".."):
			s.w.write(member{name: f.name})
		default:
			s.w.write(member{name: f.name, mode: fs.ModeSymlink | f.mode, link: target})
		}
		return
	}
	s.w.wrote = true
	if f.mode&0o111 != 0 {
		s.w.out.MakeExecutable(f.name)
	}
}

// Write is the method of io.Writer.
func (s *szSink) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		s.start()
		if s.i >= len(s.files) {
			// The folder gives more data than its files take, so the
			// decoder stops.
			return n - len(p), errSevenZip
		}
		k := min(len(p), s.left)
		f := s.files[s.i]
		switch {
		case !s.open:
		case f.link:
			if len(s.link)+k > 4096 {
				s.open = false
			} else {
				s.link = append(s.link, p[:k]...)
			}
		case !s.w.out.AppendFile(f.name, p[:k]):
			s.w.stopped = true
			return 0, errSevenZip
		}
		p = p[k:]
		s.left -= k
		if s.left == 0 {
			s.close()
		}
	}
	return n, nil
}

// extractSevenZip writes the files of the 7-Zip archive at off, as 7zz
// writes them from the bytes that binwalk carves. A folder that the walker
// cannot decode, such as one that is encrypted or has a filter, gives no
// file. When the data of a folder fails, the file that took it keeps the
// bytes before the failure, as 7zz keeps them, and binwalk takes the exit
// code of that error.
func extractSevenZip(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := parseSevenZip(data, off)
	if err != nil {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	arc, ok := binwalk.Range(data, off, off+m.Size)
	if !ok {
		return walk.Extraction{}
	}
	// The parser holds that the next header ends the match.
	h, _ := parseSevenZipHeader(arc)
	s, files, err := szHeader(arc, arc[m.Size-h.nextSize:])
	if err != nil {
		return walk.Extraction{}
	}
	w := &writer{out: out}
	// The files that have no data come first: the folders, and the empty
	// files.
	var streams []szFile
	for i := range files {
		f := &files[i]
		f.name = clean(f.name)
		switch {
		case f.name == "" || f.anti:
		case f.hasStream:
			streams = append(streams, *f)
		case f.dir:
			w.write(member{name: f.name, mode: fs.ModeDir | 0o755})
		case f.link:
		default:
			w.write(member{name: f.name, mode: f.mode})
		}
	}
	if len(streams) != len(s.sizes) {
		return w.result()
	}
	first := 0
	for i := range s.folders {
		if w.stopped {
			break
		}
		n := s.folders[i].streams
		sink := &szSink{w: w, files: streams[first : first+n], sizes: s.sizes[first : first+n]}
		first += n
		for _, f := range sink.files {
			parent(out, f.name)
		}
		_ = szDecode(arc, &s, i, sink)
	}
	return w.result()
}
