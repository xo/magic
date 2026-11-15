//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/zip.rs and src/structures/zip.rs of
// binwalk.

// Package zipsig holds the parser of the ZIP signature of binwalk, which the
// signatures zip and dahua_zip share.
package zipsig

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// LocalHeaderSize is the bytes of the fixed fields of a local file header.
var LocalHeaderSize = localStruct.Size()

// Parse is zip_parser of signatures/zip.rs.
func Parse(data []byte, off int) (walk.Match, error) {
	first, ok := Header(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	m := walk.Match{Offset: off, Confidence: walk.High}
	if eof, count, ok := EOF(data, off); ok {
		m.Size = eof - off
		m.Description = fmt.Sprintf("ZIP archive, version: %d.%d, file count: %d, total size: %d bytes",
			first.Major, first.Minor, count, m.Size)
		return m, nil
	}
	// With no end of central directory, follow the local file headers.
	// binwalk compares the absolute offset of the next header with the bytes
	// after off, and the Go code does the same.
	available := len(data) - off
	next, previous := off+first.TotalSize, -1
	for binwalk.OffsetSafe(available, next, previous) {
		h, ok := Header(data[next:])
		if !ok {
			m.Size = next - off
			m.Description = fmt.Sprintf("ZIP archive, version: %d.%d, missing end-of-central-directory header, total size: %d bytes",
				first.Major, first.Minor, m.Size)
			return m, nil
		}
		previous = next
		next += h.TotalSize
	}
	return walk.Match{}, walk.ErrNoMatch
}

// EOF is find_zip_eof: the end of the first end of central directory
// after off that parses, and the count of its files.
func EOF(data []byte, off int) (int, int, bool) {
	magic := []byte("PK\x05\x06\x00\x00\x00\x00")
	for start := off; ; start++ {
		i := bytes.Index(data[start:], magic)
		if i < 0 {
			return 0, 0, false
		}
		start += i
		if size, count, ok := eocd(data[start:]); ok {
			return start + size, count, true
		}
	}
}

var localStruct = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "version", Type: "u16"},
	{Name: "flags", Type: "u16"},
	{Name: "compression", Type: "u16"},
	{Name: "modification_time", Type: "u16"},
	{Name: "modification_date", Type: "u16"},
	{Name: "crc", Type: "u32"},
	{Name: "compressed_size", Type: "u32"},
	{Name: "uncompressed_size", Type: "u32"},
	{Name: "file_name_len", Type: "u16"},
	{Name: "extra_field_len", Type: "u16"},
}

// methods are the compression methods that a local file header can name.
var methods = []int{
	0, 1, 2, 3, 4, 5, 6, 8, 9, 10, 12, 14, 18, 19, 20, 93, 94, 95, 96, 97, 98,
	99,
}

// HeaderInfo is a local file header.
type HeaderInfo struct {
	HeaderSize, TotalSize int // the bytes of the header, and of the header and its data
	NameLen               int // the bytes of the name, after the fixed fields
	Major, Minor          int // the version that made the file
	Flags, Method         int // the flags and the compression method
}

// Header is parse_zip_header of structures/zip.rs. It does not check the
// magic.
func Header(data []byte) (HeaderInfo, bool) {
	const unusedFlags = 0b11010111_10000000
	h, ok := localStruct.Parse(data, false)
	if !ok || h["flags"]&unusedFlags != 0 || !slices.Contains(methods, h["compression"]) {
		return HeaderInfo{}, false
	}
	size := h["compressed_size"]
	if size == 0 {
		size = h["uncompressed_size"]
	}
	header := localStruct.Size() + h["file_name_len"] + h["extra_field_len"]
	return HeaderInfo{
		HeaderSize: header,
		NameLen:    h["file_name_len"],
		TotalSize:  header + size,
		Major:      h["version"] / 10,
		Minor:      h["version"] % 10,
		Flags:      h["flags"],
		Method:     h["compression"],
	}, true
}

var eocdStruct = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "disk_number", Type: "u16"},
	{Name: "central_directory_disk_number", Type: "u16"},
	{Name: "central_directory_disk_entries", Type: "u16"},
	{Name: "central_directory_total_entries", Type: "u16"},
	{Name: "central_directory_size", Type: "u32"},
	{Name: "central_directory_offset", Type: "u32"},
	{Name: "comment_length", Type: "u16"},
}

// eocd is parse_eocd_header of structures/zip.rs. It returns the size of
// the end of central directory, with its comment, and the count of files.
func eocd(data []byte) (int, int, bool) {
	h, ok := eocdStruct.Parse(data, false)
	if !ok {
		return 0, 0, false
	}
	total := h["central_directory_total_entries"]
	if h["central_directory_disk_entries"] != total || total == 0 {
		return 0, 0, false
	}
	return eocdStruct.Size() + h["comment_length"], total, true
}
