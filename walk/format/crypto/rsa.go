//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/rsa.rs of binwalk.

package crypto

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/xo/magic/walk"
)

// RSA is the signature "rsa" of binwalk.
var RSA = &walk.Signature{
	Name:        "rsa",
	Magic:       rsaMagic(),
	Description: "RSA encrypted session key",
	Parse:       parseRSA,
}

// rsaKey is RSAKeyDefinition: the layout of one size of key.
type rsaKey struct {
	magic      []byte
	bits       int
	keyID      int // the offset of the 64-bit ID of the key
	terminator int // the offset of the byte 0xD2
	usage      int // the offset of the byte that tells if the key can sign
	check      int // the offset of the 2 bytes that checks holds
	checks     [][]byte
}

// rsaChecks are the values of the 2 bytes at the check offset of a key whose
// first byte is hi.
func rsaChecks(hi byte) [][]byte {
	out := [][]byte{{hi + 1, 0x00}}
	for lo := byte(0xff); lo >= 0xf9; lo-- {
		out = append(out, []byte{hi, lo})
	}
	return out
}

// rsaKeys is rsa_key_definitions.
var rsaKeys = []rsaKey{
	{[]byte("\x84\x8C\x03"), 1024, 3, 142, 11, 12, rsaChecks(0x03)},
	{[]byte("\x85\x01\x0c\x03"), 2048, 4, 271, 12, 13, rsaChecks(0x07)},
	{[]byte("\x85\x01\x8c\x03"), 3072, 4, 399, 12, 13, rsaChecks(0x0b)},
	{[]byte("\x85\x02\x0c\x03"), 4096, 4, 527, 12, 13, rsaChecks(0x0f)},
	{[]byte("\x85\x04\x0c\x03"), 8192, 4, 1039, 12, 13, rsaChecks(0x1f)},
}

// rsaMagic is rsa_magic.
func rsaMagic() [][]byte {
	out := make([][]byte, 0, len(rsaKeys))
	for _, k := range rsaKeys {
		out = append(out, k.magic)
	}
	return out
}

// parseRSA is rsa_parser. As in binwalk, the first key whose magic matches
// decides: a key that fails its checks ends the search.
func parseRSA(data []byte, off int) (walk.Match, error) {
	for _, k := range rsaKeys {
		if !bytes.HasPrefix(data[off:], k.magic) {
			continue
		}
		m, ok := rsaParseKey(data[off:], k)
		if !ok {
			break
		}
		m.Offset = off
		return m, nil
	}
	return walk.Match{}, walk.ErrNoMatch
}

// rsaParseKey is rsa_key_parser.
func rsaParseKey(data []byte, k rsaKey) (walk.Match, bool) {
	const (
		terminator     = 0xD2
		encryptOnly    = 2
		signAndEncrypt = 1
	)
	n := k.terminator + 1
	if len(data) < n || data[k.terminator] != terminator {
		return walk.Match{}, false
	}
	id := binary.BigEndian.Uint64(data[k.keyID:])
	canSign := data[k.usage] == signAndEncrypt
	canEncrypt := canSign || data[k.usage] == encryptOnly
	if !canEncrypt {
		return walk.Match{}, false
	}
	got := data[k.check : k.check+2]
	for _, c := range k.checks {
		if bytes.Equal(got, c) {
			return walk.Match{
				Size:       n,
				Confidence: walk.Medium,
				Description: fmt.Sprintf("RSA encrypted session key, %d bits, can sign: %t, can encrypt: %t, key ID: 0x%016X",
					k.bits, canSign, canEncrypt, id),
			}, true
		}
	}
	return walk.Match{}, false
}
