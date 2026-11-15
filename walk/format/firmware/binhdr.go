//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/binhdr.rs of binwalk-ng.

package firmware

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// BINHdr is the signature "binhdr" of binwalk-ng.
var BINHdr = &walk.Signature{
	Name:        "binhdr",
	Magic:       [][]byte{[]byte("U2ND")},
	Description: "BIN firmware header",
	Parse:       parseBINHdr,
}

// parseBINHdr is bin_hdr_parser. The magic is 14 bytes into the header.
func parseBINHdr(data []byte, off int) (walk.Match, error) {
	const magicOffset = 14
	if off < magicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - magicOffset
	h, ok := binHeader(data[start:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     start,
		Confidence: walk.Medium,
		Description: fmt.Sprintf("BIN firmware header, board ID: %s, hardware revision: %s, firmware version: %d.%d",
			h.boardID, h.hardware, h.major, h.minor),
	}, nil
}

var binHdrStruct = binwalk.Struct{
	{Name: "reserved1", Type: "u32"},
	{Name: "build_date", Type: "u32"},
	{Name: "firmware_version_major", Type: "u8"},
	{Name: "firmware_version_minor", Type: "u8"},
	{Name: "magic", Type: "u32"},
	{Name: "hardware_id", Type: "u8"},
	{Name: "reserved2", Type: "u24"},
	{Name: "reserved3", Type: "u64"},
}

var binHdrHardware = map[int]string{0: "4702", 1: "4712", 2: "4712L", 3: "4704"}

type binHdrInfo struct {
	boardID  string
	hardware string
	major    int
	minor    int
}

// binHeader is parse_bin_header. A board ID of 4 bytes comes before the
// fields.
func binHeader(data []byte) (binHdrInfo, bool) {
	const structOffset = 4
	b, ok := binwalk.From(data, structOffset)
	if !ok {
		return binHdrInfo{}, false
	}
	h, ok := binHdrStruct.Parse(b, false)
	if !ok || h["reserved1"] != 0 || h["reserved2"] != 0 || h["reserved3"] != 0 {
		return binHdrInfo{}, false
	}
	hw, ok := binHdrHardware[h["hardware_id"]]
	if !ok {
		return binHdrInfo{}, false
	}
	// The board ID has 4 bytes, with no NUL.
	id := binwalk.CString(data[:structOffset])
	if len(id) != structOffset {
		return binHdrInfo{}, false
	}
	return binHdrInfo{
		boardID:  id,
		hardware: hw,
		major:    h["firmware_version_major"],
		minor:    h["firmware_version_minor"],
	}, true
}
