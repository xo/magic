//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/packimg.rs and
// src/structures/packimg.rs of binwalk.

package firmware

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// PackImg is the signature "packimg" of binwalk.
var PackImg = &walk.Signature{
	Name:        "packimg",
	Magic:       [][]byte{[]byte("--PaCkImGs--")},
	Description: "PackImg firmware header",
	Parse:       parsePackImg,
}

var packimgStruct = binwalk.Struct{
	{Name: "magic_p1", Type: "u32"},
	{Name: "magic_p2", Type: "u32"},
	{Name: "magic_p3", Type: "u32"},
	{Name: "padding1", Type: "u32"},
	{Name: "data_size", Type: "u32"},
}

// parsePackImg is packimg_parser, with parse_packimg_header. binwalk gives
// no confidence, so it is low.
func parsePackImg(data []byte, off int) (walk.Match, error) {
	const headerSize = 32
	h, ok := packimgStruct.Parse(data[off:], false)
	if !ok || len(data)-off < headerSize+h["data_size"] {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        headerSize,
		Confidence:  walk.Low,
		Description: fmt.Sprintf("PackImg firmware header, header size: %d bytes, data size: %d bytes", headerSize, h["data_size"]),
	}, nil
}
