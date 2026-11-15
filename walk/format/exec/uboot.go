//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/uboot.rs of binwalk-ng.

package exec

import (
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// UBoot is the signature "uboot" of binwalk-ng.
var UBoot = &walk.Signature{
	Name:        "uboot",
	Magic:       [][]byte{[]byte("U-Boot ")},
	Description: "U-Boot version string",
	Parse:       parseUBoot,
}

// parseUBoot is uboot_parser.
func parseUBoot(data []byte, off int) (walk.Match, error) {
	const numberOffset = 7
	if p := off + numberOffset; p >= len(data) || !binwalk.IsASCIINumber(data[p]) {
		return walk.Match{}, walk.ErrNoMatch
	}
	version := binwalk.CString(data[off+numberOffset:])
	if version == "" {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        numberOffset + len(version),
		Confidence:  walk.Medium,
		Description: "U-Boot version string: " + binwalk.Truncate(version, 100),
	}, nil
}
