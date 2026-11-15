//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates the parser of src/formats/ntfs.rs of binwalk-ng.

package disk

import (
	"fmt"
	"math"
	"math/bits"
	"slices"

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

// parseNTFS is ntfs_parser, with parse_ntfs_header. binwalk-ng needs a
// sector size of the hardware, rejects a size that overflows, and needs the
// image to end inside the data.
func parseNTFS(data []byte, off int) (walk.Match, error) {
	h, ok := ntfsHeader.Parse(data[off:], false)
	if !ok || h["unused1"] != 0 || h["unused2"] != 0 || h["unused3"] != 0 || h["unused4"] != 0 || h["unused5"] != 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	sectorSize, sectorCount := u64(h, "bytes_per_sector"), u64(h, "sector_count")
	if !slices.Contains([]uint64{512, 1024, 2048, 4096}, sectorSize) || sectorCount == math.MaxUint64 {
		return walk.Match{}, walk.ErrNoMatch
	}
	// The sector count leaves out the boot sector.
	hi, size := bits.Mul64(sectorSize, sectorCount+1)
	if hi != 0 || size <= sectorSize || size > uint64(len(data)-off) {
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
