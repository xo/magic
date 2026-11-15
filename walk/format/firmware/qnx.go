//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/qnx.rs and src/structures/qnx.rs of
// binwalk. binwalk extracts a QNX IFS image with dumpifs, a program that
// the walker has no translation of, so the walker only finds it.

package firmware

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// QNXIFS is the signature "qnx_ifs" of binwalk.
var QNXIFS = &walk.Signature{
	Name: "qnx_ifs",
	// The magic in little-endian order, and a version of 1.
	Magic:       [][]byte{[]byte("\xEB\x7E\xFF\x00\x01\x00")},
	Description: "QNX IFS image",
	Parse:       parseQNXIFS,
}

// parseQNXIFS is qnx_ifs_parser.
func parseQNXIFS(data []byte, off int) (walk.Match, error) {
	h, ok := qnxIFSStruct.Parse(data[off:], false)
	if !ok || h["flags2"] != 0 || h["zero_0"] != 0 || h["zero_1"] != 0 || h["zero_2"] != 0 || h["zero_3"] != 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := h["stored_size"]
	if size > len(data)-off {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.Low,
		Description: fmt.Sprintf("QNX IFS image, total size: %d bytes", size),
	}, nil
}

// qnxIFSStruct is the header of parse_ifs_header, from sys/startup.h of
// dumpifs.
var qnxIFSStruct = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "version", Type: "u16"},
	{Name: "flags1", Type: "u8"},
	{Name: "flags2", Type: "u8"},
	{Name: "header_size", Type: "u16"},
	{Name: "machine", Type: "u16"},
	{Name: "startup_vaddr", Type: "u32"},
	{Name: "paddr_bias", Type: "u32"},
	{Name: "image_paddr", Type: "u32"},
	{Name: "ram_paddr", Type: "u32"},
	{Name: "ram_size", Type: "u32"},
	{Name: "startup_size", Type: "u32"},
	{Name: "stored_size", Type: "u32"},
	{Name: "imagefs_paddr", Type: "u32"},
	{Name: "imagefs_size", Type: "u32"},
	{Name: "preboot_size", Type: "u16"},
	{Name: "zero_0", Type: "u16"},
	{Name: "zero_1", Type: "u32"},
	{Name: "zero_2", Type: "u32"},
	{Name: "zero_3", Type: "u32"},
}
