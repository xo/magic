//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/dlke.rs of binwalk-ng. The parser of the
// JBOOT ARM header, which dlke_parser calls, is in walk/format/internal/jbootsig.

package vendor

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/format/internal/jbootsig"
	"github.com/xo/magic/walk/internal/binwalk"
)

// DLKE is the signature "dlke" of binwalk.
var DLKE = &walk.Signature{
	Name: "dlke",
	// The magic is the ROM ID of a JBOOT header.
	Magic:       [][]byte{[]byte("DLK6E8202001"), []byte("DLK6E6110002")},
	Description: "DLK encrypted firmware",
	Parse:       parseDLKE,
}

// parseDLKE is dlke_parser. The first JBOOT header gives the size of the
// signature, and the second, which follows the signature, gives the size of
// the encrypted data.
func parseDLKE(data []byte, off int) (walk.Match, error) {
	sig, ok := jbootsig.ParseARM(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	next, ok := binwalk.From(data, off+sig.HeaderSize+sig.DataSize)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	crypt, ok := jbootsig.ParseARM(next)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       sig.HeaderSize + sig.DataSize + crypt.HeaderSize + crypt.DataSize,
		Confidence: walk.High,
		Description: fmt.Sprintf("DLK encrypted firmware, signature size: %d bytes, encrypted data size: %d bytes",
			sig.DataSize, crypt.DataSize),
	}, nil
}
