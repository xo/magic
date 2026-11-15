//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/xz.rs, src/structures/xz.rs,
// src/signatures/lzma.rs, src/structures/lzma.rs and src/extractors/lzma.rs
// of binwalk.

package compressed

import (
	"errors"
	"fmt"

	"github.com/xo/magic/compress"
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

var errWrite = errors.New("compressed: the output refused the data")

// lzmaDecompress is lzma_decompress: it decodes the stream at off with the
// auto decoder of liblzma, which takes xz or lzma, and writes what comes out
// to decompressed.bin when out is set. It fails when the stream gave no
// data, and when no decoder is registered.
func lzmaDecompress(data []byte, off int, out *walk.Output) walk.Extraction {
	m := compress.LZMA
	if len(data)-off >= 6 && string(data[off:off+6]) == "\xFD7zXZ\x00" {
		m = compress.XZ
	}
	s, ok := compress.LookupStream(m)
	if !ok {
		return walk.Extraction{}
	}
	w := &appender{out: out, name: "decompressed.bin"}
	n, err := s(data[off:], w)
	if err != nil || w.failed || w.n == 0 {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true, Size: n, HasSize: true}
}

// XZ is the signature "xz" of binwalk.
var XZ = &walk.Signature{
	Name:        "xz",
	Magic:       [][]byte{[]byte("\xFD\x37\x7a\x58\x5a\x00")},
	Description: "XZ compressed data",
	Parse:       parseXZ,
	Extract:     lzmaDecompress,
}

var xzStruct = binwalk.Struct{
	{Name: "magic_p1", Type: "u32"},
	{Name: "magic_p2", Type: "u16"},
	{Name: "flags", Type: "u16"},
	{Name: "header_crc", Type: "u32"},
}

// xzHeader is parse_xz_header: the header checks with its CRC-32.
func xzHeader(data []byte) bool {
	h, ok := xzStruct.Parse(data, false)
	return ok && binwalk.CRC32(data[6:8]) == uint32(h["header_crc"])
}

// failed is the extractor of a match that binwalk extracts with 7-Zip, which
// the walker does not run (D36).
func failed([]byte, int, *walk.Output) walk.Extraction {
	return walk.Extraction{}
}

// parseXZ is xz_parser. It adds the size of each stream that follows the
// last one. binwalk compares the absolute offset of the next stream with the
// bytes that follow the match, so it misses a stream that starts further into
// the data than its own length. The walker compares it with the bytes of the
// data, as binwalk 3.1.0 did, and the oracle carries the same fix (D59).
func parseXZ(data []byte, off int) (walk.Match, error) {
	m := walk.Match{Offset: off, Confidence: walk.High, Description: "XZ compressed data"}
	next, previous := off, -1
	available := len(data)
	headers := 0
	for binwalk.OffsetSafe(available, next, previous) {
		if !xzHeader(data[next:]) {
			break
		}
		headers++
		r := lzmaDecompress(data, next, nil)
		if !r.OK {
			// binwalk extracts it with 7-Zip.
			m.Extract = failed
			m.Description += ", valid header with malformed data stream"
			break
		}
		previous = next
		next += r.Size
		m.Size += r.Size
	}
	if headers == 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	if m.Size > 0 {
		m.Description = fmt.Sprintf("%s, total size: %d bytes", m.Description, m.Size)
	}
	return m, nil
}

// LZMA is the signature "lzma" of binwalk. binwalk extracts it with 7-Zip,
// which writes the data to lzma_<offset in hexadecimal>. The walker writes
// the same file.
var LZMA = &walk.Signature{
	Name:        "lzma",
	Magic:       lzmaMagic(),
	Description: "LZMA compressed data",
	Parse:       parseLZMA,
	Extract:     extractLZMA,
}

// lzmaMagic is lzma_magic: each supported property byte with each
// supported size of dictionary.
func lzmaMagic() [][]byte {
	out := make([][]byte, 0, 48)
	for _, p := range []byte{0x5D, 0x6E, 0x6D, 0x6C} {
		for _, d := range []uint32{
			0x10000000, 0x20000000, 0x01000000, 0x02000000, 0x04000000, 0x00800000,
			0x00400000, 0x00200000, 0x00100000, 0x00080000, 0x00020000, 0x00010000,
		} {
			out = append(out, []byte{p, byte(d), byte(d >> 8), byte(d >> 16), byte(d >> 24)})
		}
	}
	return out
}

var lzmaStruct = binwalk.Struct{
	{Name: "properties", Type: "u8"},
	{Name: "dictionary_size", Type: "u32"},
	{Name: "decompressed_size", Type: "u64"},
	{Name: "null_byte", Type: "u8"},
}

// parseLZMA is lzma_parser, with parse_lzma_header.
func parseLZMA(data []byte, off int) (walk.Match, error) {
	h, ok := lzmaStruct.Parse(data[off:], false)
	if !ok || h["null_byte"] != 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	// The size is a u64 in binwalk; -1 here is 0xFFFFFFFFFFFFFFFF, a stream
	// of unknown size.
	size := uint64(h["decompressed_size"])
	if size < 256 || (size != ^uint64(0) && size > 0xFFFFFFFF) {
		return walk.Match{}, walk.ErrNoMatch
	}
	r := lzmaDecompress(data, off, nil)
	if !r.OK {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       r.Size,
		Confidence: walk.High,
		Description: fmt.Sprintf("LZMA compressed data, properties: 0x%02X, dictionary size: %d bytes, compressed size: %d bytes, uncompressed size: %d bytes",
			h["properties"], h["dictionary_size"], r.Size, int64(size)),
	}, nil
}

// extractLZMA writes what 7-Zip writes for a stream of lzma: its data, in a
// file named after the signature and the offset.
func extractLZMA(data []byte, off int, out *walk.Output) walk.Extraction {
	if out == nil {
		return lzmaDecompress(data, off, nil)
	}
	s, ok := compress.LookupStream(compress.LZMA)
	if !ok {
		return walk.Extraction{}
	}
	w := &appender{out: out, name: fmt.Sprintf("lzma_%X", off)}
	n, err := s(data[off:], w)
	if err != nil || w.failed || w.n == 0 {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true, Size: n, HasSize: true}
}
