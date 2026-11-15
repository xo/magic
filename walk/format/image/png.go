//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/png.rs of binwalk-ng.

package image

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// PNG is the signature "png" of binwalk.
var PNG = &walk.Signature{
	Name: "png",
	// The magic of PNG, then the size and the type of the IHDR chunk, which
	// comes first and has a fixed size of 13 bytes.
	Magic:       [][]byte{[]byte("\x89PNG\x0D\x0A\x1A\x0A\x00\x00\x00\x0DIHDR")},
	Description: "PNG image",
	Parse:       parsePNG,
	Extract:     extractPNG,
}

// extractPNG is extract_png_image: it carves the PNG to image.png.
func extractPNG(data []byte, off int, out *walk.Output) walk.Extraction {
	size, ok := pngSize(data, off)
	if !ok {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: out == nil || out.CarveFile("image.png", data, off, size), Size: size, HasSize: true}
}

// parsePNG is png_parser.
func parsePNG(data []byte, off int) (walk.Match, error) {
	size, ok := pngSize(data, off)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("PNG image, total size: %d bytes", size),
		Declined:    off == 0,
	}, nil
}

// pngSize is the dry run of extract_png_image: the size of the PNG at off.
func pngSize(data []byte, off int) (int, bool) {
	const headerLen = 8
	chunks, ok := binwalk.From(data, off+headerLen)
	if !ok {
		return 0, false
	}
	n, ok := pngDataSize(chunks)
	// The PNG must end in the data.
	if !ok || off+headerLen+n > len(data) {
		return 0, false
	}
	return n + headerLen, true
}

var pngChunk = binwalk.Struct{{Name: "length", Type: "u32"}, {Name: "type", Type: "u32"}}

// pngDataSize is get_png_data_size: the size of the chunks, up to the end of
// the IEND chunk.
func pngDataSize(data []byte) (int, bool) {
	const (
		crcSize = 4
		iend    = 0x49454E44
	)
	off, previous := 0, -1
	for binwalk.OffsetSafe(len(data), off, previous) {
		// parse_png_chunk_header
		h, ok := pngChunk.Parse(data[off:], true)
		if !ok {
			break
		}
		previous = off
		off += pngChunk.Size() + h["length"] + crcSize
		if h["type"] == iend {
			return off, true
		}
	}
	return 0, false
}
