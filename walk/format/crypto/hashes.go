//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/hashes.rs of binwalk.

package crypto

import (
	"bytes"

	"github.com/xo/magic/walk"
)

// hashMagicLen is HASH_MAGIC_LEN: the size of each magic, and of each match.
const hashMagicLen = 16

// The magics of each hash. The first is big endian, the second little
// endian, as hash_endianess expects.
var (
	crc32Magic = [][]byte{
		[]byte("\x00\x00\x00\x00\x77\x07\x30\x96\xEE\x0E\x61\x2C\x99\x09\x51\xBA"),
		[]byte("\x00\x00\x00\x00\x96\x30\x07\x77\x2C\x61\x0E\xEE\xBA\x51\x09\x99"),
	}
	sha256Magic = [][]byte{
		[]byte("\x42\x8a\x2f\x98\x71\x37\x44\x91\xb5\xc0\xfb\xcf\xe9\xb5\xdb\xa5"),
		[]byte("\x98\x2f\x8a\x42\x91\x44\x37\x71\xcf\xfb\xc0\xb5\xa5\xdb\xb5\xe9"),
	}
	md5Magic = [][]byte{
		[]byte("\xd7\x6a\xa4\x78\xe8\xc7\xb7\x56\x24\x20\x70\xdb\xc1\xbd\xce\xee"),
		[]byte("\x78\xa4\x6a\xd7\x56\xb7\xc7\xe8\xdb\x70\x20\x24\xee\xce\xbd\xc1"),
	}
)

// CRC32 is the signature "crc32" of binwalk.
var CRC32 = &walk.Signature{
	Name:        "crc32",
	Magic:       crc32Magic,
	Description: "CRC32 polynomial table",
	Parse:       hashParser("CRC32 polynomial table", crc32Magic),
}

// SHA256 is the signature "sha256" of binwalk.
var SHA256 = &walk.Signature{
	Name:        "sha256",
	Magic:       sha256Magic,
	Description: "SHA256 hash constants",
	Parse:       hashParser("SHA256 hash constants", sha256Magic),
}

// MD5 is the signature "md5" of binwalk.
var MD5 = &walk.Signature{
	Name:        "md5",
	Magic:       md5Magic,
	Description: "MD5 hash constants",
	Parse:       hashParser("MD5 hash constants", md5Magic),
}

// hashParser returns crc32_parser, sha256_parser or md5_parser: each accepts
// the candidate, and names the byte order of the magic, as hash_endianess
// does.
func hashParser(desc string, magics [][]byte) func([]byte, int) (walk.Match, error) {
	return func(data []byte, off int) (walk.Match, error) {
		endian := "little"
		if bytes.HasPrefix(data[off:], magics[0]) {
			endian = "big"
		}
		return walk.Match{
			Offset:      off,
			Size:        hashMagicLen,
			Confidence:  walk.Low,
			Description: desc + ", " + endian + " endian",
		}, nil
	}
}
