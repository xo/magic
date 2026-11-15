//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/dahua_zip.rs and
// src/extractors/dahua_zip.rs of binwalk.

package vendor

import (
	"strings"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/format/internal/zipsig"
	"github.com/xo/magic/walk/internal/binwalk"
)

// DahuaZIP is the signature "dahua_zip" of binwalk.
var DahuaZIP = &walk.Signature{
	Name:        "dahua_zip",
	Magic:       [][]byte{[]byte("DH\x03\x04")},
	Description: "Dahua ZIP archive",
	Parse:       parseDahuaZIP,
	Extract:     extractDahuaZIP,
}

// extractDahuaZIP is extract_dahua_zip: it carves the archive to dahua.zip,
// with "PK" in place of "DH". It needs the end of central directory.
func extractDahuaZIP(data []byte, off int, out *walk.Output) walk.Extraction {
	const name, magic = "dahua.zip", "PK"
	eof, _, ok := zipsig.EOF(data, off)
	if !ok {
		return walk.Extraction{}
	}
	r := walk.Extraction{OK: true, Size: eof - off, HasSize: true}
	if out != nil {
		b, ok := binwalk.Range(data, off+len(magic), eof)
		r.OK = ok && out.CreateFile(name, []byte(magic)) && out.AppendFile(name, b)
	}
	return r
}

// parseDahuaZIP is dahua_zip_parser. A Dahua ZIP archive is a ZIP archive
// whose first local file header starts with "DH" in place of "PK".
func parseDahuaZIP(data []byte, off int) (walk.Match, error) {
	m, err := zipsig.Parse(data, off)
	if err != nil {
		return walk.Match{}, walk.ErrNoMatch
	}
	m.Description = strings.ReplaceAll(m.Description, "ZIP archive", "Dahua ZIP archive")
	return m, nil
}
