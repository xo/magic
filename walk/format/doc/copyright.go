//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/copyright.rs of binwalk.

package doc

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// Copyright is the signature "copyright" of binwalk.
var Copyright = &walk.Signature{
	Name: "copyright",
	Magic: [][]byte{
		[]byte("copyright"),
		[]byte("Copyright"),
		[]byte("COPYRIGHT"),
	},
	Description: "Copyright text",
	Parse:       parseCopyright,
}

// parseCopyright is copyright_parser. The text must be longer than the word
// "copyright", and the description shows its first 100 characters.
func parseCopyright(data []byte, off int) (walk.Match, error) {
	const magicSize = 9
	text := binwalk.CString(data[off:])
	if len(text) <= magicSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        len(text),
		Confidence:  walk.High,
		Description: fmt.Sprintf("Copyright text: \"%s\"", binwalk.Truncate(text, 100)),
	}, nil
}
