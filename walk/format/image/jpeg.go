//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/jpeg.rs of binwalk-ng.

package image

import (
	"fmt"
	"slices"

	"github.com/xo/magic/walk"
)

// JPEG is the signature "jpeg" of binwalk.
var JPEG = &walk.Signature{
	Name: "jpeg",
	Magic: [][]byte{
		[]byte("\xFF\xD8\xFF\xE0\x00\x10JFIF\x00"),
		[]byte("\xFF\xD8\xFF\xE1"),
		[]byte("\xFF\xD8\xFF\xE2"),
		[]byte("\xFF\xD8\xFF\xDB"),
		[]byte("\xFF\xD8\xFF\xC0"),
		[]byte("\xFF\xD8\xFF\xFE"),
	},
	Description: "JPEG image",
	Parse:       parseJPEG,
	Extract:     extractJPEG,
	NoRecurse:   true,
}

// extractJPEG is extract_jpeg_image: it carves the JPEG to image.jpg.
func extractJPEG(data []byte, off int, out *walk.Output) walk.Extraction {
	size, ok := jpegSize(data[off:])
	if !ok {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: out == nil || out.CarveFile("image.jpg", data, off, size), Size: size, HasSize: true}
}

// parseJPEG is jpeg_parser.
func parseJPEG(data []byte, off int) (walk.Match, error) {
	size, ok := jpegSize(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.Medium,
		Description: fmt.Sprintf("JPEG image, total size: %d bytes", size),
		Declined:    off == 0 && size == len(data),
	}, nil
}

// The markers that have no field of size, and the bytes after 0xFF that do
// not end the data of a Start Of Scan.
var (
	jpegNoLength = []byte{0x00, 0x01, 0xD0, 0xD1, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7, 0xD8, 0xD9}
	jpegSOSSkip  = []byte{0x00, 0xD0, 0xD1, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7}
)

// jpegSize is get_jpeg_data_size: the size of the JPEG up to its EOI marker.
func jpegSize(data []byte) (int, bool) {
	const (
		marker = 0xFF
		sos    = 0xDA
		eoi    = 0xD9
	)
	next := 0
	for {
		if next >= len(data) || data[next] != marker {
			return 0, false
		}
		next++
		if next >= len(data) {
			return 0, false
		}
		id := data[next]
		next++
		if !slices.Contains(jpegNoLength, id) {
			if next+2 > len(data) {
				return 0, false
			}
			next += int(data[next])<<8 | int(data[next+1])
		}
		// The data of a Start Of Scan follows its header, and is not in its
		// size. It ends at the next marker.
		if id == sos {
			for next+2 <= len(data) {
				if data[next] == marker && !slices.Contains(jpegSOSSkip, data[next+1]) {
					break
				}
				next++
			}
		}
		if id == eoi {
			return next, true
		}
	}
}
