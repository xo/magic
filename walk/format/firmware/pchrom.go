//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/pchrom.rs of binwalk-ng.

package firmware

import (
	"encoding/binary"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// PCHROM is the signature "pchrom" of binwalk-ng.
var PCHROM = &walk.Signature{
	Name:        "pchrom",
	Magic:       [][]byte{[]byte("\x5a\xa5\xf0\x0f")},
	Description: "Intel serial flash for PCH ROM",
	Parse:       parsePCHROM,
}

// parsePCHROM is pch_rom_parser. The magic is 16 bytes into the image.
func parsePCHROM(data []byte, off int) (walk.Match, error) {
	const magicOffset = 16
	if off < magicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - magicOffset
	headerSize, dataSize, ok := pchROMHeader(data[start:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      start,
		Size:        headerSize + dataSize,
		Confidence:  walk.Low,
		Description: "Intel serial flash for PCH ROM",
	}, nil
}

var pchROMStruct = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "fcba", Type: "u8"},
	{Name: "nc", Type: "u8"},
	{Name: "frba", Type: "u8"},
	{Name: "nr", Type: "u8"},
}

// pchROMHeader is parse_pchrom_header. It returns the size of the header
// and the size of the regions.
func pchROMHeader(data []byte) (int, int, bool) {
	const (
		structOffset = 16
		expectedFCBA = 3
		expectedFRBA = 4
	)
	b, ok := binwalk.Range(data, structOffset, structOffset+pchROMStruct.Size())
	if !ok {
		return 0, 0, false
	}
	h, ok := pchROMStruct.Parse(b, false)
	if !ok {
		return 0, 0, false
	}
	if h["fcba"] != expectedFCBA || h["frba"] != expectedFRBA || (h["nc"] != 0 && h["nc"] != 1) {
		return 0, 0, false
	}
	size, ok := pchRegionsSize(data, h["fcba"])
	if !ok {
		return 0, 0, false
	}
	return structOffset, size, true
}

// pchRegionsSize is get_pch_regions_size, with an offset of 0. It returns
// the end of the last region that the five entries of regions describe. The
// entries start at fcba shifted left by 4.
func pchRegionsSize(data []byte, fcba int) (int, bool) {
	const (
		regionCount     = 5
		regionEntrySize = 4
	)
	base := fcba << 4
	imageSize := 0
	for i := range regionCount {
		start := base + i*regionEntrySize
		b, ok := binwalk.Range(data, start, start+regionEntrySize)
		if !ok {
			return 0, false
		}
		v := int(binary.LittleEndian.Uint32(b))
		regionBase := (v & 0x1FFF) << 12
		regionLimit := (((v & 0x1FFF0000) >> 4) | 0xFFFF) + 1
		// The base and the limit are separate fields. A base after the
		// limit gives no region.
		if regionLimit > regionBase && regionLimit > imageSize {
			imageSize = regionLimit
		}
	}
	return imageSize, imageSize > 0
}
