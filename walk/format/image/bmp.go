//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/bmp.rs of binwalk-ng.

package image

import (
	"encoding/binary"
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// BMP is the signature "bmp" of binwalk.
var BMP = &walk.Signature{
	Name:        "bmp",
	Magic:       [][]byte{[]byte("BM")},
	Description: "BMP image (Bitmap)",
	Parse:       parseBMP,
	Extract:     extractBMP,
	NoRecurse:   true,
}

// extractBMP is extract_bmp_image: it carves the BMP to image.bmp.
func extractBMP(data []byte, off int, out *walk.Output) walk.Extraction {
	size, ok := bmpSize(data, off)
	if !ok {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: out == nil || out.CarveFile("image.bmp", data, off, size), Size: size, HasSize: true}
}

// parseBMP is bmp_parser.
func parseBMP(data []byte, off int) (walk.Match, error) {
	size, ok := bmpSize(data, off)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.Medium,
		Description: fmt.Sprintf("BMP image, total size: %d", size),
	}, nil
}

var bmpHeader = binwalk.Struct{
	{Name: "bfType", Type: "u16"},
	{Name: "bfSize", Type: "u32"},
	{Name: "bfReserved1", Type: "u16"},
	{Name: "bfReserved2", Type: "u16"},
	{Name: "bfOffBits", Type: "u32"},
}

// bmpSize is the dry run of extract_bmp_image: the size of the BMP at off.
func bmpSize(data []byte, off int) (int, bool) {
	const fileHeaderSize = 14
	bmp := data[off:]
	// parse_bmp_file_header
	h, ok := bmpHeader.Parse(bmp, false)
	if !ok || len(bmp) < h["bfSize"] || h["bfSize"] == 0 || h["bfOffBits"] == 0 || h["bfOffBits"] > len(bmp) {
		return 0, false
	}
	// The DIB header follows the file header, and starts with its size.
	dib, ok := binwalk.Range(data, off+fileHeaderSize, off+fileHeaderSize+4)
	if !ok {
		return 0, false
	}
	n := int(binary.LittleEndian.Uint32(dib))
	switch n {
	case 12, 40, 108, 124:
	default:
		return 0, false
	}
	// The size of the file must hold both headers.
	if h["bfSize"] < fileHeaderSize+n {
		return 0, false
	}
	// extract_bmp_image: the image data starts after both headers, and
	// before the end of the file.
	if h["bfOffBits"] < fileHeaderSize+n || h["bfOffBits"] >= h["bfSize"] {
		return 0, false
	}
	return h["bfSize"], true
}
