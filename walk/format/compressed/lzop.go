//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/lzop.rs and src/structures/lzop.rs of
// binwalk. binwalk extracts lzop files with the program lzop, from
// src/extractors/lzop.rs, and extractLZOP writes what lzop -p -N -d writes.

package compressed

import (
	"encoding/binary"
	"fmt"
	"hash/adler32"
	"hash/crc32"
	"strings"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
	"github.com/xo/magic/walk/internal/lzo"
)

// LZOP is the signature "lzop" of binwalk: a file of the program lzop.
var LZOP = &walk.Signature{
	Name:        "lzop",
	Magic:       [][]byte{[]byte("\x89LZO\x00\x0D\x0A\x1A\x0A")},
	Description: "LZO compressed data",
	Parse:       parseLZOP,
	Extract:     extractLZOP,
}

// The flags of the header of lzop.
const (
	lzopAdler32D = 0x00000001
	lzopAdler32C = 0x00000002
	lzopExtra    = 0x00000040
	lzopCRC32D   = 0x00000100
	lzopCRC32C   = 0x00000200
	lzopFilter   = 0x00000800
	lzopHCRC32   = 0x00001000
)

var (
	lzopStruct1 = binwalk.Struct{
		{Name: "magic_p1", Type: "u8"},
		{Name: "magic_p2", Type: "u64"},
		{Name: "version", Type: "u16"},
		{Name: "lib_version", Type: "u16"},
		{Name: "version_needed", Type: "u16"},
		{Name: "method", Type: "u8"},
		{Name: "level", Type: "u8"},
		{Name: "flags", Type: "u32"},
	}
	lzopStruct2 = binwalk.Struct{
		{Name: "mode", Type: "u32"},
		{Name: "mtime", Type: "u32"},
		{Name: "gmt_diff", Type: "u32"},
		{Name: "file_name_length", Type: "u8"},
	}
	lzopBlock = binwalk.Struct{
		{Name: "uncompressed_size", Type: "u32"},
		{Name: "compressed_size", Type: "u32"},
		{Name: "uncompressed_checksum", Type: "u32"},
	}
)

// parseLZOPHeader is parse_lzop_file_header. It returns the size of the
// header. binwalk tests the flag of the checksums of the compressed data
// with "flags & ADLER32_C & CRC32_C", which is always 0, so it never counts
// those checksums, and parseLZOPHeader does not either.
func parseLZOPHeader(data []byte) (int, bool) {
	h, ok := lzopStruct1.Parse(data, true)
	if !ok || h["method"] < 1 || h["method"] > 3 {
		return 0, false
	}
	if h["version"] > 0x1040 || h["version"] < h["version_needed"] {
		return 0, false
	}
	start := 21
	if h["flags"]&lzopFilter != 0 {
		start += 4
	}
	end := start + 13
	b, ok := binwalk.Range(data, start, end)
	if !ok {
		return 0, false
	}
	h2, ok := lzopStruct2.Parse(b, true)
	if !ok {
		return 0, false
	}
	size := end + h2["file_name_length"] + 4
	if size > len(data) {
		return 0, false
	}
	return size, true
}

// parseLZOP is lzop_parser.
func parseLZOP(data []byte, off int) (walk.Match, error) {
	hs, ok := parseLZOPHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	blocks, ok := binwalk.From(data, off+hs)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	n, ok := lzoDataSize(blocks)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := hs + n
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("LZO compressed data, total size: %d bytes", size),
	}, nil
}

// lzoDataSize is get_lzo_data_size, with parse_lzop_block_header and
// parse_lzop_eof_marker. It needs two blocks, and then a word of zero.
func lzoDataSize(data []byte) (int, bool) {
	size, last, blocks := 0, -1, 0
	for binwalk.OffsetSafe(len(data), size, last) {
		b, ok := lzopBlock.Parse(data[size:], true)
		if !ok || b["compressed_size"] == 0 || b["uncompressed_size"] == 0 ||
			b["uncompressed_checksum"] == 0 || b["uncompressed_size"] > 64<<20 {
			break
		}
		blocks++
		last = size
		size += lzopBlock.Size() + b["compressed_size"]
	}
	if blocks < 2 || size > len(data)-4 || binary.BigEndian.Uint32(data[size:]) != 0 {
		return 0, false
	}
	return size + 4, true
}

