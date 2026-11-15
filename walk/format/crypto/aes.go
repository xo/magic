//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/aes.rs of binwalk.

package crypto

import "github.com/xo/magic/walk"

// AESSBox is the signature "aes_sbox" of binwalk.
var AESSBox = &walk.Signature{
	Name: "aes_sbox",
	Magic: [][]byte{
		[]byte("\x63\x7C\x77\x7B\xF2\x6B\x6F\xC5"), // the forward S-box
		[]byte("\x52\x09\x6A\xD5\x30\x36\xA5\x38"), // the reverse S-box
	},
	Description: "AES S-Box",
	Parse:       aesParser("AES S-Box"),
}

// AESForwardTable is the signature "aes_forward_table" of binwalk.
var AESForwardTable = &walk.Signature{
	Name:        "aes_forward_table",
	Magic:       [][]byte{[]byte("\xC6\x63\x63\xA5\xF8\x7C\x7C\x84\xEE\x77\x77\x99\xF6\x7B\x7B\x8D")},
	Description: "AES Forward Table",
	Parse:       aesParser("AES Forward Table"),
}

// AESReverseTable is the signature "aes_reverse_table" of binwalk.
var AESReverseTable = &walk.Signature{
	Name:        "aes_reverse_table",
	Magic:       [][]byte{[]byte("\x51\xF4\xA7\x50\x7E\x41\x65\x53\x1A\x17\xA4\xC3\x3A\x27\x5E\x96")},
	Description: "AES Reverse Table",
	Parse:       aesParser("AES Reverse Table"),
}

// AESRCON is the signature "aes_rcon" of binwalk.
var AESRCON = &walk.Signature{
	Name: "aes_rcon",
	Magic: [][]byte{
		[]byte("\x01\x02\x04\x08\x10\x20\x40\x80\x1B\x36"),
		[]byte("\x01\x00\x00\x00\x02\x00\x00\x00\x04\x00\x00\x00\x08\x00\x00\x00\x10\x00\x00\x00\x20\x00\x00\x00\x40\x00\x00\x00\x80\x00\x00\x00\x1B\x00\x00\x00\x36\x00\x00\x00"),
	},
	Description: "AES RCON",
	Parse:       aesParser("AES RCON"),
}

// AESAccelerationTable is the signature "aes_acceleration_table" of binwalk.
var AESAccelerationTable = &walk.Signature{
	Name: "aes_acceleration_table",
	Magic: [][]byte{
		[]byte("\xA5\x84\x99\x8D\x0D\xBD\xB1\x54\x50\x03\xA9\x7D\x19\x62\xE6\x9A"),                                 // the S-box times 2
		[]byte("\xC6\xF8\xEE\xF6\xFF\xD6\xDE\x91\x60\x02\xCE\x56\xE7\xB5\x4D\xEC"),                                 // the S-box times 3
		[]byte("\x00\x02\x04\x06\x08\x0a\x0c\x0e\x10\x12\x14\x16\x18\x1a\x1c\x1e\x20\x22\x24\x26\x28\x2a\x2c\x2e"), // Galois multiply by 2
		[]byte("\x00\x03\x06\x05\x0c\x0f\x0a\x09\x18\x1b\x1e\x1d\x14\x17\x12\x11\x30\x33\x36\x35\x3c\x3f\x3a\x39"), // Galois multiply by 3
		[]byte("\x00\x09\x12\x1b\x24\x2d\x36\x3f\x48\x41\x5a\x53\x6c\x65\x7e\x77\x90\x99\x82\x8b\xb4\xbd\xa6\xaf"), // Galois multiply by 9
		[]byte("\x00\x0b\x16\x1d\x2c\x27\x3a\x31\x58\x53\x4e\x45\x74\x7f\x62\x69\xb0\xbb\xa6\xad\x9c\x97\x8a\x81"), // Galois multiply by 11
		[]byte("\x00\x0d\x1a\x17\x34\x39\x2e\x23\x68\x65\x72\x7f\x5c\x51\x46\x4b\xd0\xdd\xca\xc7\xe4\xe9\xfe\xf3"), // Galois multiply by 13
		[]byte("\x00\x0e\x1c\x12\x38\x36\x24\x2a\x70\x7e\x6c\x62\x48\x46\x54\x5a\xe0\xee\xfc\xf2\xd8\xd6\xc4\xca"), // Galois multiply by 14
	},
	Description: "AES Acceleration Table",
	Parse:       aesParser("AES Acceleration Table"),
}

// aesParser returns a parser that accepts each candidate, with no size, as
// aes_sbox_parser and the other parsers of aes.rs do.
func aesParser(desc string) func([]byte, int) (walk.Match, error) {
	return func(_ []byte, off int) (walk.Match, error) {
		return walk.Match{Offset: off, Confidence: walk.Low, Description: desc}, nil
	}
}
