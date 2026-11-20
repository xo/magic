//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/cab.rs of binwalk-ng. binwalk-ng
// extracts a cabinet with cabextract, and extractCAB writes the files that
// cabextract writes, for the folders that are stored or compressed with
// MSZIP or LZX. It decodes LZX with github.com/xo/magic/compress/lzx (D68),
// as one stream over the blocks of a folder, as cabextract reads it.

package archive

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/xo/magic/compress/lzx"
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// CAB is the signature "cab" of binwalk-ng. The magic holds the field
// reserved1 after "MSCF", which must be 0.
var CAB = &walk.Signature{
	Name:        "cab",
	Magic:       [][]byte{[]byte("MSCF\x00\x00\x00\x00")},
	Description: "Microsoft Cabinet archive",
	Parse:       parseCAB,
	Extract:     extractCAB,
	Carve:       true,
}

var cabStruct = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "reserved1", Type: "u32"},
	{Name: "size", Type: "u32"},
	{Name: "reserved2", Type: "u32"},
	{Name: "first_file_offset", Type: "u32"},
	{Name: "reserved3", Type: "u32"},
	{Name: "minor_version", Type: "u8"},
	{Name: "major_version", Type: "u8"},
	{Name: "folder_count", Type: "u16"},
	{Name: "file_count", Type: "u16"},
	{Name: "flags", Type: "u16"},
	{Name: "id", Type: "u16"},
	{Name: "set_number", Type: "u16"},
	{Name: "extra_header_size", Type: "u16"},
	{Name: "cbCFFolder", Type: "u8"},
	{Name: "cbCFData", Type: "u8"},
}

var cabExtraStruct = binwalk.Struct{
	{Name: "unknown1", Type: "u32"},
	{Name: "data_offset", Type: "u32"},
	{Name: "data_size", Type: "u32"},
	{Name: "unknown2", Type: "u32"},
	{Name: "unknown3", Type: "u32"},
}

// The constants of structures/cab.rs.
const (
	cabStructSize      = 40
	cabExtraStructSize = 20
	cabFlagExtraData   = 4
)

// parseCAB is cab_parser, with parse_cab_header.
func parseCAB(data []byte, off int) (walk.Match, error) {
	b := data[off:]
	h, ok := cabStruct.Parse(b, false)
	if !ok || h["reserved1"] != 0 || h["reserved2"] != 0 || h["reserved3"] != 0 ||
		h["major_version"] != 1 || h["minor_version"] != 3 {
		return walk.Match{}, walk.ErrNoMatch
	}
	total := h["size"]
	// When the flag of extra data is set, and the extra header has the
	// expected size, the extra data follows the cabinet, and counts in its
	// size.
	if h["flags"]&cabFlagExtraData != 0 && h["extra_header_size"] == cabExtraStructSize {
		eb, ok := binwalk.Range(b, cabStructSize, cabStructSize+cabExtraStructSize)
		if !ok {
			return walk.Match{}, walk.ErrNoMatch
		}
		e, ok := cabExtraStruct.Parse(eb, false)
		if !ok || e["data_offset"] != h["size"] {
			return walk.Match{}, walk.ErrNoMatch
		}
		total += e["data_size"]
	}
	if total > len(b) {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       total,
		Confidence: walk.Medium,
		Description: fmt.Sprintf("Microsoft Cabinet archive, file count: %d, folder count: %d, header size: %d, total size: %d bytes",
			h["file_count"], h["folder_count"], cabStructSize, total),
	}, nil
}

// The methods of compression of a folder, and the flags of a cabinet.
const (
	cabMethodNone  = 0
	cabMethodMSZIP = 1
	cabMethodLZX   = 3

	cabFlagPrev    = 1
	cabFlagNext    = 2
	cabFlagReserve = 4

	cabFrameSize = 32768

	// cabInputMax is the most bytes that cabextract takes in a block of
	// data, 32768 bytes and 6144 more. cabReadSize is the number of bytes
	// that it reads at a time from the data of a folder of LZX. Tests of
	// cabextract 1.11 showed both.
	cabInputMax = 38912
	cabReadSize = 4096
)

