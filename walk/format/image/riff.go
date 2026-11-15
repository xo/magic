//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/riff.rs of binwalk-ng.

package image

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// RIFF is the signature "riff" of binwalk.
var RIFF = &walk.Signature{
	Name:        "riff",
	Magic:       [][]byte{[]byte("RIFF")},
	Description: "RIFF image",
	Parse:       parseRIFF,
	Extract:     extractRIFF,
	NoRecurse:   true,
}

// extractRIFF is extract_riff_image: it carves a WAVE to video.wav, and any
// other RIFF to image.riff.
func extractRIFF(data []byte, off int, out *walk.Output) walk.Extraction {
	size, typ, ok := riffHeaderInfo(data[off:])
	if !ok || off+size > len(data) {
		return walk.Extraction{}
	}
	name := "image.riff"
	if typ == "WAVE" {
		name = "video.wav"
	}
	return walk.Extraction{OK: out == nil || out.CarveFile(name, data, off, size), Size: size, HasSize: true}
}

// riffHeaderInfo is parse_riff_header: the size and the type of the RIFF.
func riffHeaderInfo(riff []byte) (int, string, bool) {
	h, ok := riffHeader.Parse(riff, false)
	if !ok || h["magic"] != 0x46464952 || !utf8.Valid(riff[8:12]) {
		return 0, "", false
	}
	return h["file_size"] + 8, strings.TrimSpace(string(riff[8:12])), true
}

var riffHeader = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "file_size", Type: "u32"},
	{Name: "chunk_type", Type: "u32"},
}

// parseRIFF is riff_parser, with parse_riff_header.
func parseRIFF(data []byte, off int) (walk.Match, error) {
	size, typ, ok := riffHeaderInfo(data[off:])
	// The RIFF must end in the data.
	if !ok || off+size > len(data) {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.Medium,
		Description: fmt.Sprintf("RIFF image, encoding type: %s, total size: %d bytes", typ, size),
		Declined:    off == 0 && size == len(data),
	}, nil
}
