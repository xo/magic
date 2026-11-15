//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/deb.rs of binwalk-ng. binwalk-ng does not
// extract a Debian package.

package archive

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// Deb is the signature "deb" of binwalk-ng.
var Deb = &walk.Signature{
	Name:        "deb",
	Magic:       [][]byte{[]byte("!<arch>\ndebian-binary\x20\x20\x20")},
	Description: "Debian package file",
	Parse:       parseDeb,
}

// parseDeb is deb_parser.
func parseDeb(data []byte, off int) (walk.Match, error) {
	size, ok := debSize(data[off:])
	if !ok || size > len(data)-off {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("Debian package file, total size: %d bytes", size),
	}, nil
}

// debSize is parse_deb_header. It reads the size of the member
// control.tar, the second member of the ar archive, to find the header of
// the third member, data.tar, and the size of that member. The size ends
// at the end of data.tar, and binwalk does not count the padding of the
// member or the bytes of a fourth member.
func debSize(data []byte) (int, bool) {
	const (
		endMarkerSize    = 2
		dataSizeLen      = 10
		dataSizeOffset   = 48
		controlSizeStart = 120
		controlSizeEnd   = 130
	)
	b, ok := binwalk.Range(data, controlSizeStart, controlSizeEnd)
	if !ok {
		return 0, false
	}
	control, ok := decimal(b)
	if !ok {
		return 0, false
	}
	start := controlSizeEnd + endMarkerSize + control + dataSizeOffset
	end := start + dataSizeLen
	if b, ok = binwalk.Range(data, start, end); !ok {
		return 0, false
	}
	size, ok := decimal(b)
	if !ok {
		return 0, false
	}
	return end + endMarkerSize + size, true
}

// decimal reads a field of an ar header as binwalk reads it: the bytes must
// be UTF-8, and trim().parse::<usize>() of Rust takes them. Rust trims the
// white space of Unicode, as strings.TrimSpace does, and takes a "+"
// before the digits.
func decimal(b []byte) (int, bool) {
	if !utf8.Valid(b) {
		return 0, false
	}
	s := strings.TrimSpace(string(b))
	s = strings.TrimPrefix(s, "+")
	if s == "" || s[0] < '0' || s[0] > '9' {
		return 0, false
	}
	// The field holds at most 10 bytes, so the value fits.
	n, err := strconv.ParseUint(s, 10, 63)
	if err != nil {
		return 0, false
	}
	return int(n), true
}
