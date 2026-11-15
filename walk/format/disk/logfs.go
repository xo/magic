//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/logfs.rs and src/structures/logfs.rs
// of binwalk.

package disk

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// logFSMagicOffset is the offset of the magic from the start of the file
// system.
const logFSMagicOffset = 0x18

// LogFS is the signature "logfs" of binwalk.
var LogFS = &walk.Signature{
	Name:        "logfs",
	Magic:       [][]byte{[]byte("\x7A\x3A\x8E\x5C\xB9\xD5\xBF\x67")},
	Description: "LogFS file system",
	Parse:       parseLogFS,
}

var logFSSuperblock = binwalk.Struct{
	{Name: "magic", Type: "u64"},
	{Name: "crc32", Type: "u32"},
	{Name: "ifile_levels", Type: "u8"},
	{Name: "iblock_levels", Type: "u8"},
	{Name: "data_levels", Type: "u8"},
	{Name: "segment_shift", Type: "u8"},
	{Name: "block_shift", Type: "u8"},
	{Name: "write_shift", Type: "u8"},
	{Name: "pad0", Type: "u32"},
	{Name: "pad1", Type: "u16"},
	{Name: "filesystem_size", Type: "u64"},
	{Name: "segment_size", Type: "u32"},
	{Name: "bad_seg_reserved", Type: "u32"},
	{Name: "feature_incompat", Type: "u64"},
	{Name: "feature_ro_compat", Type: "u64"},
	{Name: "feature_compat", Type: "u64"},
	{Name: "feature_flags", Type: "u64"},
	{Name: "root_reserve", Type: "u64"},
	{Name: "speed_reserve", Type: "u64"},
}

// parseLogFS is logfs_parser, with parse_logfs_super_block.
func parseLogFS(data []byte, off int) (walk.Match, error) {
	if off < logFSMagicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - logFSMagicOffset
	h, ok := logFSSuperblock.Parse(data[off:], true)
	if !ok || h["pad0"] != 0 || h["pad1"] != 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := u64(h, "filesystem_size")
	return walk.Match{
		Offset:      start,
		Size:        int(size),
		Confidence:  walk.Medium,
		Description: fmt.Sprintf("LogFS file system, total size: %d bytes", size),
	}, nil
}
