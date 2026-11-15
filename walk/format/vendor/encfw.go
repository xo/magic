//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates the signatures of src/formats/encfw.rs and
// src/formats/encrpted_img.rs of binwalk-ng.

package vendor

import (
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// encfwKnown is encfw_known_firmware: the magic of each known encrypted
// firmware, and its make and model.
var encfwKnown = map[string]string{
	"\xdf\x8c\x39\x0d": "D-Link DIR-822 rev C",
	"\x35\x66\x6f\x68": "D-Link DAP-1665",
	"\xf5\x2a\xa0\xb4": "D-Link DIR-842 rev C",
	"\xe3\x13\x00\x5b": "D-Link DIR-850 rev A",
	"\x0a\x14\xe4\x24": "D-Link DIR-850 rev B",
}

// EncFW is the signature "encfw" of binwalk.
var EncFW = &walk.Signature{
	Name:  "encfw",
	Short: true,
	Magic: [][]byte{
		[]byte("\xdf\x8c\x39\x0d"),
		[]byte("\x35\x66\x6f\x68"),
		[]byte("\xf5\x2a\xa0\xb4"),
		[]byte("\xe3\x13\x00\x5b"),
		[]byte("\x0a\x14\xe4\x24"),
	},
	Description: "Known encrypted firmware",
	Parse:       parseEncFW,
}

// parseEncFW is encfw_parser.
func parseEncFW(data []byte, off int) (walk.Match, error) {
	const magicLen = 4
	b, ok := binwalk.Range(data, off, off+magicLen)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	name, ok := encfwKnown[string(b)]
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	conf := walk.Medium
	if off != 0 {
		conf = walk.Low
	}
	return walk.Match{
		Offset:      off,
		Confidence:  conf,
		Description: "Known encrypted firmware, " + name,
	}, nil
}

// EncrptedImg is the signature "encrpted_img" of binwalk.
var EncrptedImg = &walk.Signature{
	Name:        "encrpted_img",
	Short:       true,
	Magic:       [][]byte{[]byte("encrpted_img")},
	Description: "D-Link Encrpted Image",
	Parse:       parseEncrptedImg,
}

// parseEncrptedImg is encrpted_img_parser. It accepts each candidate.
func parseEncrptedImg(_ []byte, off int) (walk.Match, error) {
	conf := walk.Medium
	if off != 0 {
		conf = walk.Low
	}
	return walk.Match{
		Offset:      off,
		Confidence:  conf,
		Description: "D-Link Encrpted Image",
	}, nil
}