// extractLZOP writes what lzop -p -N -d writes for the carved match. lzop
// reads the header by its version, checks the checksum of the header and of
// each block, and decodes each block with lzo1x_decompress_safe. It names
// the file after the name in the header, without its folders, or after
// the carved file without its suffix ".lzo".
func extractLZOP(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := parseLZOP(data, off)
	if err != nil {
		return walk.Extraction{}
	}
	in := data[off : off+m.Size]
	r := lzopReader{b: in, pos: 9, ok: true}
	version := r.u16()
	r.u16() // the version of the library
	if version >= 0x0940 {
		if needed := r.u16(); needed > 0x1040 || needed < 0x0900 {
			return walk.Extraction{}
		}
	}
	method := r.u8()
	if version >= 0x0940 {
		r.u8() // the level
	}
	flags := r.u32()
	if flags&lzopFilter != 0 || method < 1 || method > 3 {
		return walk.Extraction{} // the walker has no filter of lzop
	}
	r.u32() // the mode
	r.u32() // the time
	if version >= 0x0940 {
		r.u32() // the high word of the time
	}
	name := string(r.bytes(int(r.u8())))
	sum := r.pos
	check := r.u32()
	if !r.ok || check != lzopChecksum(flags&lzopHCRC32 != 0, in[9:sum]) {
		return walk.Extraction{}
	}
	if flags&lzopExtra != 0 {
		n := r.u32()
		r.bytes(int(n))
		r.u32()
	}
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		name = fmt.Sprintf("lzop_%X", off)
	}
	w := &appender{out: out, name: name}
	for r.ok {
		dlen := r.u32()
		if dlen == 0 {
			break
		}
		clen := r.u32()
		if dlen > 64<<20 || clen == 0 || clen > dlen {
			return walk.Extraction{}
		}
		var sums []uint32
		for _, f := range []uint32{lzopAdler32D, lzopCRC32D, lzopAdler32C, lzopCRC32C} {
			switch {
			case flags&f == 0:
				sums = append(sums, 0)
			case f&(lzopAdler32C|lzopCRC32C) != 0 && clen == dlen:
				// A stored block has one checksum, of its data.
				sums = append(sums, sums[len(sums)-2])
			default:
				sums = append(sums, r.u32())
			}
		}
		src := r.bytes(int(clen))
		if !r.ok {
			return walk.Extraction{}
		}
		if !lzopVerify(flags, lzopAdler32C, lzopCRC32C, sums[2:], src) {
			return walk.Extraction{}
		}
		dst := src
		if clen < dlen {
			if dst, err = lzo.Decode(src, int(dlen)); err != nil || len(dst) != int(dlen) {
				return walk.Extraction{}
			}
		}
		if !lzopVerify(flags, lzopAdler32D, lzopCRC32D, sums[:2], dst) {
			return walk.Extraction{}
		}
		if _, err := w.Write(dst); err != nil {
			return walk.Extraction{}
		}
	}
	if !r.ok {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true, Size: m.Size, HasSize: true}
}

// lzopChecksum is the checksum of a header of lzop: Adler-32, or CRC-32
// when the flag F_H_CRC32 is set.
func lzopChecksum(crc bool, b []byte) uint32 {
	if crc {
		return crc32.ChecksumIEEE(b)
	}
	return adler32.Checksum(b)
}

// lzopVerify compares the Adler-32 and the CRC-32 of b with sums, for each
// of the two whose flag is set.
func lzopVerify(flags, adler, crc uint32, sums []uint32, b []byte) bool {
	if flags&adler != 0 && adler32.Checksum(b) != sums[0] {
		return false
	}
	return flags&crc == 0 || crc32.ChecksumIEEE(b) == sums[1]
}

// lzopReader reads the big-endian fields of a file of lzop. A read past the
// end clears ok, and returns zero.
type lzopReader struct {
	b   []byte
	pos int
	ok  bool
}

func (r *lzopReader) bytes(n int) []byte {
	if !r.ok || n < 0 || len(r.b)-r.pos < n {
		r.ok = false
		return nil
	}
	b := r.b[r.pos : r.pos+n]
	r.pos += n
	return b
}

func (r *lzopReader) u8() uint8 {
	if b := r.bytes(1); b != nil {
		return b[0]
	}
	return 0
}

func (r *lzopReader) u16() uint16 {
	if b := r.bytes(2); b != nil {
		return binary.BigEndian.Uint16(b)
	}
	return 0
}

func (r *lzopReader) u32() uint32 {
	if b := r.bytes(4); b != nil {
		return binary.BigEndian.Uint32(b)
	}
	return 0
}