// cabFolder is a CFFOLDER entry.
type cabFolder struct {
	start  int // the offset of its first CFDATA block
	blocks int
	method int
	window int // the bits of the window of LZX
}

// cabFile is a CFFILE entry.
type cabFile struct {
	name   string
	size   int
	offset int // the offset of its data in the data of the folder
	folder int
}

// cabReader reads the fields of a cabinet. A read past the end sets bad
// and gives zeros.
type cabReader struct {
	b   []byte
	p   int
	bad bool
}

func (r *cabReader) next(n int) []byte {
	if r.bad || n < 0 || r.p+n > len(r.b) {
		r.bad = true
		return make([]byte, n&0xffff)
	}
	b := r.b[r.p : r.p+n]
	r.p += n
	return b
}

func (r *cabReader) u8() int  { return int(r.next(1)[0]) }
func (r *cabReader) u16() int { return int(binary.LittleEndian.Uint16(r.next(2))) }
func (r *cabReader) u32() int { return int(binary.LittleEndian.Uint32(r.next(4))) }

// cstring reads a string that ends with a NUL.
func (r *cabReader) cstring() []byte {
	if r.bad || r.p > len(r.b) {
		r.bad = true
		return nil
	}
	i := bytes.IndexByte(r.b[r.p:], 0)
	if i < 0 {
		r.bad = true
		return nil
	}
	s := r.b[r.p : r.p+i]
	r.p += i + 1
	return s
}

// cabName is the name that cabextract writes for a file: the bytes of the
// name, with "/" in place of "\". cabextract does not change a name that
// is not UTF-8.
func cabName(b []byte) string {
	return strings.ReplaceAll(string(b), "\\", "/")
}

// cabHeader reads the folders and the files of the cabinet b, as libmspack
// reads them for cabextract.
func cabHeader(b []byte) (folders []cabFolder, files []cabFile, dataReserve int, ok bool) {
	r := &cabReader{b: b, p: 8}
	size := r.u32()
	r.next(4) // reserved2
	filesOffset := r.u32()
	r.next(6) // reserved3, and the version
	numFolders, numFiles, flags := r.u16(), r.u16(), r.u16()
	r.next(4) // the identifier of the set, and the number in the set
	folderReserve := 0
	if flags&cabFlagReserve != 0 {
		headerReserve := r.u16()
		folderReserve, dataReserve = r.u8(), r.u8()
		r.next(headerReserve)
	}
	if flags&cabFlagPrev != 0 {
		r.cstring()
		r.cstring()
	}
	if flags&cabFlagNext != 0 {
		r.cstring()
		r.cstring()
	}
	if r.bad || numFolders*8 > len(b) || numFiles*16 > len(b) {
		return nil, nil, 0, false
	}
	for range numFolders {
		f := cabFolder{start: r.u32(), blocks: r.u16()}
		t := r.u16()
		f.method, f.window = t&0x0F, t>>8&0x1F
		r.next(folderReserve)
		folders = append(folders, f)
	}
	// cabextract reads the files that follow the folders, whatever the
	// offset of the files in the header, and fails when that offset is not
	// inside the cabinet. A test of 18 offsets with cabextract 1.11 showed
	// this.
	if filesOffset >= size {
		return nil, nil, 0, false
	}
	for range numFiles {
		f := cabFile{size: r.u32(), offset: r.u32(), folder: r.u16()}
		r.next(6) // the date, the time and the attributes
		f.name = cabName(r.cstring())
		files = append(files, f)
	}
	if r.bad {
		return nil, nil, 0, false
	}
	return folders, files, dataReserve, true
}

// cabChecksum is cabd_checksum of libmspack: the XOR of the words of data,
// and of the bytes that are left.
func cabChecksum(data []byte, sum uint32) uint32 {
	n := len(data) &^ 3
	for i := 0; i < n; i += 4 {
		sum ^= binary.LittleEndian.Uint32(data[i:])
	}
	var ul uint32
	for _, c := range data[n:] {
		ul = ul<<8 | uint32(c)
	}
	return sum ^ ul
}

