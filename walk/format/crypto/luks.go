//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/luks.rs of binwalk-ng.

package crypto

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// LUKS is the signature "luks" of binwalk.
var LUKS = &walk.Signature{
	Name:        "luks",
	Magic:       [][]byte{[]byte("LUKS\xBA\xBE")},
	Description: "LUKS header",
	Parse:       parseLUKS,
}

// parseLUKS is luks_parser.
func parseLUKS(data []byte, off int) (walk.Match, error) {
	h, ok := luksHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	m := walk.Match{Offset: off, Confidence: walk.Medium}
	if h.version == 1 {
		m.Description = fmt.Sprintf("LUKS header, version: %d, cipher algorithm: %s, cipher mode: %s, hash fn: %s",
			h.version, h.cipherAlgorithm, h.cipherMode, h.hashFn)
	} else {
		m.Description = fmt.Sprintf("LUKS header, version: %d, header size: %d bytes, hash fn: %s",
			h.version, h.headerSize, h.hashFn)
	}
	return m, nil
}

var luksBase = binwalk.Struct{
	{Name: "magic_1", Type: "u32"},
	{Name: "magic_2", Type: "u16"},
	{Name: "version", Type: "u16"},
	{Name: "header_size", Type: "u64"},
}

// luksInfo is LUKSHeader.
type luksInfo struct {
	version, headerSize                 int
	hashFn, cipherMode, cipherAlgorithm string
}

// luksHeader is parse_luks_header.
func luksHeader(data []byte) (luksInfo, bool) {
	const luks2MinHeaderSize = 4032
	base, ok := luksBase.Parse(data, true)
	if !ok {
		return luksInfo{}, false
	}
	h := luksInfo{version: base["version"]}
	b, ok := binwalk.Range(data, 72, 104)
	if !ok {
		return luksInfo{}, false
	}
	h.hashFn = binwalk.CString(b)
	if h.hashFn == "" {
		return luksInfo{}, false
	}
	switch h.version {
	case 1:
		// The data holds 104 bytes here, so both strings are in it.
		h.cipherAlgorithm = binwalk.CString(data[8:40])
		h.cipherMode = binwalk.CString(data[40:72])
		if h.cipherMode != "" && h.cipherAlgorithm != "" {
			return h, true
		}
	case 2:
		// A u64 above the largest int is negative here. It is larger than
		// the data in binwalk-ng too, so both reject it. The header can
		// fill the data to its end.
		h.headerSize = base["header_size"]
		if h.headerSize > luks2MinHeaderSize && h.headerSize <= len(data) {
			return h, true
		}
	}
	return luksInfo{}, false
}
