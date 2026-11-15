//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/dlob.rs and src/structures/dlob.rs of
// binwalk.

package firmware

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// DLOB is the signature "dlob" of binwalk.
var DLOB = &walk.Signature{
	Name:        "dlob",
	Magic:       [][]byte{[]byte("\x5e\xa3\xa4\x17")},
	Description: "DLOB firmware header",
	Parse:       parseDLOB,
}

// parseDLOB is dlob_parser.
func parseDLOB(data []byte, off int) (walk.Match, error) {
	h, ok := dlobHeader(data[off:])
	if !ok || len(data)-off < h.headerSize+h.dataSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset: off,
		// The size is only the header, which is mostly metadata, so that
		// the walk does not skip the data.
		Size:        h.headerSize,
		Confidence:  walk.Medium,
		Description: fmt.Sprintf("DLOB firmware header, header size: %d bytes, data size: %d", h.headerSize, h.dataSize),
	}, nil
}

var dlobStruct1 = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "metadata_size", Type: "u32"},
	{Name: "data_size", Type: "u32"},
}

var dlobStruct2 = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "metadata_size", Type: "u32"},
	{Name: "data_size", Type: "u32"},
	{Name: "unknown", Type: "u64"},
	{Name: "unknown", Type: "u64"},
}

type dlobInfo struct {
	headerSize int
	dataSize   int
}

// dlobHeader is parse_dlob_header. The header has two parts: the first
// holds only metadata, and the second gives the size of the data.
func dlobHeader(data []byte) (dlobInfo, bool) {
	h1, ok := dlobStruct1.Parse(data, true)
	if !ok || h1["data_size"] != 0 {
		return dlobInfo{}, false
	}
	off2 := dlobStruct1.Size() + h1["metadata_size"]
	b, ok := binwalk.From(data, off2)
	if !ok {
		return dlobInfo{}, false
	}
	h2, ok := dlobStruct2.Parse(b, true)
	if !ok || h1["magic"] != h2["magic"] {
		return dlobInfo{}, false
	}
	total := off2 + dlobStruct2.Size() + h2["metadata_size"]
	if total >= h2["data_size"] {
		return dlobInfo{}, false
	}
	return dlobInfo{headerSize: total, dataSize: h2["data_size"]}, true
}
