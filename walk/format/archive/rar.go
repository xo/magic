//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/rar.rs and src/structures/rar.rs of
// binwalk. binwalk extracts a RAR archive with unrar. The walker has no
// extractor for it: the decoder of RAR is large, and its license limits its
// use.

package archive

import (
	"bytes"
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// RAR is the signature "rar" of binwalk.
var RAR = &walk.Signature{
	Name:        "rar",
	Magic:       [][]byte{[]byte("Rar!\x1A\x07")},
	Description: "RAR archive",
	Parse:       parseRAR,
}

var rarStruct = binwalk.Struct{
	{Name: "magic_p1", Type: "u32"},
	{Name: "magic_p2", Type: "u16"},
	{Name: "version", Type: "u8"},
}

// rarEOF is the marker of the end of an archive of each version of RAR.
var rarEOF = map[int][]byte{
	4: []byte("\xC4\x3D\x7B\x00\x40\x07\x00"),
	5: []byte("\x1d\x77\x56\x51\x03\x05\x04\x00"),
}

// parseRAR is rar_parser. When it finds no marker of the end, the match
// has no size and a low confidence, and its description says so.
func parseRAR(data []byte, off int) (walk.Match, error) {
	h, ok := rarStruct.Parse(data[off:], false)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	// The byte of the version is 0 for RAR 4 and 1 for RAR 5.
	var version int
	switch h["version"] {
	case 0:
		version = 4
	case 1:
		version = 5
	default:
		return walk.Match{}, walk.ErrNoMatch
	}
	m := walk.Match{Offset: off, Confidence: walk.Low}
	extra := ""
	// get_rar_size takes the first marker after the magic. The size is the
	// end of that marker.
	eof := rarEOF[version]
	if i := bytes.Index(data[off:], eof); i >= 0 {
		m.Size = i + len(eof)
		m.Confidence = walk.Medium
	} else {
		extra = " (failed to locate RAR EOF)"
	}
	m.Description = fmt.Sprintf("RAR archive, version: %d, total size: %d bytes%s", version, m.Size, extra)
	return m, nil
}
