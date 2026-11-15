//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/rtk.rs and src/structures/rtk.rs of
// binwalk.

package firmware

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// RTK is the signature "rtk" of binwalk. It is short, so the walker tries it
// only at the start of the data.
var RTK = &walk.Signature{
	Name:        "rtk",
	Short:       true,
	Magic:       [][]byte{[]byte("RTK0")},
	Description: "RTK firmware header",
	Parse:       parseRTK,
}

var rtkStruct = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "image_size", Type: "u32"},
	{Name: "checksum", Type: "u32"},
	{Name: "unknown1", Type: "u32"},
	{Name: "header_size", Type: "u32"},
	{Name: "unknown2", Type: "u32"},
	{Name: "unknown3", Type: "u32"},
	{Name: "identifier", Type: "u32"},
}

// parseRTK is rtk_parser, with parse_rtk_header. The image must fill the
// rest of the data.
func parseRTK(data []byte, off int) (walk.Match, error) {
	const magicSize = 4
	h, ok := rtkStruct.Parse(data[off:], false)
	if !ok || h["image_size"] != len(data)-off {
		return walk.Match{}, walk.ErrNoMatch
	}
	headerSize := h["header_size"] + magicSize
	return walk.Match{
		Offset:      off,
		Size:        headerSize,
		Confidence:  walk.Medium,
		Description: fmt.Sprintf("RTK firmware header, header size: %d bytes, image size: %d", headerSize, h["image_size"]),
	}, nil
}
