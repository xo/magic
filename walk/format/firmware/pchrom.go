//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/pchrom.rs and src/structures/pchrom.rs
// of binwalk. binwalk extracts a PCH ROM with uefi-firmware-parser, a
// program that the walker has no translation of, so the walker only finds
// it.

package firmware

import (
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// PCHROM is the signature "pchrom" of binwalk.
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
	{Name: "flmagic", Type: "u32"},
	{Name: "flmap0_fcba", Type: "u8"},
	{Name: "flmap0_nc", Type: "u8"},
	{Name: "flmap0_frba_nr", Type: "u16"},
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
	if h["flmap0_fcba"] != expectedFCBA || h["flmap0_frba_nr"] != expectedFRBA || (h["flmap0_nc"] != 0 && h["flmap0_nc"] != 1) {
		return 0, 0, false
	}
	size, ok := pchRegionsSize(data, h["flmap0_fcba"])
	if !ok {
		return 0, 0, false
	}
	return structOffset, size, true
}

// pchRegionsSize is get_pch_regions_size, with an offset of 0. It returns
// the end of the last region that the five entries of regions describe.
//
// binwalk takes the base of the entries from bits 16 to 23 of fcba, which
// is a field of one byte, so the base is always 0. The fifth entry is then
// the magic, which gives a region that ends at 16 MiB. So binwalk finds a
// PCH ROM only in data of more than 16 MiB.
func pchRegionsSize(data []byte, fcba int) (int, bool) {
	const (
		regionCount     = 5
		regionEntrySize = 4
	)
	base := ((fcba >> 16) & 0xFF) << 4
	entry := binwalk.Struct{{Name: "region_value", Type: "u32"}}
	imageSize := 0
	for i := range regionCount {
		start := base + i*regionEntrySize
		b, ok := binwalk.Range(data, start, start+regionEntrySize)
		if !ok {
			return 0, false
		}
		e, _ := entry.Parse(b, false)
		v := e["region_value"]
		regionBase := (v & 0x1FFF) << 12
		regionLimit := (((v & 0x1FFF0000) >> 4) | 0xFFFF) + 1
		// binwalk subtracts in a usize, which wraps in a release build. The
		// size is then not 0 when the limit is not the base.
		if regionLimit != regionBase && regionLimit > imageSize {
			imageSize = regionLimit
		}
	}
	return imageSize, imageSize > 0
}
