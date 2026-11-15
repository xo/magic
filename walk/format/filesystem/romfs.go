//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/romfs.rs, src/structures/romfs.rs and
// src/extractors/romfs.rs of binwalk.

package filesystem

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// RomFS is the signature "romfs" of binwalk.
var RomFS = &walk.Signature{
	Name:        "romfs",
	Magic:       [][]byte{[]byte("-rom1fs-")},
	Description: "RomFS filesystem",
	Parse:       parseRomFS,
	Extract:     extractRomFS,
}

// parseRomFS is romfs_parser.
func parseRomFS(data []byte, off int) (walk.Match, error) {
	r := extractRomFS(data, off, nil)
	if !r.OK || !r.HasSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	h, ok := parseRomFSHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        r.Size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("RomFS filesystem, volume name: \"%s\", total size: %d bytes", h.volumeName, r.Size),
	}, nil
}

// The types of a file of RomFS, in the low 3 bits of the offset of the next
// header.
const (
	romfsDirectory   = 1
	romfsRegular     = 2
	romfsSymlink     = 3
	romfsBlockDevice = 4
	romfsCharDevice  = 5
	romfsSocket      = 6
	romfsFIFO        = 7
)

// romfsEntry is RomFSEntry of extractors/romfs.rs.
type romfsEntry struct {
	name       string
	info       int
	offset     int // the start of the data, from the start of the image
	size       int
	typ        int
	executable bool
	target     string // the target of a link
	major      int    // the numbers of a device
	minor      int
	children   []romfsEntry
}

// extractRomFS is extract_romfs. It writes the files to a folder that has
// the name of the volume.
func extractRomFS(data []byte, off int, out *walk.Output) walk.Extraction {
	h, ok := parseRomFSHeader(data[off:])
	if !ok {
		return walk.Extraction{}
	}
	romfs, ok := binwalk.Range(data, off, off+h.imageSize)
	if !ok {
		return walk.Extraction{}
	}
	// binwalk has no such limit. See processRomFSEntries.
	visits := len(romfs)/16 + 1
	root, ok := processRomFSEntries(romfs, h.headerSize, &visits)
	// binwalk expects at least one file in the root of the image.
	if !ok || len(root) == 0 {
		return walk.Extraction{}
	}
	r := walk.Extraction{OK: true, Size: h.imageSize, HasSize: true}
	if out != nil {
		count := 0
		// binwalk writes the files through a second Chroot at the folder of
		// the volume, so that a path or the target of a link stays inside it.
		if out.CreateDirectory(h.volumeName) {
			count = extractRomFSEntries(romfs, root, "", out.Sub(h.volumeName))
		}
		if count == 0 {
			r.OK = false
		}
	}
	return r
}

// processRomFSEntries is process_romfs_entries: the list of files whose
// first header is at off, with the files of each folder in it.
//
// A folder whose files start at a header that the walk of a parent folder
// already read makes binwalk recurse until its stack overflows, and folders
// that share their files make it take a time that grows exponentially with
// the depth. The walker stops both: it reads at most visits headers in all,
// which is more than a real image has, and rejects the image past that.
func processRomFSEntries(data []byte, off int, visits *int) ([]romfsEntry, bool) {
	var entries []romfsEntry
	var processed []int
	next, previous := off, -1
	// The offset of the next header is 0 at the end of the list.
	for next != 0 && binwalk.OffsetSafe(len(data), next, previous) {
		// A header at an offset seen before ends the loop, which otherwise
		// does not end.
		if slices.Contains(processed, next) {
			break
		}
		processed = append(processed, next)
		if *visits--; *visits < 0 {
			return nil, false
		}
		f, ok := parseRomFSFile(data[next:])
		if !ok {
			break
		}
		e := romfsEntry{
			name:       f.name,
			info:       f.info,
			offset:     next + f.dataOffset,
			size:       f.size,
			typ:        f.typ,
			executable: f.executable,
		}
		if e.offset+e.size > len(data) {
			return nil, false
		}
		if e.name != "." && e.name != ".." {
			switch e.typ {
			case romfsSymlink:
				b := data[e.offset : e.offset+e.size]
				if !utf8.Valid(b) {
					return nil, false
				}
				e.target = string(b)
			case romfsBlockDevice, romfsCharDevice:
				// The info field holds the numbers of a device.
				e.minor = e.info & 0xFFFF
				e.major = (e.info >> 16) & 0xFFFF
			}
			if e.typ == romfsDirectory {
				// The info field of a folder is the offset of its first
				// header.
				children, ok := processRomFSEntries(data, e.info, visits)
				if !ok {
					return nil, false
				}
				e.children = children
			}
			// binwalk keeps only folders, links and regular files.
			if e.typ == romfsDirectory || e.typ == romfsSymlink || e.typ == romfsRegular {
				entries = append(entries, e)
			}
		}
		previous = next
		next = f.next
	}
	return entries, true
}

