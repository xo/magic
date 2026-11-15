//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/dkbs.rs and src/structures/dkbs.rs of
// binwalk.

package vendor

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// DKBS is the signature "dkbs" of binwalk.
var DKBS = &walk.Signature{
	Name:        "dkbs",
	Magic:       [][]byte{[]byte("_dkbs_")},
	Description: "DKBS firmware header",
	Parse:       parseDKBS,
}

// parseDKBS is dkbs_parser. The magic is 7 bytes into the header.
func parseDKBS(data []byte, off int) (walk.Match, error) {
	const magicOffset = 7
	if off < magicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - magicOffset
	h, ok := dkbsHeader(data[start:])
	if !ok || len(data)-start < h.headerSize+h.dataSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	conf := walk.Medium
	if start == 0 {
		conf = walk.High
	}
	return walk.Match{
		Offset:     start,
		Size:       h.headerSize,
		Confidence: conf,
		Description: fmt.Sprintf("DKBS firmware header, board ID: %s, firmware version: %s, boot device: %s, endianness: %s, header size: %d bytes, data size: %d",
			h.board, h.version, h.boot, h.endianness, h.headerSize, h.dataSize),
	}, nil
}

var dkbsSizeStruct = binwalk.Struct{{Name: "size", Type: "u32"}}

type dkbsInfo struct {
	board, version, boot, endianness string
	headerSize, dataSize             int
}

// dkbsHeader is parse_dkbs_header. The size of the data is big-endian when
// its high byte is 0, and little-endian when it is not.
func dkbsHeader(data []byte) (dkbsInfo, bool) {
	const headerSize = 0xA0
	if len(data) < headerSize {
		return dkbsInfo{}, false
	}
	r := dkbsInfo{
		headerSize: headerSize,
		version:    binwalk.CString(data[0x28:0x48]),
		board:      binwalk.CString(data[0:0x20]),
		boot:       binwalk.CString(data[0x70:0x90]),
	}
	if r.version == "" || r.board == "" || r.boot == "" {
		return dkbsInfo{}, false
	}
	b := data[0x68:0x6C]
	if f, _ := dkbsSizeStruct.Parse(b, true); f["size"]&0xFF000000 == 0 {
		r.dataSize, r.endianness = f["size"], "big"
	} else {
		f, _ = dkbsSizeStruct.Parse(b, false)
		r.dataSize, r.endianness = f["size"], "little"
	}
	if r.dataSize == 0 {
		return dkbsInfo{}, false
	}
	return r, true
}