// cabSink takes the data of a folder in order, and adds each part of it to
// the files whose data it holds. The files are in the order of their
// offsets, and active holds those that the data has reached and not passed,
// so that each part of the data visits only the files that take bytes of it.
type cabSink struct {
	w      *writer
	files  []cabFile
	next   int // the first file that the data has not reached
	active []cabFile
	pos    int // the offset in the data of the folder
}

// add adds the data at the offset pos of the folder to the files.
func (s *cabSink) add(b []byte) bool {
	end := s.pos + len(b)
	for s.next < len(s.files) && s.files[s.next].offset < end {
		s.active = append(s.active, s.files[s.next])
		s.next++
	}
	keep := s.active[:0]
	for _, f := range s.active {
		start, stop := max(f.offset, s.pos), min(f.offset+f.size, end)
		if start < stop && !s.w.out.AppendFile(f.name, b[start-s.pos:stop-s.pos]) {
			s.w.stopped = true
			return false
		}
		if f.offset+f.size > end {
			keep = append(keep, f)
		}
	}
	s.active = keep
	s.pos = end
	return true
}

// cabBlock reads the next CFDATA block of a folder: its data and the size
// of the data after decoding. It fails when the block passes the end of the
// cabinet, or has a bad checksum. A checksum of 0 is not checked.
func cabBlock(r *cabReader, reserve int) (block []byte, usize int, ok bool) {
	sum := uint32(r.u32())
	head := r.next(4)
	size, usize := int(binary.LittleEndian.Uint16(head)), int(binary.LittleEndian.Uint16(head[2:]))
	r.next(reserve)
	block = r.next(size)
	if r.bad || sum != 0 && cabChecksum(head, cabChecksum(block, 0)) != sum {
		return nil, 0, false
	}
	return block, usize, true
}

// cabDecode decodes the blocks of folder f of the cabinet b up to the end
// of the last of its files, and gives the data to s. It fails as
// libmspack fails: on a bad checksum, a block that does not decode, or
// data that ends before the files end.
func cabDecode(b []byte, f cabFolder, reserve, end int, s *cabSink) bool {
	if f.method == cabMethodLZX {
		return cabLZX(b, f, reserve, end, s)
	}
	r := &cabReader{b: b, p: f.start}
	var window []byte
	for range f.blocks {
		if s.pos >= end {
			return true
		}
		block, usize, ok := cabBlock(r, reserve)
		if !ok {
			return false
		}
		var out []byte
		switch f.method {
		case cabMethodNone:
			if len(block) != usize {
				return false
			}
			out = block
		case cabMethodMSZIP:
			if usize > cabFrameSize || !bytes.HasPrefix(block, []byte("CK")) {
				return false
			}
			// libmspack keeps its window of 32 KiB from one block to the
			// next, and starts each block at the start of the window. So
			// the history of a block is the window, as the last block
			// left it.
			fr := flate.NewReaderDict(bytes.NewReader(block[2:]), window)
			out = make([]byte, usize)
			_, err := io.ReadFull(fr, out)
			_ = fr.Close()
			if err != nil {
				return false
			}
			if window == nil {
				window = make([]byte, cabFrameSize)
			}
			copy(window, out)
		default:
			return false
		}
		if !s.add(out) {
			return false
		}
	}
	return s.pos >= end
}

