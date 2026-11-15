//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/btrfs.rs of binwalk-ng.

package disk

import (
	"fmt"
	"hash/crc32"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// BTRFS is the signature "btrfs" of binwalk.
var BTRFS = &walk.Signature{
	Name:        "btrfs",
	Magic:       [][]byte{[]byte("_BHRfS_M")},
	Description: "BTRFS file system",
	Parse:       parseBTRFS,
}

// parseBTRFS is btrfs_parser.
func parseBTRFS(data []byte, off int) (walk.Match, error) {
	const magicOffset = 0x10040
	if off < magicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - magicOffset
	h, ok := parseBTRFSHeader(data[start:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := u64(h, "total_bytes")
	return walk.Match{
		Offset:     start,
		Size:       int(size),
		Confidence: walk.Medium,
		Description: fmt.Sprintf("BTRFS file system, node size: %d, sector size: %d, leaf size: %d, stripe size: %d, bytes used: %d, total size: %d bytes",
			h["node_size"], h["sector_size"], h["leaf_size"], h["stripe_size"], u64(h, "bytes_used"), size),
	}, nil
}

var btrfsSuperblock = binwalk.Struct{
	{Name: "header_checksum", Type: "u32"},
	{Name: "unused1", Type: "u32"},
	{Name: "unused2", Type: "u64"},
	{Name: "unused3", Type: "u64"},
	{Name: "unused4", Type: "u64"},
	{Name: "uuid_p1", Type: "u64"},
	{Name: "uuid_p2", Type: "u64"},
	{Name: "block_phys_addr", Type: "u64"},
	{Name: "flags", Type: "u64"},
	{Name: "magic", Type: "u64"},
	{Name: "generation", Type: "u64"},
	{Name: "root_tree_address", Type: "u64"},
	{Name: "chunk_tree_address", Type: "u64"},
	{Name: "log_tree_address", Type: "u64"},
	{Name: "log_root_transid", Type: "u64"},
	{Name: "total_bytes", Type: "u64"},
	{Name: "bytes_used", Type: "u64"},
	{Name: "root_dir_objid", Type: "u64"},
	{Name: "num_devices", Type: "u64"},
	{Name: "sector_size", Type: "u32"},
	{Name: "node_size", Type: "u32"},
	{Name: "leaf_size", Type: "u32"},
	{Name: "stripe_size", Type: "u32"},
}

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// parseBTRFSHeader is parse_btrfs_header. It checks the CRC-32C of the
// superblock.
func parseBTRFSHeader(data []byte) (binwalk.Fields, bool) {
	const (
		superblockOffset = 0x10000
		superblockEnd    = superblockOffset + 0x1000
		crcStart         = 0x20
	)
	sb, ok := binwalk.Range(data, superblockOffset, superblockEnd)
	if !ok {
		return nil, false
	}
	h, ok := btrfsSuperblock.Parse(sb, false)
	if !ok || uint32(h["header_checksum"]) != crc32.Checksum(sb[crcStart:], castagnoli) {
		return nil, false
	}
	return h, true
}
