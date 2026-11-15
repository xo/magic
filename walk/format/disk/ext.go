//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/ext.rs and src/structures/ext.rs of
// binwalk. binwalk extracts EXT with tsk_recover of The Sleuth Kit, and
// extractEXT in exttsk.go writes the files that it writes.

package disk

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// EXT is the signature "ext" of binwalk. The magic is only 2 bytes, so each
// pattern adds the possible values of the state and the errors fields, and
// a minor revision of 0.
var EXT = &walk.Signature{
	Name: "ext",
	Magic: [][]byte{
		[]byte("\x53\xEF\x01\x00\x01\x00\x00\x00"),
		[]byte("\x53\xEF\x01\x00\x02\x00\x00\x00"),
		[]byte("\x53\xEF\x01\x00\x03\x00\x00\x00"),
		[]byte("\x53\xEF\x02\x00\x01\x00\x00\x00"),
		[]byte("\x53\xEF\x02\x00\x02\x00\x00\x00"),
		[]byte("\x53\xEF\x02\x00\x03\x00\x00\x00"),
	},
	Description: "EXT filesystem",
	Parse:       parseEXT,
	Extract:     extractEXT,
}

// parseEXT is ext_parser. The description puts the free blocks after "block
// count", the reserved blocks after "free blocks" and the block count after
// "reserved blocks", as binwalk does.
func parseEXT(data []byte, off int) (walk.Match, error) {
	const magicOffset = 1080
	// binwalk subtracts without a check. The usize wraps, and the slice
	// that follows fails.
	if off < magicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - magicOffset
	h, ok := parseEXTHeader(data[start:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     start,
		Size:       h.imageSize,
		Confidence: walk.Medium,
		Description: fmt.Sprintf("EXT filesystem for %s, inodes: %d, block size: %d, block count: %d, free blocks: %d, reserved blocks: %d, total size: %d bytes",
			h.os, h.inodes, h.blockSize, h.freeBlocks, h.reservedBlocks, h.blocks, h.imageSize),
	}, nil
}

type extHeader struct {
	os             string
	blockSize      int
	imageSize      int
	blocks         int
	inodes         int
	freeBlocks     int
	reservedBlocks int
}

var extSuperblock = binwalk.Struct{
	{Name: "inodes_count", Type: "u32"},
	{Name: "blocks_count", Type: "u32"},
	{Name: "reserved_blocks_count", Type: "u32"},
	{Name: "free_blocks_count", Type: "u32"},
	{Name: "free_inodes_count", Type: "u32"},
	{Name: "first_data_block", Type: "u32"},
	{Name: "log_block_size", Type: "u32"},
	{Name: "log_frag_size", Type: "u32"},
	{Name: "blocks_per_group", Type: "u32"},
	{Name: "frags_per_group", Type: "u32"},
	{Name: "inodes_per_group", Type: "u32"},
	{Name: "modification_time", Type: "u32"},
	{Name: "write_time", Type: "u32"},
	{Name: "mount_count", Type: "u16"},
	{Name: "max_mount_count", Type: "u16"},
	{Name: "magic", Type: "u16"},
	{Name: "state", Type: "u16"},
	{Name: "errors", Type: "u16"},
	{Name: "s_minor_rev_level", Type: "u16"},
	{Name: "last_check", Type: "u32"},
	{Name: "check_interval", Type: "u32"},
	{Name: "creator_os", Type: "u32"},
	{Name: "s_rev_level", Type: "u32"},
	{Name: "resuid", Type: "u16"},
	{Name: "resgid", Type: "u16"},
}

var extOS = map[int]string{
	0: "Linux",
	1: "GNU HURD",
	2: "MASIX",
	3: "FreeBSD",
	4: "Lites",
}

// parseEXTHeader is parse_ext_header.
func parseEXTHeader(data []byte) (extHeader, bool) {
	const (
		superblockOffset = 1024
		superblockSize   = 1024
		maxBlockLog      = 2
	)
	if len(data) < superblockOffset+superblockSize {
		return extHeader{}, false
	}
	s, ok := extSuperblock.Parse(data[superblockOffset:], false)
	if !ok {
		return extHeader{}, false
	}
	os, ok := extOS[s["creator_os"]]
	if !ok || s["s_rev_level"] > 1 || s["first_data_block"] > 1 || s["log_block_size"] > maxBlockLog {
		return extHeader{}, false
	}
	blockSize := 1024 << s["log_block_size"]
	return extHeader{
		os:             os,
		blockSize:      blockSize,
		imageSize:      blockSize * s["blocks_count"],
		blocks:         s["blocks_count"],
		inodes:         s["inodes_count"],
		freeBlocks:     s["free_blocks_count"],
		reservedBlocks: s["reserved_blocks_count"],
	}, true
}
