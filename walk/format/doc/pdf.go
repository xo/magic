//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/pdf.rs of binwalk.

package doc

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// PDF is the signature "pdf" of binwalk. The magic has a major version of 1.
var PDF = &walk.Signature{
	Name:        "pdf",
	Magic:       [][]byte{[]byte("%PDF-1.")},
	Description: "PDF document",
	Parse:       parsePDF,
}

// parsePDF is pdf_parser. The header is like "%PDF-1.7\n%": the minor
// version, then new lines, then a percent sign.
func parsePDF(data []byte, off int) (walk.Match, error) {
	const (
		minSize       = 16
		minorOffset   = 7
		newlineOffset = 8
	)
	h, ok := binwalk.Range(data, off, off+minSize)
	if !ok || !binwalk.IsASCIINumber(h[minorOffset]) {
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
			Description: fmt.Sprintf("PDF document, version 1.%d", h[minorOffset]-'0'),
		}, nil
	}
	return walk.Match{}, walk.ErrNoMatch
}
