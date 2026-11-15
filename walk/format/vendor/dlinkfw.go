//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates the signature of src/formats/dlink_fw.rs of
// binwalk-ng.

package vendor

import (
	"fmt"
	"strings"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// DLinkFW is the signature "dlink_fw" of binwalk-ng: the firmware of D-Link
// DAP-1325 and DAP-1610, with a header of 64 bytes in plain text before the
// encrypted data.
var DLinkFW = &walk.Signature{
	Name:        "dlink_fw",
	Magic:       [][]byte{[]byte("DAP-1325"), []byte("DAP-1610")},
	Description: "D-Link firmware",
	Parse:       parseDLinkFW,
}

// dlinkFWHeaderSize is the size of the header of dlink_fw, in plain text.
const dlinkFWHeaderSize = 64

// parseDLinkFW is dlink_fw_parser. The header holds the model, the hardware
// revision, the version and the build, as strings, and the mark
// "encrypt\x00". The match has no size, as in binwalk-ng.
func parseDLinkFW(data []byte, off int) (walk.Match, error) {
	h, ok := binwalk.Range(data, off, off+dlinkFWHeaderSize)
	if !ok || string(h[56:64]) != "encrypt\x00" {
		return walk.Match{}, walk.ErrNoMatch
	}
	field := func(start, end int) string {
		return strings.TrimRight(binwalk.CString(h[start:end]), "\r")
	}
	conf := walk.Low
	if off == 0 {
		conf = walk.High
	}
	return walk.Match{
		Offset:     off,
		Confidence: conf,
		Description: fmt.Sprintf("D-Link firmware, model: %s, HW rev: %s, firmware version: %s, build: %s",
			field(0, 32), field(32, 40), field(40, 48), field(48, 56)),
	}, nil
}
