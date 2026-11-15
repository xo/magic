//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/gif.rs, src/structures/gif.rs and
// src/extractors/gif.rs of binwalk.

package image

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// GIF is the signature "gif" of binwalk.
var GIF = &walk.Signature{
	Name:        "gif",
	Magic:       [][]byte{[]byte("GIF87a"), []byte("GIF89a")},
	Description: "GIF image",
	Parse:       parseGIF,
	Extract:     extractGIF,
	NoRecurse:   true,
}

// extractGIF is extract_gif_image: it carves the GIF to image.gif.
func extractGIF(data []byte, off int, out *walk.Output) walk.Extraction {
	size, ok := gifSize(data, off)
	if !ok {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: out == nil || out.CarveFile("image.gif", data, off, size), Size: size, HasSize: true}
}

// parseGIF is gif_parser.
func parseGIF(data []byte, off int) (walk.Match, error) {
	size, ok := gifSize(data, off)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	h, ok := gifHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("GIF image, %dx%d pixels, total size: %d bytes", h.width, h.height, size),
		Declined:    off == 0,
	}, nil
}

var gifHeaderStruct = binwalk.Struct{
	{Name: "magic_p1", Type: "u24"},
	{Name: "magic_p2", Type: "u24"},
	{Name: "image_width", Type: "u16"},
	{Name: "image_height", Type: "u16"},
	{Name: "flags", Type: "u8"},
	{Name: "bg_color_index", Type: "u8"},
	{Name: "aspect_ratio", Type: "u8"},
}

type gifHeaderInfo struct {
	size, width, height int
}

// gifHeader is parse_gif_header.
func gifHeader(data []byte) (gifHeaderInfo, bool) {
	h, ok := gifHeaderStruct.Parse(data, false)
	if !ok {
		return gifHeaderInfo{}, false
	}
	return gifHeaderInfo{
		size:   gifHeaderStruct.Size() + gifColorTable(h["flags"]),
		width:  h["image_width"],
		height: h["image_height"],
	}, true
}

// gifColorTable is parse_gif_flags: the size of the color table that the
// flags name.
func gifColorTable(flags int) int {
	if flags&0x80 == 0 {
		return 0
	}
	return 3 * (1 << ((flags & 0b111) + 1))
}

var gifDescriptor = binwalk.Struct{
	{Name: "magic", Type: "u8"},
	{Name: "image_left", Type: "u16"},
	{Name: "image_top", Type: "u16"},
	{Name: "image_width", Type: "u16"},
	{Name: "image_height", Type: "u16"},
	{Name: "flags", Type: "u8"},
}

// gifImageDescriptor is parse_gif_image_descriptor.
func gifImageDescriptor(data []byte) (int, bool) {
	const lzwCodeSize = 1
	h, ok := gifDescriptor.Parse(data, false)
	if !ok {
		return 0, false
	}
	total := gifDescriptor.Size() + gifColorTable(h["flags"]) + lzwCodeSize
	blocks, ok := binwalk.From(data, total)
	if !ok {
		return 0, false
	}
	n, ok := gifSubBlocks(blocks)
	return total + n, ok
}

// gifSubBlocks is parse_gif_sub_blocks: the size of the sub-blocks, each a
// byte of size and its data, up to the terminator.
func gifSubBlocks(data []byte) (int, bool) {
	next, previous := 0, -1
	for binwalk.OffsetSafe(len(data), next, previous) {
		if data[next] == 0 {
			return next + 1, true
		}
		previous = next
		next += int(data[next]) + 1
	}
	return 0, false
}

var gifExtension = binwalk.Struct{
	{Name: "magic", Type: "u8"},
	{Name: "extension_type", Type: "u8"},
	{Name: "sub_block_offset", Type: "u8"},
}

// gifExtensionSize is parse_gif_extension.
func gifExtensionSize(data []byte) (int, bool) {
	const (
		plainText   = 1
		application = 0xFF
		headerSize  = 2
	)
	h, ok := gifExtension.Parse(data, false)
	if !ok {
		return 0, false
	}
	blocksOff := headerSize
	if t := h["extension_type"]; t == application || t == plainText {
		blocksOff += h["sub_block_offset"] + 1
	}
	blocks, ok := binwalk.From(data, blocksOff)
	if !ok {
		return 0, false
	}
	n, ok := gifSubBlocks(blocks)
	return blocksOff + n, ok
}

// gifSize is the dry run of extract_gif_image: the size of the GIF at off.
func gifSize(data []byte, off int) (int, bool) {
	h, ok := gifHeader(data[off:])
	if !ok {
		return 0, false
	}
	image, ok := binwalk.From(data, off+h.size)
	if !ok {
		return 0, false
	}
	n, ok := gifDataSize(image)
	return h.size + n, ok
}

// gifDataSize is get_gif_data_size: the size of the blocks, up to the
// terminator.
func gifDataSize(data []byte) (int, bool) {
	const (
		extension  = 0x21
		terminator = 0x3B
		descriptor = 0x2C
	)
	next, previous := 0, -1
	for binwalk.OffsetSafe(len(data), next, previous) {
		var size int
		var ok bool
		switch data[next] {
		case descriptor:
			size, ok = gifImageDescriptor(data[next:])
		case extension:
			size, ok = gifExtensionSize(data[next:])
		case terminator:
			return next + 1, true
		}
		if !ok {
			break
		}
		previous = next
		next += size
	}
	return 0, false
}
