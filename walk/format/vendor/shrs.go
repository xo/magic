//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/shrs.rs and src/structures/shrs.rs of
// binwalk.

package vendor

import (
	"encoding/hex"
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// SHRS is the signature "shrs" of binwalk.
var SHRS = &walk.Signature{
	Name:        "shrs",
	Magic:       [][]byte{[]byte("SHRS")},
	Description: "SHRS encrypted firmware",
	Parse:       parseSHRS,
}

var shrsStruct = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "unknown1", Type: "u32"},
	{Name: "encrypted_data_size", Type: "u32"},
}

// parseSHRS is shrs_parser, with parse_shrs_header. An IV of 16 bytes
// follows the structure, and the header is 0x6DC bytes.
func parseSHRS(data []byte, off int) (walk.Match, error) {
	const (
		ivStart    = 12
		ivEnd      = ivStart + 16
		headerSize = 0x6DC
	)
	h, ok := shrsStruct.Parse(data[off:], true)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	iv, ok := binwalk.Range(data[off:], ivStart, ivEnd)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	conf := walk.Low
	if off == 0 {
		conf = walk.Medium
	}
	size := h["encrypted_data_size"]
	return walk.Match{
		Offset:     off,
		Size:       headerSize + size,
		Confidence: conf,
		Description: fmt.Sprintf("SHRS encrypted firmware, header size: %d bytes, encrypted data size: %d bytes, IV: %s",
			headerSize, size, hex.EncodeToString(iv)),
	}, nil
}
