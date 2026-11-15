//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/apfs.rs and src/structures/apfs.rs of
// binwalk.

package disk

import (
	"fmt"
	"slices"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// apfsMagicOffset is the offset of the magic from the start of the image.
const apfsMagicOffset = 0x20

// APFS is the signature "apfs" of binwalk.
var APFS = &walk.Signature{
	Name:        "apfs",
	Magic:       [][]byte{[]byte("NXSB")},
	Description: "APple File System",
	Parse:       parseAPFS,
}

// parseAPFS is apfs_parser. The size wraps as the usize of binwalk does.
func parseAPFS(data []byte, off int) (walk.Match, error) {
	const mbrBlockSize = 512
	if off < apfsMagicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - apfsMagicOffset
	available := uint64(len(data) - start)
	blockSize, blockCount, ok := parseAPFSHeader(data[start:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := blockCount * blockSize
	truncated := ""
	// An APFS in an EFI GPT with a protective MBR counts the block of the
	// MBR in its size. Out of the GPT, the size is 512 bytes too long.
	if size > available && size-available == mbrBlockSize {
		size -= mbrBlockSize
		truncated = fmt.Sprintf(" (truncated by %d bytes)", mbrBlockSize)
	}
	return walk.Match{
		Offset:     start,
		Size:       int(size),
		Confidence: walk.Medium,
		Description: fmt.Sprintf("APple File System, block size: %d bytes, block count: %d, total size: %d bytes%s",
			blockSize, blockCount, size, truncated),
	}, nil
}

var apfsHeader = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "block_size", Type: "u32"},
	{Name: "block_count", Type: "u64"},
	{Name: "nx_features", Type: "u64"},
	{Name: "nx_ro_compat_features", Type: "u64"},
	{Name: "nx_incompat_features", Type: "u64"},
	{Name: "nx_uuid_p1", Type: "u64"},
	{Name: "nx_uuid_p2", Type: "u64"},
	{Name: "nx_next_oid", Type: "u64"},
	{Name: "nx_next_xid", Type: "u64"},
	{Name: "nx_xp_desc_blocks", Type: "u32"},
	{Name: "nx_xp_data_blocks", Type: "u32"},
	{Name: "nx_xp_desc_base", Type: "u64"},
	{Name: "nx_xp_data_base", Type: "u64"},
	{Name: "nx_xp_desc_next", Type: "u32"},
	{Name: "nx_xp_data_next", Type: "u32"},
	{Name: "nx_xp_desc_index", Type: "u32"},
	{Name: "nx_xp_desc_len", Type: "u32"},
	{Name: "nx_xp_data_index", Type: "u32"},
	{Name: "nx_xp_data_len", Type: "u32"},
	{Name: "nx_spaceman_oid", Type: "u64"},
	{Name: "nx_omap_oid", Type: "u64"},
	{Name: "nx_reaper_oid", Type: "u64"},
	{Name: "nx_xp_test_type", Type: "u32"},
	{Name: "nx_xp_max_file_systems", Type: "u32"},
}

// parseAPFSHeader is parse_apfs_header: the block size and the block count.
func parseAPFSHeader(data []byte) (uint64, uint64, bool) {
	const (
		maxFSCount  = 100
		fsBlockSize = 512
	)
	b, ok := binwalk.Range(data, apfsMagicOffset, apfsMagicOffset+apfsHeader.Size())
	if !ok {
		return 0, 0, false
	}
	h, ok := apfsHeader.Parse(b, false)
	if !ok || h["block_size"] == 0 || h["block_count"] == 0 ||
		!slices.Contains([]int{0, 1, 2, 3}, h["nx_features"]) ||
		h["nx_ro_compat_features"] != 0 ||
		!slices.Contains([]int{0, 1, 2, 3, 0x100, 0x101, 0x102, 0x103}, h["nx_incompat_features"]) ||
		h["nx_xp_test_type"] != 0 {
		return 0, 0, false
	}
	// binwalk divides as f32 and rounds up. The f32 is exact for every
	// count up to the maximum, so the division of integers gives the same.
	fsCount := (h["nx_xp_max_file_systems"] + fsBlockSize - 1) / fsBlockSize
	if fsCount == 0 || fsCount > maxFSCount {
		return 0, 0, false
	}
	return u64(h, "block_size"), u64(h, "block_count"), true
}
