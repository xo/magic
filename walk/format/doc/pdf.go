//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/pdf.rs of binwalk-ng.

package doc

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// PDF is the signature "pdf" of binwalk-ng. The magic has a major version of
// 1 or 2.
var PDF = &walk.Signature{
	Name:        "pdf",
	Magic:       [][]byte{[]byte("%PDF-1."), []byte("%PDF-2.")},
	Description: "PDF document",
	Parse:       parsePDF,
}

// parsePDF is pdf_parser. The header is like "%PDF-1.7\n%": the version,
// then new lines, then a percent sign. The version is 1.0 to 1.9, or 2.0.
func parsePDF(data []byte, off int) (walk.Match, error) {
	const (
		minSize       = 16
		majorOffset   = 5
		minorOffset   = 7
		newlineOffset = 8
	)
	h, ok := binwalk.Range(data, off, off+minSize)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	major, minor := h[majorOffset], h[minorOffset]
	if !(major == '1' && binwalk.IsASCIINumber(minor)) && !(major == '2' && minor == '0') {
		return walk.Match{}, walk.ErrNoMatch
	}
	for _, c := range h[newlineOffset:] {
		if c == '\n' || c == '\r' {
			continue
		}
		if c != '%' {
			break
		}
		return walk.Match{
			Offset:      off,
			Confidence:  walk.Low,
			Description: fmt.Sprintf("PDF document, version %d.%d", major-'0', minor-'0'),
		}, nil
	}
	return walk.Match{}, walk.ErrNoMatch
}
