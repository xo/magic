//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/cpio.rs and src/structures/cpio.rs of
// binwalk. binwalk extracts a CPIO archive with 7zz, from
// src/extractors/sevenzip.rs, and extractCPIO writes the files that 7zz
// writes.

package archive

import (
	"bytes"
	"fmt"
	"io/fs"
	"strconv"
	"unicode/utf8"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// CPIO is the signature "cpio" of binwalk.
var CPIO = &walk.Signature{
	Name:        "cpio",
	Magic:       [][]byte{[]byte("070701"), []byte("070702")},
	Description: "CPIO ASCII archive",
	Parse:       parseCPIO,
	Extract:     extractCPIO,
}

// cpioTrailer is the name of the last entry of an archive.
const cpioTrailer = "TRAILER!!!"

// parseCPIO is cpio_parser.
func parseCPIO(data []byte, off int) (walk.Match, error) {
	count, size := 0, 0
	next, previous := off, -1
	for binwalk.OffsetSafe(len(data), next, previous) {
		h, ok := cpioHeader(data[next:])
		if !ok || (h.magic != "070701" && h.magic != "070702") {
			break
		}
		count++
		size += h.headerSize + h.dataSize
		if h.name == cpioTrailer {
			// One entry and the trailer at least, or binwalk takes the
			// archive for a false positive.
			if count > 1 {
				return walk.Match{
					Offset:      off,
					Size:        size,
					Confidence:  walk.High,
					Description: fmt.Sprintf("CPIO ASCII archive, file count: %d", count-1),
				}, nil
			}
			break
		}
		previous = next
		next = off + size
	}
	return walk.Match{}, walk.ErrNoMatch
}

// cpioHeaderSize is CPIO_HEADER_SIZE: the fixed part of a header, before
// the name.
const cpioHeaderSize = 110

// cpioEntry is CPIOEntryHeader, with the fields that the extractor reads.
type cpioEntry struct {
	magic      string
	name       string
	headerSize int // the header and the name, padded to 4 bytes
	dataSize   int // the data, padded to 4 bytes
	fileSize   int // the data, not padded
}

// cpioHeader is parse_cpio_entry_header.
func cpioHeader(data []byte) (cpioEntry, bool) {
	if len(data) <= cpioHeaderSize {
		return cpioEntry{}, false
	}
	fileSize, ok := cpioHex(data[54:62])
	if !ok {
		return cpioEntry{}, false
	}
	nameSize, ok := cpioHex(data[94:102])
	if !ok {
		return cpioEntry{}, false
	}
	// binwalk subtracts the NUL from the size of the name in a usize. A
	// size of 0 wraps, and the range of the name is then empty and fails.
	name, ok := binwalk.Range(data, cpioHeaderSize, cpioHeaderSize+nameSize-1)
	if !ok || !utf8.Valid(name) {
		return cpioEntry{}, false
	}
	header := cpioHeaderSize + nameSize
	return cpioEntry{
		magic:      string(data[:6]),
		name:       string(name),
		headerSize: header + cpioPadding(header),
		dataSize:   fileSize + cpioPadding(fileSize),
		fileSize:   fileSize,
	}, true
}

// cpioHex is usize::from_str_radix(s, 16) of Rust: hex digits of either
// case, after one optional "+".
func cpioHex(b []byte) (int, bool) {
	if !utf8.Valid(b) {
		return 0, false
	}
	s := string(b)
	if len(s) > 1 && s[0] == '+' {
		s = s[1:]
	}
	if s == "" || s[0] == '+' || s[0] == '-' {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0, false
	}
	return int(n), true
}

// cpioPadding is byte_padding: the bytes up to the next multiple of 4.
func cpioPadding(n int) int {
	if n%4 == 0 {
		return 0
	}
	return 4 - n%4
}

// The types of the mode of a CPIO entry, as in stat.h.
const (
	cpioTypeMask   = 0o170000
	cpioTypeSocket = 0o140000
	cpioTypeLink   = 0o120000
	cpioTypeFile   = 0o100000
	cpioTypeBlock  = 0o060000
	cpioTypeDir    = 0o040000
	cpioTypeChar   = 0o020000
	cpioTypeFIFO   = 0o010000
)

// extractCPIO writes the members of the CPIO archive at off, as 7zz writes
// them from the bytes that binwalk carves. It reads the entries as the
// parser does, up to the trailer.
func extractCPIO(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := parseCPIO(data, off)
	if err != nil {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	// binwalk carves the bytes of the match to a file, and fails when the
	// match goes past the end of the data.
	b, ok := binwalk.Range(data, off, off+m.Size)
	if !ok {
		return walk.Extraction{}
	}
	w := &writer{out: out}
	for next := 0; next < len(b) && !w.stopped; {
		h, ok := cpioHeader(b[next:])
		if !ok || h.name == cpioTrailer {
			break
		}
		content, ok := binwalk.Range(b, next+h.headerSize, next+h.headerSize+h.fileSize)
		if !ok {
			break
		}
		mode, _ := cpioHex(b[next+14 : next+22])
		major, _ := cpioHex(b[next+78 : next+86])
		minor, _ := cpioHex(b[next+86 : next+94])
		next += h.headerSize + h.dataSize
		mem := member{name: h.name, mode: fs.FileMode(mode) & fs.ModePerm, major: major, minor: minor}
		switch mode & cpioTypeMask {
		case cpioTypeFile:
			mem.data = bytes.NewReader(content)
		case cpioTypeDir:
			mem.mode |= fs.ModeDir
		case cpioTypeLink:
			mem.mode |= fs.ModeSymlink
			mem.link = string(content)
		case cpioTypeChar:
			mem.mode |= fs.ModeDevice | fs.ModeCharDevice
		case cpioTypeBlock:
			mem.mode |= fs.ModeDevice
		case cpioTypeFIFO:
			mem.mode |= fs.ModeNamedPipe
		case cpioTypeSocket:
			mem.mode |= fs.ModeSocket
		default:
			continue
		}
		w.write(mem)
	}
	return w.result()
}
