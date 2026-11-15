//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/tplink.rs of binwalk-ng.

package firmware

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// TPLink is the signature "tplink" of binwalk-ng.
var TPLink = &walk.Signature{
	Name:        "tplink",
	Magic:       [][]byte{[]byte("\x01\x00\x00\x00TP-LINK Technologies\x00\x00\x00\x00ver. 1.0")},
	Description: "TP-Link firmware header",
	Parse:       parseTPLink,
}

// TPLinkRTOS is the signature "tplink_rtos" of binwalk-ng.
var TPLinkRTOS = &walk.Signature{
	Name:        "tplink_rtos",
	Magic:       [][]byte{[]byte("\x00\x14\x2F\xC0")},
	Description: "TP-Link RTOS firmware",
	Parse:       parseTPLinkRTOS,
}

var tplinkStruct = binwalk.Struct{
	{Name: "product_id", Type: "u32"},
	{Name: "product_version", Type: "u32"},
	{Name: "reserved1", Type: "u32"},
	{Name: "image_checksum_p1", Type: "u64"},
	{Name: "image_checksum_p2", Type: "u64"},
	{Name: "reserved2", Type: "u32"},
	{Name: "kernel_checksum_p1", Type: "u64"},
	{Name: "kernel_checksum_p2", Type: "u64"},
	{Name: "reserved3", Type: "u32"},
	{Name: "kernel_load_address", Type: "u32"},
	{Name: "kernel_entry_point", Type: "u32"},
	{Name: "image_length", Type: "u32"},
	{Name: "kernel_offset", Type: "u32"},
	{Name: "kernel_length", Type: "u32"},
	{Name: "rootfs_offset", Type: "u32"},
	{Name: "rootfs_length", Type: "u32"},
	{Name: "bootloader_offset", Type: "u32"},
	{Name: "bootloader_length", Type: "u32"},
	{Name: "fw_version_major", Type: "u16"},
	{Name: "fw_version_minor", Type: "u16"},
	{Name: "fw_version_patch", Type: "u16"},
	{Name: "reserved4", Type: "u32"},
}

// parseTPLink is tplink_parser, with parse_tplink_header. The fields start
// 0x40 bytes into a header of 0x200 bytes.
func parseTPLink(data []byte, off int) (walk.Match, error) {
	const (
		structOffset = 0x40
		headerSize   = 0x200
	)
	b := data[off:]
	if len(b) < headerSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	h, ok := tplinkStruct.Parse(b[structOffset:], false)
	if !ok || h["reserved1"] != 0 || h["reserved2"] != 0 || h["reserved3"] != 0 || h["reserved4"] != 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       headerSize,
		Confidence: walk.Medium,
		Description: fmt.Sprintf("TP-Link firmware header, kernel load address: 0x%X, kernel entry point: 0x%X, header size: %d bytes",
			h["kernel_load_address"], h["kernel_entry_point"], headerSize),
	}, nil
}

var tplinkRTOSStruct = binwalk.Struct{
	{Name: "magic1", Type: "u32"},
	{Name: "unknown1", Type: "u64"},
	{Name: "unknown2", Type: "u64"},
	{Name: "magic2", Type: "u32"},
	{Name: "data_size", Type: "u32"},
	{Name: "model_number", Type: "u16"},
	{Name: "hardware_revision_major", Type: "u8"},
	{Name: "hardware_revision_minor", Type: "u8"},
}

// parseTPLinkRTOS is tplink_rtos_parser, with parse_tplink_rtos_header. The
// size is the total size, and the image must fit in the data.
func parseTPLinkRTOS(data []byte, off int) (walk.Match, error) {
	const (
		headerSize      = 0x94
		magic2          = 0x494D4730
		totalSizeOffset = 20
	)
	h, ok := tplinkRTOSStruct.Parse(data[off:], true)
	if !ok || h["magic2"] != magic2 {
		return walk.Match{}, walk.ErrNoMatch
	}
	total := h["data_size"] + totalSizeOffset
	if off+total > len(data) {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       total,
		Confidence: walk.Medium,
		Description: fmt.Sprintf("TP-Link RTOS firmware, model number: %X, hardware version: %X.%X, header size: %d bytes, total size: %d bytes",
			h["model_number"], h["hardware_revision_major"], h["hardware_revision_minor"],
			headerSize, total),
	}, nil
}
