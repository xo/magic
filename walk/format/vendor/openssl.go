//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/openssl.rs of binwalk-ng.

package vendor

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// OpenSSL is the signature "openssl" of binwalk.
var OpenSSL = &walk.Signature{
	Name:        "openssl",
	Magic:       [][]byte{[]byte("Salted__")},
	Description: "OpenSSL encryption",
	Parse:       parseOpenSSL,
}

// parseOpenSSL is openssl_crypt_parser.
func parseOpenSSL(data []byte, off int) (walk.Match, error) {
	salt, ok := openSSLHeader(data[off:])
	if !ok || saltInvalid(salt) {
		return walk.Match{}, walk.ErrNoMatch
	}
	conf := walk.Low
	if off == 0 {
		conf = walk.Medium
	}
	return walk.Match{
		Offset:     off,
		Confidence: conf,
		// {:#X} of Rust is "0x" and the digits in upper case.
		Description: fmt.Sprintf("OpenSSL encryption, salt: 0x%X", salt),
	}, nil
}

var openSSLStruct = binwalk.Struct{
	{Name: "magic", Type: "u64"},
	{Name: "salt", Type: "u64"},
}

// openSSLHeader is parse_openssl_crypt_header. It returns the salt, which
// must not be 0.
func openSSLHeader(data []byte) (uint64, bool) {
	h, ok := openSSLStruct.Parse(data, true)
	if !ok || h["salt"] == 0 {
		return 0, false
	}
	return uint64(h["salt"]), true
}

// saltInvalid is is_salt_invalid: true when each byte of the salt is 0 or
// printable ASCII.
func saltInvalid(salt uint64) bool {
	for i := range 8 {
		b := byte(salt >> (8 * i))
		if b != 0 && !binwalk.IsPrintableASCII(b) {
			return false
		}
	}
	return true
}
