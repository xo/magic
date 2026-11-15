//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/cpio.rs of binwalk-ng. binwalk-ng
// extracts a CPIO archive with 7z, from src/formats/sevenzip.rs, and
// extractCPIO writes the files that 7z writes.

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

// CPIO is the signature "cpio" of binwalk-ng.
var CPIO = &walk.Signature{
	Name:        "cpio",
	Magic:       [][]byte{[]byte("070701"), []byte("070702")},
	Description: "CPIO ASCII archive",
	Parse:       parseCPIO,
	Extract:     extractCPIO,
	Carve:       true,
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

// cpioItem is an entry of an archive, as extractCPIO reads it.
type cpioItem struct {
	name    string
	mode    int
	ino     [3]int // the inode, and the major and minor numbers of its device
	content []byte
}

// extractCPIO writes the members of the CPIO archive at off, as 7z writes
// them from the bytes that binwalk-ng carves, which are the data from off.
// 7z reads the entries up to the trailer, and stops at the first header
// with a field that is not 8 hex digits. When the first header is not
// valid, it looks for the magic of an archive further on. 7z writes a
// device, a FIFO and a socket as an empty file. It takes an entry with no
// data, other than a folder or a link, for a hard link to the next entry
// of the same inode that has data, and writes that data and its mode.
func extractCPIO(data []byte, off int, out *walk.Output) walk.Extraction {
	if _, err := parseCPIO(data, off); err != nil {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	b := data[off:]
	var items []cpioItem
	for start := 0; len(items) == 0 && start < len(b); {
		items = cpioItems(b[start:])
		i := cpioNextMagic(b[start+1:])
		if i < 0 {
			break
		}
		start += 1 + i
	}
	w := &writer{out: out}
	for i, it := range items {
		if w.stopped {
			break
		}
		typ := it.mode & cpioTypeMask
		if len(it.content) == 0 && typ != cpioTypeDir && typ != cpioTypeLink {
			for _, l := range items[i+1:] {
				if l.ino == it.ino && len(l.content) > 0 {
					it.mode, it.content = l.mode, l.content
					typ = cpioTypeFile
					break
				}
			}
		}
		mem := member{name: it.name, mode: fs.FileMode(it.mode) & fs.ModePerm}
		switch typ {
		case cpioTypeDir:
			mem.mode |= fs.ModeDir
		case cpioTypeLink:
			mem.mode |= fs.ModeSymlink
			mem.link = string(it.content)
		case cpioTypeFile:
			mem.data = bytes.NewReader(it.content)
		case cpioTypeChar, cpioTypeBlock, cpioTypeFIFO, cpioTypeSocket:
		default:
			continue
		}
		w.write(mem)
	}
	return w.result()
}

// cpioItems reads the entries of an archive at the start of b, as 7z reads
// them, up to the trailer or to the first header that is not valid.
func cpioItems(b []byte) []cpioItem {
	var items []cpioItem
	for next := 0; next < len(b); {
		h, ok := cpioHeader(b[next:])
		if !ok || (h.magic != "070701" && h.magic != "070702") || h.name == cpioTrailer {
			break
		}
		var fields [13]int
		for i := range fields {
			if fields[i], ok = cpioStrictHex(b[next+6+8*i : next+14+8*i]); !ok {
				return items
			}
		}
		content, ok := binwalk.Range(b, next+h.headerSize, next+h.headerSize+h.fileSize)
		if !ok {
			break
		}
		items = append(items, cpioItem{
			name:    h.name,
			mode:    fields[1],
			ino:     [3]int{fields[0], fields[7], fields[8]},
			content: content,
		})
		next += h.headerSize + h.dataSize
	}
	return items
}

// cpioNextMagic is the index of the next magic of a CPIO archive in b, or
// -1.
func cpioNextMagic(b []byte) int {
	i, j := bytes.Index(b, []byte("070701")), bytes.Index(b, []byte("070702"))
	switch {
	case i < 0:
		return j
	case j < 0:
		return i
	}
	return min(i, j)
}

// cpioStrictHex reads a field of 8 hex digits, as 7z reads it.
func cpioStrictHex(b []byte) (int, bool) {
	n := 0
	for _, c := range b {
		var d byte
		switch {
		case c >= '0' && c <= '9':
			d = c - '0'
		case c >= 'a' && c <= 'f':
			d = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, false
		}
		n = n<<4 | int(d)
	}
	return n, true
}
