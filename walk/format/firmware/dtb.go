//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/dtb.rs, src/structures/dtb.rs and
// src/extractors/dtb.rs of binwalk.

package firmware

import (
	"fmt"
	"strings"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// DTB is the signature "dtb" of binwalk: a flattened device tree.
var DTB = &walk.Signature{
	Name:        "dtb",
	Magic:       [][]byte{[]byte("\xD0\x0D\xFE\xED")},
	Description: "Device tree blob (DTB)",
	Parse:       parseDTB,
	Extract:     extractDTB,
}

// dtbHeader is DTBHeader of dtb.rs.
type dtbHeader struct {
	totalSize     int
	version       int
	cpuID         int
	structOffset  int
	stringsOffset int
	structSize    int
	stringsSize   int
}

var dtbStruct = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "total_size", Type: "u32"},
	{Name: "dt_struct_offset", Type: "u32"},
	{Name: "dt_strings_offset", Type: "u32"},
	{Name: "mem_reservation_block_offset", Type: "u32"},
	{Name: "version", Type: "u32"},
	{Name: "min_compatible_version", Type: "u32"},
	{Name: "cpu_id", Type: "u32"},
	{Name: "dt_strings_size", Type: "u32"},
	{Name: "dt_struct_size", Type: "u32"},
}

// parseDTBHeader is parse_dtb_header. It tests the offset of the structure
// with "& 4", not "% 4", as binwalk does.
func parseDTBHeader(data []byte) (dtbHeader, bool) {
	h, ok := dtbStruct.Parse(data, true)
	if !ok || h["version"] != 17 || h["min_compatible_version"] != 16 {
		return dtbHeader{}, false
	}
	if h["dt_struct_offset"]&4 != 0 || h["mem_reservation_block_offset"]%8 != 0 {
		return dtbHeader{}, false
	}
	size := dtbStruct.Size()
	if h["dt_struct_offset"] < size || h["dt_strings_offset"] < size || h["mem_reservation_block_offset"] < size {
		return dtbHeader{}, false
	}
	return dtbHeader{
		totalSize:     h["total_size"],
		version:       h["version"],
		cpuID:         h["cpu_id"],
		structOffset:  h["dt_struct_offset"],
		stringsOffset: h["dt_strings_offset"],
		structSize:    h["dt_struct_size"],
		stringsSize:   h["dt_strings_size"],
	}, true
}

// parseDTB is dtb_parser. The size is the total size of the header, which
// binwalk does not compare with the data.
func parseDTB(data []byte, off int) (walk.Match, error) {
	h, ok := parseDTBHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	structEnd := off + h.structOffset + h.structSize
	stringsEnd := off + h.stringsOffset + h.stringsSize
	if len(data) < structEnd || len(data) < stringsEnd {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       h.totalSize,
		Confidence: walk.Medium,
		Description: fmt.Sprintf("Device tree blob (DTB), version: %d, CPU ID: %d, total size: %d bytes",
			h.version, h.cpuID, h.totalSize),
	}, nil
}

// dtbNode is DTBNode of dtb.rs.
type dtbNode struct {
	begin, end, eof, nop, property bool

	name      string
	data      []byte
	totalSize int
}

// The tokens of the structure of a device tree.
const (
	fdtBeginNode = 1
	fdtEndNode   = 2
	fdtProp      = 3
	fdtNOP       = 4
	fdtEnd       = 9
)

var (
	dtbToken    = binwalk.Struct{{Name: "id", Type: "u32"}}
	dtbProperty = binwalk.Struct{{Name: "data_len", Type: "u32"}, {Name: "name_offset", Type: "u32"}}
)

// parseDTBNode is parse_dtb_node. A node that it cannot read has none of
// its flags set.
func parseDTBNode(h dtbHeader, data []byte, off int) dtbNode {
	var n dtbNode
	nd, ok := binwalk.From(data, off)
	if !ok {
		return n
	}
	t, ok := dtbToken.Parse(nd, true)
	if !ok {
		return n
	}
	n.totalSize = dtbToken.Size()
	switch id := t["id"]; {
	case id == fdtEndNode:
		n.end = true
	case id == fdtNOP:
		n.nop = true
	case id == fdtEnd:
		n.eof = true
	case len(nd) <= n.totalSize:
		// The other tokens hold more data.
	case id == fdtBeginNode:
		n.begin = true
		n.name = binwalk.CString(nd[n.totalSize:])
		n.totalSize += dtbAligned(len(n.name) + 1)
	case id == fdtProp:
		p, ok := dtbProperty.Parse(nd[n.totalSize:], true)
		if !ok {
			break
		}
		n.totalSize += dtbProperty.Size()
		pd, ok := binwalk.Range(nd, n.totalSize, n.totalSize+p["data_len"])
		if !ok {
			break
		}
		n.data = pd
		n.totalSize += dtbAligned(len(pd))
		if name, ok := binwalk.From(data, h.stringsOffset+p["name_offset"]); ok {
			n.name = binwalk.CString(name)
			n.property = n.name != ""
		}
	}
	return n
}

// dtbAligned is dtb_aligned: n rounded up to a multiple of 4.
func dtbAligned(n int) int {
	if r := n % 4; r != 0 {
		return n + 4 - r
	}
	return n
}

// extractDTB is extract_dtb. It writes each property to a file named after
// it, in the folders of the nodes that hold it. A node with no name, such as
// the root, adds no folder. The tree must end with its token of end.
func extractDTB(data []byte, off int, out *walk.Output) walk.Extraction {
	h, ok := parseDTBHeader(data[off:])
	if !ok {
		return walk.Extraction{}
	}
	dd, ok := binwalk.Range(data, off, off+h.totalSize)
	if !ok {
		return walk.Extraction{}
	}
	var path []string
	next, previous := h.structOffset, -1
	for binwalk.OffsetSafe(len(dd), next, previous) {
		n := parseDTBNode(h, dd, next)
		switch {
		case n.begin:
			if n.name != "" {
				path = append(path, n.name)
			}
		case n.end:
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
		case n.eof:
			return walk.Extraction{OK: true, Size: len(dd), HasSize: true}
		case n.property:
			if out != nil {
				dir := strings.Join(path, "/")
				if !out.CreateDirectory(dir) || !out.CreateFile(dir+"/"+n.name, n.data) {
					return walk.Extraction{}
				}
			}
		case !n.nop:
			// An unknown or invalid node.
			return walk.Extraction{}
		}
		previous = next
		next += n.totalSize
	}
	return walk.Extraction{}
}
