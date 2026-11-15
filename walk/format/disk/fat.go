//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/fat.rs of binwalk-ng. binwalk-ng
// extracts FAT with tsk_recover of The Sleuth Kit, from
// src/extractors/tsk.rs, and extractFAT in fattsk.go writes the files that
// it writes.

package disk

import (
	"fmt"
	"slices"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// fatMagicOffset is the offset of the magic from the start of the FAT.
const fatMagicOffset = 0x1FE

// FAT is the signature "fat" of binwalk.
var FAT = &walk.Signature{
	Name:        "fat",
	Short:       true,
	Magic:       [][]byte{[]byte("\x55\xAA")},
	MagicOffset: fatMagicOffset,
	Description: "FAT file system",
	Parse:       parseFAT,
	Extract:     extractFAT,
}

// parseFAT is fat_parser.
func parseFAT(data []byte, off int) (walk.Match, error) {
	if off < fatMagicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - fatMagicOffset
	size, fat32, ok := parseFATHeader(data[start:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	typ := "FAT12/16"
	if fat32 {
		typ = "FAT32"
	}
	return walk.Match{
		Offset:      start,
		Size:        size,
		Confidence:  walk.Medium,
		Description: fmt.Sprintf("FAT file system, type: %s, total size: %d bytes", typ, size),
	}, nil
}

var fatBootSector = binwalk.Struct{
	{Name: "opcode1", Type: "u8"},
	{Name: "opcode2", Type: "u8"},
	{Name: "opcode3", Type: "u8"},
	{Name: "oem_name", Type: "u64"},
	{Name: "bytes_per_sector", Type: "u16"},
	{Name: "sectors_per_cluster", Type: "u8"},
	{Name: "reserved_sectors", Type: "u16"},
	{Name: "fat_count", Type: "u8"},
	{Name: "root_entries_count_16", Type: "u16"},
	{Name: "total_sectors_16", Type: "u16"},
	{Name: "media_type", Type: "u8"},
	{Name: "fat_size_16", Type: "u16"},
	{Name: "sectors_per_track", Type: "u16"},
	{Name: "number_of_heads", Type: "u16"},
	{Name: "hidden_sectors", Type: "u32"},
	{Name: "total_sectors_32", Type: "u32"},
}

// parseFATHeader is parse_fat_header: the total size, and whether the FAT
// is FAT32.
func parseFATHeader(data []byte) (int, bool, bool) {
	h, ok := fatBootSector.Parse(data, false)
	if !ok ||
		!slices.Contains([]int{0xEB, 0xE9}, h["opcode1"]) ||
		!slices.Contains([]int{512, 1024, 2048, 4096}, h["bytes_per_sector"]) ||
		!slices.Contains([]int{1, 2, 4, 8, 16, 32, 64, 128}, h["sectors_per_cluster"]) ||
		h["reserved_sectors"] == 0 ||
		// binwalk-ng accepts one FAT, as flash media can have.
		(h["fat_count"] != 1 && h["fat_count"] != 2) ||
		!slices.Contains([]int{0xF0, 0xF8, 0xF9, 0xFA, 0xFB, 0xFC, 0xFD, 0xFE}, h["media_type"]) {
		return 0, false, false
	}
	sectors := h["total_sectors_32"]
	if h["total_sectors_16"] != 0 {
		sectors = h["total_sectors_16"]
	}
	size := sectors * h["bytes_per_sector"]
	return size, h["fat_size_16"] == 0, size > 0
}
