//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/ecos.rs of binwalk.

package exec

import "github.com/xo/magic/walk"

// ECos is the signature "ecos" of binwalk: the handler of exceptions of the
// eCos kernel, for MIPS only.
var ECos = &walk.Signature{
	Name: "ecos",
	// The first instructions of the handler, in both orders of bytes, with
	// and without the nop that some versions of eCos omit.
	Magic: [][]byte{
		[]byte("\x00\x68\x1A\x40\x00\x00\x00\x00\x7F\x00\x5A\x33"),
		[]byte("\x00\x68\x1A\x40\x7F\x00\x5A\x33"),
		[]byte("\x40\x1A\x68\x00\x00\x00\x00\x00\x33\x5A\x00\x7F"),
		[]byte("\x40\x1A\x68\x00\x33\x5A\x00\x7F"),
	},
	Description: "eCos kernel exception handler",
	Parse:       parseECos,
}

// parseECos is exception_handler_parser.
func parseECos(data []byte, off int) (walk.Match, error) {
	endian := "big"
	if data[off] == 0 {
		endian = "little"
	}
	return walk.Match{
		Offset:      off,
		Confidence:  walk.Low,
		Description: "eCos kernel exception handler, MIPS " + endian + " endian",
	}, nil
}
