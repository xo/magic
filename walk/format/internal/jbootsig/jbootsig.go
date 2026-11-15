//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates parse_jboot_arm_header of src/structures/jboot.rs of
// binwalk.

// Package jbootsig holds the parser of the JBOOT ARM header of binwalk, which
// the signatures jboot_arm and dlke share.
package jbootsig

import "github.com/xo/magic/walk/internal/binwalk"

var armStruct = binwalk.Struct{
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

// ARM is the JBOOT ARM header, JBOOTArmHeader of binwalk.
type ARM struct {
	HeaderSize  int
	DataSize    int
	DataOffset  int
	EraseOffset int
	EraseSize   int
	RomID       string
}

// ParseARM is parse_jboot_arm_header. A ROM ID of 12 bytes comes before the
// fields.
func ParseARM(data []byte) (ARM, bool) {
	const (
		structOffset = 12
		lpvs         = 1
		mbz          = 0
		headerID     = 0x4842
		maxVersion   = 4
	)
	b, ok := binwalk.From(data, structOffset)
	if !ok {
		return ARM{}, false
	}
	h, ok := armStruct.Parse(b, false)
	if !ok {
		return ARM{}, false
	}
	for _, name := range []string{"reserved2", "reserved3", "reserved4", "reserved5", "reserved6", "reserved7", "reserved8"} {
		if h[name] != 0 {
			return ARM{}, false
		}
	}
	if h["lpvs"] != lpvs || h["mbz"] != mbz || h["header_id"] != headerID || h["header_version"] > maxVersion {
		return ARM{}, false
	}
	return ARM{
		HeaderSize:  armStruct.Size() + structOffset,
		RomID:       binwalk.CString(data[:structOffset]),
		DataSize:    h["data_size"],
		DataOffset:  h["data_start"],
		EraseOffset: h["erase_start"],
		EraseSize:   h["erase_size"],
	}, true
}
