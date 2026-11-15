//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/dpapi.rs and src/structures/dpapi.rs
// of binwalk.

package crypto

import "github.com/xo/magic/walk"

// DPAPI is the signature "dpapi" of binwalk.
var DPAPI = &walk.Signature{
	Name:        "dpapi",
	Short:       true,
	Magic:       [][]byte{[]byte("\x01\x00\x00\x00\xD0\x8c\x9d\xdf\x01\x15\xd1\x11\x8c\x7a\x00\xc0\x4f\xc2\x97\xeb")},
	Description: "DPAPI blob data",
	Parse:       parseDPAPI,
}

// parseDPAPI is dpapi_parser. It never matches, as in binwalk: the parser
// returns an error after it makes its description. Also,
// parse_dpapi_blob_header always fails, because its structure has fields of
// the type "u128", which the structure parser of binwalk does not know.
func parseDPAPI([]byte, int) (walk.Match, error) {
	return walk.Match{}, walk.ErrNoMatch
}
