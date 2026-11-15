//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/pjl.rs of binwalk-ng.

package doc

import (
	"fmt"
	"strings"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// PJL is the signature "pjl" of binwalk.
var PJL = &walk.Signature{
	Name:        "pjl",
	Magic:       [][]byte{[]byte("\x1B%-12345X@PJL")},
	Description: "HP Printer Job Language data",
	Parse:       parsePJL,
}

// parsePJL is pjl_parser. The size runs from the magic to the first NUL after
// the first "@PJL".
func parsePJL(data []byte, off int) (walk.Match, error) {
	const commandsOffset = 9
	b, ok := binwalk.From(data, off+commandsOffset)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	text := binwalk.CString(b)
	if len(text) == 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	// binwalk puts a space for each carriage return, and removes each new
	// line.
	shown := strings.ReplaceAll(strings.ReplaceAll(text, "\r", " "), "\n", "")
	return walk.Match{
		Offset:      off,
		Size:        commandsOffset + len(text),
		Confidence:  walk.Low,
		Description: fmt.Sprintf("HP Printer Job Language data: \"%s\"", shown),
	}, nil
}
