//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/android_bootimg.rs and
// src/structures/android_bootimg.rs of binwalk.

package exec

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// AndroidBootImg is the signature "android_bootimg" of binwalk.
var AndroidBootImg = &walk.Signature{
	Name:        "android_bootimg",
	Magic:       [][]byte{[]byte("ANDROID!")},
	Description: "Android boot image",
	Parse:       parseAndroidBootImg,
}

var androidBootImgHeader = binwalk.Struct{
	{Name: "magic", Type: "u64"},
	{Name: "kernel_size", Type: "u32"},
	{Name: "kernel_load_addr", Type: "u32"},
	{Name: "ramdisk_size", Type: "u32"},
	{Name: "ramdisk_load_addr", Type: "u32"},
}

// parseAndroidBootImg is android_bootimg_parser, with
// parse_android_bootimg_header.
func parseAndroidBootImg(data []byte, off int) (walk.Match, error) {
	h, ok := androidBootImgHeader.Parse(data[off:], false)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	conf := walk.Low
	if off == 0 {
		conf = walk.Medium
	}
	return walk.Match{
		Offset:     off,
		Confidence: conf,
		Description: fmt.Sprintf("Android boot image, kernel size: %d bytes, kernel load address: 0x%X, ramdisk size: %d bytes, ramdisk load address: 0x%X",
			h["kernel_size"], h["kernel_load_addr"], h["ramdisk_size"], h["ramdisk_load_addr"]),
	}, nil
}
