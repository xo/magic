//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/extractors/inflate.rs, src/formats/gzip.rs,
// src/formats/zlib.rs and src/formats/gpg.rs of binwalk-ng.

package compressed

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/adler32"
	"io"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
	"github.com/xo/magic/walk/internal/flate"
)

// inflated is DeflateResult of inflate.rs.
type inflated struct {
	size    int    // the bytes of deflate data
	adler32 uint32 // the Adler-32 of the data that came out
	ok      bool
}

// inflate is inflate_decompressor: it decodes the deflate data at off, and
// appends what comes out to decompressed.bin when out is set. It succeeds
// when the stream ends and gave some data.
//
// flate2 1.1.10, which binwalk-ng uses, fails on a stream that is cut
// short, and the flate of the walker reports io.ErrUnexpectedEOF for it.
// The flate of the walker decodes as miniz_oxide, the inflater of flate2,
// decodes. A bytes.Reader is an io.ByteReader, so flate reads no byte past
// the end of a whole stream, and the bytes that it read are the size.
func inflate(data []byte, off int, out *walk.Output) inflated {
	const blockSize = 8192
	src, ok := binwalk.From(data, off)
	if !ok {
		return inflated{}
	}
	br := bytes.NewReader(src)
	r := flate.NewReader(br)
	sum := adler32.New()
	buf := make([]byte, blockSize)
	total := 0
	for {
		n, err := r.Read(buf)
		if n > 0 {
			total += n
			sum.Write(buf[:n])
			if out != nil && !out.AppendFile("decompressed.bin", buf[:n]) {
				return inflated{}
			}
		}
		switch {
		case err == nil:
			continue
		case errors.Is(err, io.EOF):
			if total == 0 {
				return inflated{}
			}
			return inflated{size: len(src) - br.Len(), adler32: sum.Sum32(), ok: true}
		default:
			return inflated{}
		}
	}
}

// Gzip is the signature "gzip" of binwalk-ng.
var Gzip = &walk.Signature{
	Name:        "gzip",
	Magic:       [][]byte{[]byte("\x1f\x8b\x08")},
	Description: "gzip compressed data",
	Parse:       parseGzip,
	Extract:     extractGzip,
}

// extractGzip is gzip_decompress: it inflates the data after the header.
func extractGzip(data []byte, off int, out *walk.Output) walk.Extraction {
	h, ok := gzipHeader(data[off:])
	if !ok || len(data) <= off+h.size {
		return walk.Extraction{}
	}
	r := inflate(data, off+h.size, out)
	return walk.Extraction{OK: r.ok, Size: r.size, HasSize: r.ok}
}

// parseGzip is gzip_parser.
func parseGzip(data []byte, off int) (walk.Match, error) {
	const crcSize, isizeSize = 4, 4
	r := extractGzip(data, off, nil)
	if !r.OK {
		return walk.Match{}, walk.ErrNoMatch
	}
	h, ok := gzipHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	name := ""
	if h.name != "" {
		name = " original file name: \"" + h.name + "\","
	}
	total := h.size + r.Size + crcSize + isizeSize
	return walk.Match{
		Offset:      off,
		Size:        total,
		Confidence:  walk.High,
		Description: fmt.Sprintf("gzip compressed data,%s operating system: %s, timestamp: %s, total size: %d bytes", name, h.os, binwalk.EpochString(h.timestamp), total),
	}, nil
}

// gzipOS are the systems of a gzip header that binwalk knows.
var gzipOS = map[int]string{
	0:   "FAT filesystem (MS-DOS, OS/2, NT/Win32",
	1:   "Amiga",
	2:   "VMS (or OpenVMS)",
	3:   "Unix",
	4:   "VM/CMS",
	5:   "Atari TOS",
	6:   "HPFS filesystem (OS/2, NT)",
	7:   "Macintosh",
	8:   "Z-System",
	9:   "CP/M",
	10:  "TOPS-20",
	11:  "NTFS filesystem (NT)",
	12:  "QDOS",
	13:  "Acorn RISCOS",
	255: "unknown",
}

var (
	gzipStruct = binwalk.Struct{
		{Name: "magic", Type: "u16"},
		{Name: "compression_method", Type: "u8"},
		{Name: "flags", Type: "u8"},
		{Name: "timestamp", Type: "u32"},
		{Name: "extra_flags", Type: "u8"},
		{Name: "osid", Type: "u8"},
	}
	gzipExtra = binwalk.Struct{{Name: "id", Type: "u16"}, {Name: "extra_data_len", Type: "u16"}}
)

