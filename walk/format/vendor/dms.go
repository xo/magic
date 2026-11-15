//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/dms.rs and src/extractors/swapped.rs of
// binwalk-ng.

package vendor

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// DMS is the signature "dms" of binwalk.
var DMS = &walk.Signature{
	Name:        "dms",
	Magic:       [][]byte{[]byte("0><1")},
	Description: "DMS firmware image",
	Parse:       parseDMS,
	Extract:     extractSwappedU16,
}

// extractSwappedU16 is extract_swapped_u16, the extractor of binwalk for
// DMS: it swaps each two bytes with the next two, from off to the end of
// data, and writes them to swapped.bin.
func extractSwappedU16(data []byte, off int, out *walk.Output) walk.Extraction {
	const swapSize = 2
	return extractSwapped(data, off, out, swapSize)
}

// extractSwapped is extract_swapped.
func extractSwapped(data []byte, off int, out *walk.Output, n int) walk.Extraction {
	b, ok := binwalk.From(data, off)
	if !ok {
		return walk.Extraction{}
	}
	swapped := byteSwap(b, n)
	if len(swapped) == 0 {
		return walk.Extraction{}
	}
	return walk.Extraction{
		OK:      out == nil || out.CreateFile("swapped.bin", swapped),
		Size:    len(swapped),
		HasSize: true,
	}
}

// parseDMS is dms_parser. The magic is 4 bytes into the header, and each two
// bytes of the header are swapped with the next two.
func parseDMS(data []byte, off int) (walk.Match, error) {
	const (
		minSize     = 0x100
		swapSize    = 2
		magicOffset = 4
	)
	if off < magicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - magicOffset
	b, ok := binwalk.Range(data, start, start+minSize)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	h, ok := dmsStruct.Parse(byteSwap(b, swapSize), true)
	if !ok || h["magic_p1"] != 0x4D47 || h["magic_p2"] != 0x3C31303E {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := h["image_size"]
	return walk.Match{
		Offset:      start,
		Size:        size,
		Confidence:  walk.Medium,
		Description: fmt.Sprintf("DMS firmware image, total size: %d bytes", size),
	}, nil
}

// dmsStruct is the structure of parse_dms_header.
var dmsStruct = binwalk.Struct{
	{Name: "unknown1", Type: "u16"},
	{Name: "magic_p1", Type: "u16"},
	{Name: "magic_p2", Type: "u32"},
	{Name: "unknown2", Type: "u32"},
	{Name: "image_size", Type: "u32"},
}

// byteSwap is byte_swap of swapped.rs. In each chunk of 2*n bytes, it puts
// the second n bytes before the first n. A short chunk at the end stays as
// it is.
func byteSwap(data []byte, n int) []byte {
	chunk := 2 * n
	out := make([]byte, 0, len(data))
	i := 0
	for ; i+chunk <= len(data); i += chunk {
		out = append(out, data[i+n:i+chunk]...)
		out = append(out, data[i:i+n]...)
	}
	return append(out, data[i:]...)
}
