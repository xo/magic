//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/dlke.rs and parse_jboot_arm_header of
// src/structures/jboot.rs of binwalk.

package vendor

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// DLKE is the signature "dlke" of binwalk.
var DLKE = &walk.Signature{
	Name: "dlke",
	// The magic is the ROM ID of a JBOOT header.
	Magic:       [][]byte{[]byte("DLK6E8202001"), []byte("DLK6E6110002")},
	Description: "DLK encrypted firmware",
	Parse:       parseDLKE,
}

// parseDLKE is dlke_parser. The first JBOOT header gives the size of the
// signature, and the second, which follows the signature, gives the size of
// the encrypted data.
func parseDLKE(data []byte, off int) (walk.Match, error) {
	sigHeader, sigSize, ok := jbootARMHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	next, ok := binwalk.From(data, off+sigHeader+sigSize)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	cryptHeader, cryptSize, ok := jbootARMHeader(next)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       sigHeader + sigSize + cryptHeader + cryptSize,
		Confidence: walk.High,
		Description: fmt.Sprintf("DLK encrypted firmware, signature size: %d bytes, encrypted data size: %d bytes",
			sigSize, cryptSize),
	}, nil
}

var jbootARMStruct = binwalk.Struct{
	{Name: "drange", Type: "u16"},
	{Name: "image_checksum", Type: "u16"},
	{Name: "block_size", Type: "u32"},
	{Name: "reserved2", Type: "u32"},
	{Name: "reserved3", Type: "u16"},
	{Name: "lpvs", Type: "u8"},
	{Name: "mbz", Type: "u8"},
	{Name: "timestamp", Type: "u32"},
	{Name: "erase_start", Type: "u32"},
	{Name: "erase_size", Type: "u32"},
	{Name: "data_start", Type: "u32"},
	{Name: "data_size", Type: "u32"},
	{Name: "reserved4", Type: "u32"},
	{Name: "reserved5", Type: "u32"},
	{Name: "reserved6", Type: "u32"},
	{Name: "reserved7", Type: "u32"},
	{Name: "header_id", Type: "u16"},
	{Name: "header_version", Type: "u16"},
	{Name: "reserved8", Type: "u16"},
	{Name: "section_id", Type: "u8"},
	{Name: "image_info_type", Type: "u8"},
	{Name: "image_info_offset", Type: "u32"},
	{Name: "family", Type: "u16"},
	{Name: "header_checksum", Type: "u16"},
}

// jbootARMHeader is parse_jboot_arm_header of jboot.rs. It returns the size
// of the header and of the data. The structure follows a ROM ID of 12 bytes.
// The package of the JBOOT signatures can change, so this package keeps a
// copy.
func jbootARMHeader(data []byte) (int, int, bool) {
	const romIDSize = 12
	b, ok := binwalk.From(data, romIDSize)
	if !ok {
		return 0, 0, false
	}
	h, ok := jbootARMStruct.Parse(b, false)
	if !ok {
		return 0, 0, false
	}
	for _, f := range []string{"reserved2", "reserved3", "reserved4", "reserved5", "reserved6", "reserved7", "reserved8"} {
		if h[f] != 0 {
			return 0, 0, false
		}
	}
	if h["lpvs"] != 1 || h["mbz"] != 0 || h["header_id"] != 0x4842 || h["header_version"] > 4 {
		return 0, 0, false
	}
	return jbootARMStruct.Size() + romIDSize, h["data_size"], true
}
