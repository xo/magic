//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/cfe.rs of binwalk.

package firmware

import "github.com/xo/magic/walk"

// CFE is the signature "cfe" of binwalk.
var CFE = &walk.Signature{
	Name:        "cfe",
	Magic:       [][]byte{[]byte("CFE1CFE1")},
	Description: "CFE bootloader",
	Parse:       parseCFE,
}

// parseCFE is cfe_parser. The magic is 28 bytes into the bootloader, and
// binwalk checks nothing else.
func parseCFE(_ []byte, off int) (walk.Match, error) {
	const magicOffset = 28
	if off < magicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	m := walk.Match{
		Offset:      off - magicOffset,
		Confidence:  walk.Low,
		Description: "CFE bootloader",
	}
	// A bootloader at the start of the file is more sure.
	if m.Offset == 0 {
		m.Confidence = walk.Medium
	}
	return m, nil
}
