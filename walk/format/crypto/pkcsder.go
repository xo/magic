//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/pkcs_der.rs of binwalk.

package crypto

import (
	"bytes"

	"github.com/xo/magic/walk"
)

// PKCSDERHash is the signature "pkcs_der_hash" of binwalk.
var PKCSDERHash = &walk.Signature{
	Name:        "pkcs_der_hash",
	Magic:       pkcsDERMagic(),
	Description: "PKCS DER hash",
	Parse:       parsePKCSDER,
}

// pkcsDERHashes is der_hash_lookups: the DER prefix of each hash. No prefix
// starts another, so the order of the map of binwalk does not matter.
var pkcsDERHashes = []struct {
	name  string
	magic []byte
}{
	{"MD5", []byte("\x30\x20\x30\x0c\x06\x08\x2a\x86\x48\x86\xf7\x0d\x02\x05\x05\x00\x04\x10")},
	{"SHA1", []byte("\x30\x21\x30\x09\x06\x05\x2b\x0e\x03\x02\x1a\x05\x00\x04\x14")},
	{"SHA256", []byte("\x30\x31\x30\x0d\x06\x09\x60\x86\x48\x01\x65\x03\x04\x02\x01\x05\x00\x04\x20")},
	{"SHA384", []byte("\x30\x41\x30\x0d\x06\x09\x60\x86\x48\x01\x65\x03\x04\x02\x02\x05\x00\x04\x30")},
	{"SHA512", []byte("\x30\x51\x30\x0d\x06\x09\x60\x86\x48\x01\x65\x03\x04\x02\x03\x05\x00\x04")},
}

// pkcsDERMagic is der_hash_magic.
func pkcsDERMagic() [][]byte {
	out := make([][]byte, 0, len(pkcsDERHashes))
	for _, h := range pkcsDERHashes {
		out = append(out, h.magic)
	}
	return out
}

// parsePKCSDER is der_hash_parser.
func parsePKCSDER(data []byte, off int) (walk.Match, error) {
	for _, h := range pkcsDERHashes {
		if bytes.HasPrefix(data[off:], h.magic) {
			return walk.Match{
				Offset:      off,
				Size:        len(h.magic),
				Confidence:  walk.Medium,
				Description: "PKCS DER hash, " + h.name,
			}, nil
		}
	}
	return walk.Match{}, walk.ErrNoMatch
}
