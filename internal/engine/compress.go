package engine

import (
	"strings"

	"github.com/xo/magic/compress"
)

// This file is file_zmagic of compress.c: with the flag Compress, libmagic
// looks inside compressed data. It decodes gzip, zlib, bzip2, xz, lzma and
// zstd with the libraries that it links. The port decodes a format when a
// program registers a decoder for it with github.com/xo/magic/compress
// (D45). For the other formats libmagic runs an outside program, which the
// port never does (D36), so the port answers as libmagic does when it finds
// no such program.

// The flags of compression.
const (
	Compress       Flags = 0x0000004
	CompressTransp Flags = 0x2000000
	NoCompressFork Flags = 0x4000000
)

// The results of uncompressbuf.
const (
	okData = iota
	noData
	errData
)

// compressor is one row of compr in compress.c.
type compressor struct {
	magic string
	match func([]byte) bool
	size  int    // how many bytes the magic needs
	prog  string // the program that libmagic runs
}

var compressors = []compressor{
	{magic: "\037\235", size: 2, prog: "gzip"},         // 0, compress
	{magic: "\037\235", size: 2, prog: "uncompress"},   // 1, compress
	{magic: "\037\213", size: 2, prog: "gzip"},         // 2, gzip
	{magic: "\037\236", size: 2, prog: "gzip"},         // 3, frozen
	{magic: "\037\240", size: 2, prog: "gzip"},         // 4, SCO LZH
	{magic: "\037\036", size: 2, prog: "gzip"},         // 5, packed
	{magic: "PK\003\004", size: 4, prog: "gzip"},       // 6, pkzip
	{magic: "BZh", size: 3, prog: "bzip2"},             // 7, bzip2
	{magic: "LZIP", size: 4, prog: "lzip"},             // 8, lzip
	{magic: "\3757zXZ\000", size: 6, prog: "xz"},       // 9, xz
	{magic: "LRZI", size: 4, prog: "lrzip"},            // 10, lrzip
	{magic: "\004\"M\030", size: 4, prog: "lz4"},       // 11, lz4
	{magic: "\x28\xb5\x2f\xfd", size: 4, prog: "zstd"}, // 12, zstd
	{match: lzmaCmp, size: 13, prog: "xz"},             // 13, lzma
	{match: zlibCmp, size: 2, prog: "python"},          // 14, zlib
}

// zlibCmp is zlibcmp: a zlib header.
func zlibCmp(b []byte) bool {
	if b[0]&0xf != 8 || b[0]&0x80 != 0 {
		return false
	}
	return (uint16(b[0])<<8|uint16(b[1]))%31 == 0
}

// lzmaCmp is lzmacmp: an lzma header.
func lzmaCmp(b []byte) bool {
	if b[0] != 0x5d || b[1] != 0 || b[2] != 0 {
		return false
	}
	return b[12] == 0 || b[12] == 0xff
}

// methodName is methodname of compress.c: the library of a registered
// decoder, and else the program.
func methodName(i int) string {
	if name, _, ok := decoder(i); ok {
		return name
	}
	return compressors[i].prog
}

// zmagic is file_zmagic: it describes the data inside compressed data, and
// then the compressed data. It returns 1 when it printed, and -1 on an
// error.
func (st *state) zmagic(b *buffer) int {
	if st.flags&Compress == 0 {
		return 0
	}
	mime := st.flags & Mime
	buf := b.data
	rv := 0
	defer func() { st.flags |= Compress }()
	for i, c := range compressors {
		if len(buf) < c.size {
			continue
		}
		if c.match != nil {
			if !c.match(buf) {
				continue
			}
		} else if !strings.HasPrefix(string(buf), c.magic) {
			continue
		}
		urv, out := st.uncompress(i, buf)
		if urv == noData {
			continue
		}
		st.flags &^= Compress
		var prv int
		if urv == errData {
			prv = st.decompressionError(i, out)
		} else {
			prv = st.buffer(&buffer{data: out}, len(out))
		}
		if prv == -1 {
			return -1
		}
		rv = 1
		if st.flags&CompressTransp != 0 {
			return rv
		}
		if mime != Mime && mime != 0 {
			return rv
		}
		lead := " ("
		if mime != 0 {
			lead = " compressed-encoding="
		}
		if st.printf("%s", lead) == -1 {
			return -1
		}
		pb := st.push()
		if st.buffer(&buffer{data: buf}, len(buf)) == -1 {
			st.pop(pb)
			return -1
		}
		rbuf, ok := st.pop(pb)
		if !ok {
			return -1
		}
		if rbuf != nil && st.printf("%s", rbuf) == -1 {
			return -1
		}
		if mime == 0 && st.printf(")") == -1 {
			return -1
		}
	}
	return rv
}

// decompressionError is format_decompression_error.
func (st *state) decompressionError(i int, msg []byte) int {
	if st.flags&Mime == 0 {
		return st.printf("ERROR:[%s: %s]", methodName(i), msg)
	}
	m := make([]byte, len(msg))
	for k, c := range msg {
		if isAlnum(c) {
			m[k] = c
		} else {
			m[k] = '-'
		}
	}
	return st.printf("application/x-decompression-error-%s-%s", methodName(i), m)
}

// methods maps the rows of compr that libmagic decodes with a library to
// the methods of the registry.
var methods = map[int]compress.Method{
	2:  compress.Gzip,
	14: compress.Zlib,
	7:  compress.Bzip2,
	9:  compress.XZ,
	13: compress.LZMA,
	12: compress.Zstd,
}

// decoder returns the registered decoder of row i of compr, and the name of
// its library.
func decoder(i int) (string, compress.Decoder, bool) {
	m, ok := methods[i]
	if !ok {
		return "", nil, false
	}
	return compress.Lookup(m)
}

// uncompress is uncompressbuf: the data decompressed with row i, at most
// bytesMax bytes, or the text of an error.
func (st *state) uncompress(i int, old []byte) (int, []byte) {
	if _, d, ok := decoder(i); ok {
		if st.flags&NoCompressFork != 0 {
			return errData, []byte("Fork is required to uncompress, but disabled")
		}
		out, err := d(old, bytesMax)
		if err != nil {
			return errData, []byte(err.Error())
		}
		return okData, out
	}
	// libmagic would run a program here. The port runs none (D36), and
	// answers as libmagic does when posix_spawnp finds no program.
	return errData, []byte("Cannot posix_spawn `" + compressors[i].prog + "', No such file or directory")
}