type gzipInfo struct {
	os, name  string
	size      int
	timestamp uint32
}

// gzipHeader is parse_gzip_header.
func gzipHeader(data []byte) (gzipInfo, bool) {
	const (
		flagCRC      = 0b0000_0010
		flagExtra    = 0b0000_0100
		flagName     = 0b0000_1000
		flagComment  = 0b0001_0000
		flagReserved = 0b1110_0000
	)
	h, ok := gzipStruct.Parse(data, false)
	if !ok {
		return gzipInfo{}, false
	}
	os, known := gzipOS[h["osid"]]
	if h["flags"]&flagReserved != 0 || h["compression_method"] != 8 || !known {
		return gzipInfo{}, false
	}
	info := gzipInfo{os: os, size: gzipStruct.Size(), timestamp: uint32(h["timestamp"])}
	if h["flags"]&flagExtra != 0 {
		b, ok := binwalk.Range(data, info.size, info.size+gzipExtra.Size())
		if !ok {
			return gzipInfo{}, false
		}
		x, ok := gzipExtra.Parse(b, false)
		if !ok {
			return gzipInfo{}, false
		}
		info.size += gzipExtra.Size() + x["extra_data_len"]
	}
	if h["flags"]&flagName != 0 {
		b, ok := binwalk.From(data, info.size)
		if !ok {
			return gzipInfo{}, false
		}
		info.name = binwalk.CString(b)
		info.size += len(info.name) + 1
	}
	if h["flags"]&flagComment != 0 {
		b, ok := binwalk.From(data, info.size)
		if !ok {
			return gzipInfo{}, false
		}
		info.size += len(binwalk.CString(b)) + 1
	}
	if h["flags"]&flagCRC != 0 {
		info.size += 2
	}
	return info, len(data) >= info.size
}

// Zlib is the signature "zlib" of binwalk-ng.
var Zlib = &walk.Signature{
	Name:        "zlib",
	Short:       true,
	Magic:       [][]byte{[]byte("\x78\x01"), []byte("\x78\x9c"), []byte("\x78\xDA"), []byte("\x78\x5E")},
	Description: "Zlib compressed file",
	Parse:       parseZlib,
	Extract:     extractZlib,
}

// extractZlib is zlib_decompress: it inflates the data after the header of
// two bytes, and checks the Adler-32 that follows it.
func extractZlib(data []byte, off int, out *walk.Output) walk.Extraction {
	const headerSize, checksumSize = 2, 4
	r := inflate(data, off+headerSize, out)
	if !r.ok {
		return walk.Extraction{}
	}
	start := off + headerSize + r.size
	b, ok := binwalk.Range(data, start, start+checksumSize)
	if !ok || binary.BigEndian.Uint32(b) != r.adler32 {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true, Size: headerSize + r.size + checksumSize, HasSize: true}
}

// parseZlib is zlib_parser.
func parseZlib(data []byte, off int) (walk.Match, error) {
	r := extractZlib(data, off, nil)
	if !r.OK {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        r.Size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("Zlib compressed file, total size: %d bytes", r.Size),
	}, nil
}

// GPGSigned is the signature "gpg_signed" of binwalk-ng: zlib data whose two
// bytes of magic are those of GPG.
var GPGSigned = &walk.Signature{
	Name:        "gpg_signed",
	Short:       true,
	Magic:       [][]byte{[]byte("\xA3\x01")},
	Description: "GPG signed file",
	Parse:       parseGPGSigned,
	Extract:     extractGPG,
}

// extractGPG is gpg_decompress. It checks no Adler-32.
func extractGPG(data []byte, off int, out *walk.Output) walk.Extraction {
	const headerSize = 2
	r := inflate(data, off+headerSize, out)
	if !r.ok {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true, Size: headerSize + r.size, HasSize: true}
}

// parseGPGSigned is gpg_signed_parser.
func parseGPGSigned(data []byte, off int) (walk.Match, error) {
	r := extractGPG(data, off, nil)
	if !r.OK {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        r.Size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("GPG signed file, total size: %d bytes", r.Size),
	}, nil
}
