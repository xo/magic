//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/rtk.rs of binwalk-ng.

package firmware

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// RTK is the signature "rtk" of binwalk-ng. It is short, so the walker tries it
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

// parseRTK is rtk_parser, with parse_rtk_header. The image must fit in the
// rest of the data. binwalk-ng allows data after it, such as a file system
// or the padding of a flash.
func parseRTK(data []byte, off int) (walk.Match, error) {
	const magicSize = 4
	h, ok := rtkStruct.Parse(data[off:], false)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	headerSize := h["header_size"] + magicSize
	size := h["image_size"]
	if size == 0 || headerSize > size || size > len(data)-off {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        headerSize,
		Confidence:  walk.Medium,
		Description: fmt.Sprintf("RTK firmware header, header size: %d bytes, image size: %d", headerSize, h["image_size"]),
	}, nil
}