// cabLZX decodes folder f of LZX as cabextract 1.11 decodes it. Tests of
// cabinets made for the purpose showed these facts:
//
//   - The data of the blocks of the folder is one stream of frames of 32768
//     bytes. A frame can start in one block and end in another.
//   - A frame ends at the end of a word, and the next frame starts at the next
//     word, or at the next byte after an uncompressed block. lzx.Decoder.Used
//     gives that offset.
//   - A frame whose last match passes its end is an error. lzx.Decoder.Overrun
//     tells this.
//   - The sizes of the blocks after decoding count only in their sum, which
//     is the size of the stream after decoding. A block of 0 or more than
//     32768 bytes after decoding is an error, and so is a block of more than
//     cabInputMax bytes.
//   - cabextract reads the stream 4096 bytes at a time, over the ends of the
//     blocks, and fails when it reads a bad block, whether or not a frame
//     needs its data. At the end of a frame, it has read 2 bytes past the
//     frame, or 0 when the frame ends in an uncompressed block.
//     lzx.Decoder.Uncompressed tells this.
//   - After the last block, cabextract reads 2 bytes of 0, and fails when
//     it reads more.
//   - The sum of the sizes is known when cabextract has read the last block.
//     It sets the size of each frame from then on: 32768 bytes, or the bytes
//     that are left in the stream. The size of a frame is set at its start,
//     or after the first read for the first frame. Until then, each frame
//     has 32768 bytes.
func cabLZX(b []byte, f cabFolder, reserve, end int, s *cabSink) bool {
	// The window of LZX in a cabinet has 2^15 to 2^21 bytes.
	dec, err := lzx.NewDecoder(1 << f.window)
	if err != nil {
		return false
	}
	// stream holds the data of the blocks up to the first bad block, which
	// starts at the offset bad of the stream, or at -1 when no block is bad.
	// last is the offset of the last block, or -1 when it is bad.
	r := &cabReader{b: b, p: f.start}
	var stream []byte
	total, bad, last := 0, -1, -1
	for i := range f.blocks {
		block, usize, ok := cabBlock(r, reserve)
		if !ok || usize == 0 || usize > cabFrameSize || len(block) > cabInputMax {
			bad = len(stream)
			break
		}
		if i == f.blocks-1 {
			last = len(stream)
		}
		stream = append(stream, block...)
		total += usize
	}
	if bad < 0 {
		stream = append(stream, 0, 0)
	}
	// p is the start of the next frame in stream, and n is the end of the
	// bytes that cabextract has read. Before the first frame, it reads the
	// first 4096 bytes.
	var out []byte
	p, n := 0, cabReadSize
	for s.pos < end {
		if bad >= 0 && n > bad {
			return false
		}
		size := lzx.FrameSize
		if last >= 0 && last < n {
			size = min(size, total-s.pos)
		}
		if size <= 0 {
			return false
		}
		if out, err = dec.Decode(out[:0], stream[p:], size); err != nil || dec.Overrun() > 0 || !s.add(out) {
			return false
		}
		p += dec.Used()
		i := p
		if !dec.Uncompressed() {
			i += 2
		}
		if i > len(stream) {
			return false
		}
		n = max(n, (i+cabReadSize-1)/cabReadSize*cabReadSize)
	}
	return bad < 0 || n <= bad
}

// extractCAB writes the files of the cabinet at off, as cabextract writes
// them from the bytes that binwalk carves. cabextract reads the folders
// that are compressed with Quantum too, but the walker has no decoder of
// Quantum, so a cabinet that has such a folder fails. A cabinet that
// continues in another one fails too, as it does in cabextract.
func extractCAB(data []byte, off int, out *walk.Output) walk.Extraction {
	if _, err := parseCAB(data, off); err != nil {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	// binwalk gives cabextract the bytes of the match, and when that fails,
	// all the bytes to the end of the input. cabextract reads past the size
	// that the header gives, so the walker reads to the end of data too.
	b := data[off:]
	folders, files, reserve, ok := cabHeader(b)
	if !ok {
		return walk.Extraction{}
	}
	byFolder := make([][]cabFile, len(folders))
	for _, f := range files {
		if f.folder >= len(folders) {
			return walk.Extraction{}
		}
		if m := folders[f.folder].method; m != cabMethodNone && m != cabMethodMSZIP && m != cabMethodLZX {
			return walk.Extraction{}
		}
		byFolder[f.folder] = append(byFolder[f.folder], f)
	}
	w := &writer{out: out}
	for i, folder := range folders {
		s := &cabSink{w: w}
		end := 0
		for _, f := range byFolder[i] {
			// A file that cannot be written, such as a second file of one
			// name, is skipped, as the writer of the other archives skips
			// it.
			f.name = clean(f.name)
			if f.name == "" || !parent(out, f.name) || !out.CreateFile(f.name, nil) {
				continue
			}
			w.wrote = true
			if f.size > 0 {
				s.files = append(s.files, f)
				end = max(end, f.offset+f.size)
			}
		}
		slices.SortStableFunc(s.files, func(a, b cabFile) int { return a.offset - b.offset })
		if end > 0 && !cabDecode(b, folder, reserve, end, s) {
			return walk.Extraction{}
		}
	}
	return w.result()
}
