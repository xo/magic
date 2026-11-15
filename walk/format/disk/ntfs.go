//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/ntfs.rs and src/structures/ntfs.rs of
// binwalk.

package disk

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// NTFS is the signature "ntfs" of binwalk.
var NTFS = &walk.Signature{
	Name:        "ntfs",
	Magic:       [][]byte{[]byte("\xEB\x52\x90NTFS\x20\x20\x20\x20")},
	Description: "NTFS partition",
	Parse:       parseNTFS,
}

var ntfsHeader = binwalk.Struct{
	{Name: "opcodes", Type: "u24"},
	{Name: "magic", Type: "u64"},
	{Name: "bytes_per_sector", Type: "u16"},
	{Name: "sectors_per_cluster", Type: "u8"},
	{Name: "unused1", Type: "u16"},
	{Name: "unused2", Type: "u24"},
	{Name: "unused3", Type: "u16"},
	{Name: "media_type", Type: "u8"},
	{Name: "unused4", Type: "u16"},
	{Name: "sectors_per_track", Type: "u16"},
	{Name: "head_count", Type: "u16"},
	{Name: "hidden_sector_count", Type: "u32"},
	{Name: "unused5", Type: "u32"},
	{Name: "unknown", Type: "u32"},
	{Name: "sector_count", Type: "u64"},
}

// parseNTFS is ntfs_parser, with parse_ntfs_header. The size wraps as the
// usize of binwalk does.
func parseNTFS(data []byte, off int) (walk.Match, error) {
	h, ok := ntfsHeader.Parse(data[off:], false)
	if !ok || h["unused1"] != 0 || h["unused2"] != 0 || h["unused3"] != 0 || h["unused4"] != 0 || h["unused5"] != 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	sectorSize, sectorCount := u64(h, "bytes_per_sector"), u64(h, "sector_count")
	// The sector count leaves out the boot sector.
	size := sectorSize * (sectorCount + 1)
	if size <= sectorSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       int(size),
		Confidence: walk.Medium,
		Description: fmt.Sprintf("NTFS partition, number of sectors: %d, bytes per sector: %d, total size: %d bytes",
			sectorCount, sectorSize, size),
	}, nil
}
