//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/chk.rs and src/structures/chk.rs of
// binwalk.

package firmware

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// CHK is the signature "chk" of binwalk.
var CHK = &walk.Signature{
	Name:        "chk",
	Magic:       [][]byte{[]byte("\x2A\x23\x24\x5E")},
	Description: "CHK firmware header",
	Parse:       parseCHK,
}

// parseCHK is chk_parser.
func parseCHK(data []byte, off int) (walk.Match, error) {
	h, ok := chkHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	total := h.headerSize + h.kernelSize + h.rootfsSize
	if len(data)-off < total || total <= h.headerSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       h.headerSize,
		Confidence: walk.Medium,
		Description: fmt.Sprintf("CHK firmware header, board ID: %s, header size: %d bytes, data size: %d bytes",
			h.boardID, h.headerSize, h.kernelSize+h.rootfsSize),
	}, nil
}

var chkStruct = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "header_size", Type: "u32"},
	{Name: "unknown", Type: "u64"},
	{Name: "kernel_checksum", Type: "u32"},
	{Name: "rootfs_checksum", Type: "u32"},
	{Name: "rootfs_size", Type: "u32"},
	{Name: "kernel_size", Type: "u32"},
	{Name: "image_checksum", Type: "u32"},
	{Name: "header_checksum", Type: "u32"},
}

type chkInfo struct {
	headerSize int
	kernelSize int
	rootfsSize int
	boardID    string
}

// chkHeader is parse_chk_header. The board ID follows the fixed fields, up
// to the end of the header.
func chkHeader(data []byte) (chkInfo, bool) {
	const maxHeaderSize = 100
	h, ok := chkStruct.Parse(data, true)
	if !ok {
		return chkInfo{}, false
	}
	size := h["header_size"]
	if size <= chkStruct.Size() || size > maxHeaderSize {
		return chkInfo{}, false
	}
	b, ok := binwalk.Range(data, chkStruct.Size(), size)
	if !ok {
		return chkInfo{}, false
	}
	id := binwalk.CString(b)
	if id == "" {
		return chkInfo{}, false
	}
	return chkInfo{
		headerSize: size,
		kernelSize: h["kernel_size"],
		rootfsSize: h["rootfs_size"],
		boardID:    id,
	}, true
}
