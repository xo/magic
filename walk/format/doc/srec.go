//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/srec.rs of binwalk.

package doc

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// SRecord is the signature "srecord" of binwalk. The magic is a header
// record that holds the text "HDR" in hex.
var SRecord = &walk.Signature{
	Name:        "srecord",
	Magic:       [][]byte{[]byte("S00600004844521B")},
	Description: "Motorola S-record",
	Parse:       parseSRecord,
}

// SRecordGeneric is the signature "srecord_generic" of binwalk. Its magic
// is short, so it matches only at the start of the data.
var SRecordGeneric = &walk.Signature{
	Name:        "srecord_generic",
	Short:       true,
	Magic:       [][]byte{[]byte("S0")},
	Description: "Motorola S-record (generic)",
	Parse:       parseSRecord,
}

// parseSRecord is srec_parser. It finds a footer line, "\nS9", "\nS8" or
// "\nS7", and the end of that line. The three footers cannot overlap, so a
// search for each in order of position gives the order of the matcher of
// binwalk.
func parseSRecord(data []byte, off int) (walk.Match, error) {
	const footerLen = 3
	for i := off; i+footerLen <= len(data); i++ {
		if data[i] != '\n' || data[i+1] != 'S' || (data[i+2] != '9' && data[i+2] != '8' && data[i+2] != '7') {
			continue
		}
		os := "Unix"
		eof, previous := i+footerLen, -1
		for binwalk.OffsetSafe(len(data), eof, previous) {
			if data[eof] != '\r' && data[eof] != '\n' {
				previous = eof
				eof++
				continue
			}
			// A line ends in "\n" or "\r\n".
			if data[eof] == '\r' {
				eof++
				os = "Windows"
			}
			if eof < len(data) && data[eof] == '\n' {
				eof++
				size := eof - off
				return walk.Match{
					Offset:      off,
					Size:        size,
					Confidence:  walk.High,
					Description: fmt.Sprintf("Motorola S-record, origin OS: %s, total size: %d bytes", os, size),
				}, nil
			}
			return walk.Match{}, walk.ErrNoMatch
		}
	}
	return walk.Match{}, walk.ErrNoMatch
}
