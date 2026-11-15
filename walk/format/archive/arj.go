//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/arj.rs and src/structures/arj.rs of
// binwalk.
//
// binwalk extracts an ARJ archive with 7zz, from src/extractors/sevenzip.rs,
// but the walker has no extractor for it. The size of the match is the
// size of the first header only, a byte that is about 30. binwalk carves
// that many bytes, and 7zz cannot open them as an archive. So binwalk
// extracts no file from an ARJ archive, and the walker extracts none too.

package archive

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// ARJ is the signature "arj" of binwalk.
var ARJ = &walk.Signature{
	Name:        "arj",
	Magic:       [][]byte{[]byte("\x60\xea")},
	Description: "ARJ archive data",
	Parse:       parseARJ,
}

var arjStruct = binwalk.Struct{
	{Name: "magic", Type: "u16"},
	{Name: "basic_header_size", Type: "u16"},
	{Name: "extra_header_size", Type: "u8"},
	{Name: "archiver_version", Type: "u8"},
	{Name: "min_version", Type: "u8"},
	{Name: "host_os", Type: "u8"},
	{Name: "internal_flags", Type: "u8"},
	{Name: "compression_method", Type: "u8"},
	{Name: "file_type", Type: "u8"},
	{Name: "reserved1", Type: "u8"},
	{Name: "datetime_file", Type: "u32"},
	{Name: "compressed_filesize", Type: "u32"},
	{Name: "original_filesize", Type: "u32"},
}

// The names of the hosts, the methods and the types of file of ARJ.
var (
	arjHosts = []string{
		"MS-DOS", "PRIMOS", "UNIX", "AMIGA", "MAX-OS", "OS/2", "APPLE GS",
		"ATARI ST", "NeXT", "VAX VMS",
	}
	arjMethods = []string{
		"stored", "compressed most", "compressed", "compressed faster",
		"compressed fastest",
	}
	arjTypes = []string{
		"binary", "7-bit text", "comment header", "directory", "volume label",
	}
)

// parseARJ is arj_parser, with parse_arj_header.
func parseARJ(data []byte, off int) (walk.Match, error) {
	b := data[off:]
	h, ok := arjStruct.Parse(b, false)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	version, minVersion := h["archiver_version"], h["min_version"]
	if version < 1 || version > 16 || minVersion < 1 || minVersion > 16 || version < minVersion {
		return walk.Match{}, walk.ErrNoMatch
	}
	f := h["internal_flags"]
	flags := "no password"
	if f&0x01 != 0 {
		flags = "password"
	}
	if f&0x04 != 0 {
		flags += "|multi-volume"
	}
	if f&0x10 != 0 {
		flags += "|slash-switched"
	}
	if f&0x20 != 0 {
		flags += "|backup"
	}
	host, method, typ := h["host_os"], h["compression_method"], h["file_type"]
	if host >= len(arjHosts) || method >= len(arjMethods) || typ >= len(arjTypes) {
		return walk.Match{}, walk.ErrNoMatch
	}
	// binwalk takes each size as an i32, and rejects a negative one.
	compressed, uncompressed := int32(h["compressed_filesize"]), int32(h["original_filesize"])
	if compressed < 0 || uncompressed < 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	// The size of the header is the byte of the size of the first header,
	// and the name is read 4 bytes past it, as binwalk reads them.
	size := h["extra_header_size"]
	name := ""
	if rest, ok := binwalk.From(b, size+4); ok {
		name = binwalk.CString(rest)
	}
	if size > len(b) {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       size,
		Confidence: walk.Medium,
		Description: fmt.Sprintf("ARJ archive data, header size: %d, version %d, minimum version to extract: %d, "+
			"flags: %s, compression method: %s, file type: %s, original name: %s, original file date: %s, "+
			"compressed file size: %d, uncompressed file size: %d, os: %s",
			size, version, minVersion, flags, arjMethods[method], arjTypes[typ], name,
			binwalk.EpochString(uint32(h["datetime_file"])), compressed, uncompressed, arjHosts[host]),
		Declined: arjTypes[typ] != "comment header",
	}, nil
}