// extractRomFSEntries is extract_romfs_entries: it writes entries to the
// folder parent of out, the output of the volume, and returns the number of
// files and folders that it wrote.
//
// binwalk joins the absolute path of the parent with the name of the entry,
// so a name with ".." that goes above the folder of the volume gives a path
// that depends on the folder of the output. The walker cleans the path inside
// the folder of the volume.
func extractRomFSEntries(data []byte, entries []romfsEntry, parent string, out *walk.Output) int {
	count := 0
	for _, e := range entries {
		p := strings.TrimPrefix(sanitizePath(parent+"/"+e.name, true), "/")
		var ok bool
		switch e.typ {
		case romfsDirectory:
			ok = out.CreateDirectory(p)
		case romfsRegular:
			ok = out.CarveFile(p, data, e.offset, e.size)
		case romfsSymlink:
			ok = out.CreateSymlink(p, e.target)
		case romfsFIFO:
			ok = out.CreateFIFO(p)
		case romfsSocket:
			ok = out.CreateSocket(p)
		case romfsBlockDevice:
			ok = out.CreateBlockDevice(p, e.major, e.minor)
		case romfsCharDevice:
			ok = out.CreateCharDevice(p, e.major, e.minor)
		default:
			continue
		}
		if !ok {
			continue
		}
		count++
		if e.typ == romfsDirectory && len(e.children) > 0 {
			count += extractRomFSEntries(data, e.children, p, out)
		}
		if e.typ == romfsRegular && e.executable {
			out.MakeExecutable(p)
		}
	}
	return count
}

var romfsHeader = binwalk.Struct{
	{Name: "magic", Type: "u64"},
	{Name: "image_size", Type: "u32"},
	{Name: "checksum", Type: "u32"},
}

// romfsHeaderInfo is RomFSHeader.
type romfsHeaderInfo struct {
	imageSize  int
	headerSize int // the bytes of the header with the name of the volume
	volumeName string
}

// parseRomFSHeader is parse_romfs_header.
func parseRomFSHeader(data []byte) (romfsHeaderInfo, bool) {
	// The checksum covers at most the first 512 bytes of the image.
	const maxCRCLen = 512
	h, ok := romfsHeader.Parse(data, true)
	if !ok || h["image_size"] <= romfsHeader.Size() {
		return romfsHeaderInfo{}, false
	}
	// The name of the volume follows the header, and ends with a NUL.
	name := binwalk.CString(data[romfsHeader.Size():])
	crc, ok := binwalk.Range(data, 0, min(maxCRCLen, h["image_size"]))
	if !ok || !romfsCRCValid(crc) {
		return romfsHeaderInfo{}, false
	}
	return romfsHeaderInfo{
		imageSize: h["image_size"],
		// The name has a NUL, and is padded to 16 bytes.
		headerSize: romfsHeader.Size() + romfsAlign(len(name)+1),
		volumeName: name,
	}, true
}

var romfsFileHeader = binwalk.Struct{
	{Name: "next_header_offset", Type: "u32"},
	{Name: "info", Type: "u32"},
	{Name: "size", Type: "u32"},
	{Name: "checksum", Type: "u32"},
}

// romfsFile is the part of RomFSFileHeader that the extractor reads.
type romfsFile struct {
	info       int
	size       int
	name       string
	dataOffset int // the start of the data, from the start of the header
	typ        int
	executable bool
	next       int // the next header, from the start of the image
}

// parseRomFSFile is parse_romfs_file_entry.
func parseRomFSFile(data []byte) (romfsFile, bool) {
	const (
		typeMask = 0b0111
		execMask = 0b1000
		nextMask = 0xFFFFFFF0
	)
	h, ok := romfsFileHeader.Parse(data, true)
	if !ok {
		return romfsFile{}, false
	}
	// The name of the file follows the header, and ends with a NUL.
	name := binwalk.CString(data[romfsFileHeader.Size():])
	if name == "" {
		return romfsFile{}, false
	}
	next := h["next_header_offset"]
	return romfsFile{
		info:       h["info"],
		size:       h["size"],
		name:       name,
		dataOffset: romfsFileHeader.Size() + romfsAlign(len(name)+1),
		typ:        next & typeMask,
		executable: next&execMask != 0,
		next:       next & nextMask,
	}, true
}

// romfsAlign is romfs_align: x rounded up to 16 bytes.
func romfsAlign(x int) int {
	const alignment = 16
	if r := x % alignment; r > 0 {
		return x + alignment - r
	}
	return x
}

// romfsCRCValid is romfs_crc_valid: the sum of the big-endian 32-bit words
// of data is 0, and data is a whole number of words.
func romfsCRCValid(data []byte) bool {
	if len(data)%4 != 0 {
		return false
	}
	var sum uint32
	for i := 0; i < len(data); i += 4 {
		sum += binary.BigEndian.Uint32(data[i:])
	}
	return sum == 0
}

// sanitizePath is sanitize_path of the Chroot of binwalk. A ".." part goes,
// with the part before it that has not gone yet, and so does an empty part.
// The search for the part before stops at the first part, even when the
// first part has gone, as in binwalk.
func sanitizePath(p string, keepRoot bool) string {
	parts := strings.Split(p, "/")
	var exclude []int
	for i, part := range parts {
		switch part {
		case "..":
			exclude = append(exclude, i)
			if i > 0 {
				j := i - 1
				for j > 0 && slices.Contains(exclude, j) {
					j--
				}
				exclude = append(exclude, j)
			}
		case "":
			exclude = append(exclude, i)
		}
	}
	var sb strings.Builder
	if keepRoot && strings.HasPrefix(p, "/") {
		sb.WriteString("/")
	}
	for i, part := range parts {
		if !slices.Contains(exclude, i) {
			sb.WriteString("/")
			sb.WriteString(part)
		}
	}
	s := sb.String()
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	return s
}
