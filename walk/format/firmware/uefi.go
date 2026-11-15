//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/uefi.rs of binwalk-ng.

package firmware

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// UEFIVolume is the signature "uefi_pi_volume" of binwalk-ng.
var UEFIVolume = &walk.Signature{
	Name:        "uefi_pi_volume",
	Magic:       [][]byte{[]byte("_FVH")},
	Description: "UEFI PI firmware volume",
	Parse:       parseUEFIVolume,
	NoRecurse:   true,
}

// UEFICapsule is the signature "uefi_capsule" of binwalk-ng.
var UEFICapsule = &walk.Signature{
	Name: "uefi_capsule",
	// The GUIDs of an EFI capsule, an EFI2 capsule and a UEFI capsule.
	Magic: [][]byte{
		[]byte("\xBD\x86\x66\x3B\x76\x0D\x30\x40\xB7\x0E\xB5\x51\x9E\x2F\xC5\xA0"),
		[]byte("\x8B\xA6\x3C\x4A\x23\x77\xFB\x48\x80\x3D\x57\x8C\xC1\xFE\xC4\x4D"),
		[]byte("\xB9\x82\x91\x53\xB5\xAB\x91\x43\xB6\x9A\xE3\xA9\x43\xF7\x2F\xCC"),
	},
	Description: "UEFI capsule image",
	Parse:       parseUEFICapsule,
	NoRecurse:   true,
}

// parseUEFIVolume is uefi_volume_parser. The magic is 40 bytes into the
// volume.
func parseUEFIVolume(data []byte, off int) (walk.Match, error) {
	const magicOffset = 40
	if off < magicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - magicOffset
	h, ok := uefiVolumeHeader(data[start:])
	// A volume size of 2^63 or more is negative here. binwalk adds it to
	// the offset in a usize that can wrap, and then drops the match because
	// its size is past the end of the data. The walker rejects it here.
	if !ok || h.volumeSize < 0 || len(data) < start+h.volumeSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     start,
		Size:       h.volumeSize,
		Confidence: walk.Medium,
		Description: fmt.Sprintf("UEFI PI firmware volume, header CRC: 0x%X, header size: %d bytes, total size: %d bytes",
			h.headerCRC, h.headerSize, h.volumeSize),
	}, nil
}

// uefiVolumeStruct is the header of binwalk. binwalk reads it from the
// start of the volume, so volume_size is the first 8 bytes of the zero
// vector, and not the length of the volume, which is 32 bytes into it. The
// walker reads it as binwalk does.
var uefiVolumeStruct = binwalk.Struct{
	{Name: "volume_size", Type: "u64"},
	{Name: "magic", Type: "u32"},
	{Name: "attributes", Type: "u32"},
	{Name: "header_size", Type: "u16"},
	{Name: "header_crc", Type: "u16"},
	{Name: "extended_header_offset", Type: "u16"},
	{Name: "reserved", Type: "u8"},
	{Name: "revision", Type: "u8"},
}

type uefiVolumeInfo struct {
	headerCRC  int
	headerSize int
	volumeSize int
}

// uefiVolumeHeader is parse_uefi_volume_header.
func uefiVolumeHeader(data []byte) (uefiVolumeInfo, bool) {
	h, ok := uefiVolumeStruct.Parse(data, false)
	if !ok {
		return uefiVolumeInfo{}, false
	}
	// binwalk compares in a usize. A negative volume size here is above
	// every header size there.
	sizeOK := h["volume_size"] < 0 || h["header_size"] < h["volume_size"]
	if !sizeOK || h["reserved"] != 0 || (h["revision"] != 1 && h["revision"] != 2) {
		return uefiVolumeInfo{}, false
	}
	return uefiVolumeInfo{
		headerCRC:  h["header_crc"],
		headerSize: h["header_size"],
		volumeSize: h["volume_size"],
	}, true
}

// parseUEFICapsule is uefi_capsule_parser.
func parseUEFICapsule(data []byte, off int) (walk.Match, error) {
	h, ok := uefiCapsuleStruct.Parse(data[off:], false)
	if !ok || h["header_size"] >= h["total_size"] {
		return walk.Match{}, walk.ErrNoMatch
	}
	// The capsule must fit in the rest of the data.
	if h["total_size"] == 0 || h["total_size"] > len(data)-off {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       h["total_size"],
		Confidence: walk.Medium,
		Description: fmt.Sprintf("UEFI capsule image, header size: %d bytes, total size: %d bytes",
			h["header_size"], h["total_size"]),
	}, nil
}

// uefiCapsuleStruct is the header of parse_uefi_capsule_header.
var uefiCapsuleStruct = binwalk.Struct{
	{Name: "guid_p1", Type: "u64"},
	{Name: "guid_p2", Type: "u64"},
	{Name: "header_size", Type: "u32"},
	{Name: "flags", Type: "u32"},
	{Name: "total_size", Type: "u32"},
}
