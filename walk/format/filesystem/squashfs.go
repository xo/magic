//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/squashfs.rs and
// src/structures/squashfs.rs of binwalk. binwalk extracts SquashFS with
// sasquatch, a version of unsquashfs, from src/extractors/squashfs.rs, and
// extractSquashFS in squashfs4.go writes the files that it writes.

package filesystem

import (
	"encoding/binary"
	"fmt"
	"math/bits"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// SquashFS is the signature "squashfs" of binwalk.
var SquashFS = &walk.Signature{
	Name: "squashfs",
	Magic: [][]byte{
		[]byte("sqsh"), []byte("hsqs"), []byte("sqlz"), []byte("qshs"),
		[]byte("tqsh"), []byte("hsqt"), []byte("shsq"),
	},
	Description: "SquashFS file system",
	Parse:       parseSquashFS,
	Extract:     extractSquashFS,
}

// squashfsCompression are the compressions that binwalk names.
var squashfsCompression = map[int]string{
	0: "unknown", 1: "gzip", 2: "lzma", 3: "lzo", 4: "xz", 5: "lz4", 6: "zstd",
}

// parseSquashFS is squashfs_parser. It checks the first entry of the table
// of UIDs. binwalk compares the absolute offset of that table with the size
// of the header, and so does parseSquashFS.
func parseSquashFS(data []byte, off int) (walk.Match, error) {
	available := len(data) - off
	h, ok := squashfsHeader(data[off:])
	if !ok || h.imageSize > available {
		return walk.Match{}, walk.ErrNoMatch
	}
	uidStart := off + h.uidTableStart
	if uidStart <= h.headerSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	uidData, ok := binwalk.From(data, uidStart)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	uid, ok := squashfsUIDEntry(uidData, h.major, h.big)
	if !ok || (uid != 0 && (uid <= h.headerSize || uid > h.imageSize)) {
		return walk.Match{}, walk.ErrNoMatch
	}
	comp, ok := squashfsCompression[h.compression]
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	endian := "little"
	if h.big {
		endian = "big"
	}
	return walk.Match{
		Offset:     off,
		Size:       h.imageSize,
		Confidence: walk.High,
		Description: fmt.Sprintf("SquashFS file system, %s endian, version: %d.%d, compression: %s, inode count: %d, block size: %d, image size: %d bytes, created: %s",
			endian, h.major, h.minor, comp, h.inodeCount, h.blockSize, h.imageSize, binwalk.EpochString(uint32(h.timestamp))),
	}, nil
}

var (
	squashfsV4 = binwalk.Struct{
		{Name: "magic", Type: "u32"},
		{Name: "inode_count", Type: "u32"},
		{Name: "modification_time", Type: "u32"},
		{Name: "block_size", Type: "u32"},
		{Name: "fragment_count", Type: "u32"},
		{Name: "compression_id", Type: "u16"},
		{Name: "block_log", Type: "u16"},
		{Name: "flags", Type: "u16"},
		{Name: "id_count", Type: "u16"},
		{Name: "major_version", Type: "u16"},
		{Name: "minor_version", Type: "u16"},
		{Name: "root_inode_ref", Type: "u64"},
		{Name: "image_size", Type: "u64"},
		{Name: "uid_start", Type: "u64"},
	}
	squashfsV3 = binwalk.Struct{
		{Name: "magic", Type: "u32"},
		{Name: "inode_count", Type: "u32"},
		{Name: "bytes_used_2", Type: "u32"},
		{Name: "uid_start_2", Type: "u32"},
		{Name: "guid_start_2", Type: "u32"},
		{Name: "inode_table_start_2", Type: "u32"},
		{Name: "directory_table_start_2", Type: "u32"},
		{Name: "major_version", Type: "u16"},
		{Name: "minor_version", Type: "u16"},
		{Name: "block_size_1", Type: "u16"},
		{Name: "block_log", Type: "u16"},
		{Name: "flags", Type: "u8"},
		{Name: "uid_count", Type: "u8"},
		{Name: "guid_count", Type: "u8"},
		{Name: "modification_time", Type: "u32"},
		{Name: "root_inode_ref", Type: "u64"},
		{Name: "block_size", Type: "u32"},
		{Name: "fragment_entry_count", Type: "u32"},
		{Name: "fragment_table_start_2", Type: "u32"},
		{Name: "image_size", Type: "u64"},
		{Name: "uid_start", Type: "u64"},
		{Name: "guid_start", Type: "u64"},
		{Name: "inode_table_start", Type: "u64"},
		{Name: "directory_table_start", Type: "u64"},
		{Name: "fragment_table_start", Type: "u64"},
		{Name: "lookup_table_start", Type: "u64"},
	}
)

type squashfsInfo struct {
	big                                           bool
	major, minor                                  int
	headerSize, imageSize, uidTableStart          int
	timestamp, blockSize, inodeCount, compression int
}

// squashfsHeader is parse_squashfs_header. The major version is at the same
// place in every version, and gives the byte order.
func squashfsHeader(data []byte) (squashfsInfo, bool) {
	const (
		maxVersion = 4
		minHeader  = 120
	)
	if len(data) <= minHeader {
		return squashfsInfo{}, false
	}
	var h squashfsInfo
	v := int(binary.LittleEndian.Uint16(data[28:30]))
	if v == 0 || v > maxVersion {
		h.big = true
		v = int(binary.BigEndian.Uint16(data[28:30]))
	}
	if v == 0 || v > maxVersion {
		return squashfsInfo{}, false
	}
	var f binwalk.Fields
	var ok bool
	if v == 4 {
		h.headerSize = squashfsV4.Size()
		f, ok = squashfsV4.Parse(data, h.big)
	} else {
		h.headerSize = squashfsV3.Size()
		f, ok = squashfsV3.Parse(data, h.big)
		if ok && v < 3 {
			f["uid_start"] = f["uid_start_2"]
			f["image_size"] = f["bytes_used_2"]
		}
	}
	if !ok {
		return squashfsInfo{}, false
	}
	h.imageSize = f["image_size"]
	if h.imageSize <= minHeader || f["block_size"] <= 0 || f["block_log"] != bits.Len(uint(f["block_size"]))-1 {
		return squashfsInfo{}, false
	}
	h.timestamp = f["modification_time"]
	h.blockSize = f["block_size"]
	h.inodeCount = f["inode_count"]
	h.major = f["major_version"]
	h.minor = f["minor_version"]
	h.uidTableStart = f["uid_start"]
	h.compression = f["compression_id"]
	return h, true
}

// squashfsUIDEntry is parse_squashfs_uid_entry: the first pointer of the
// table of UIDs, of 64 bits in version 4 and of 32 bits before.
func squashfsUIDEntry(data []byte, version int, big bool) (int, bool) {
	typ := "u32"
	if version == 4 {
		typ = "u64"
	}
	s := binwalk.Struct{{Name: "uid_block_ptr", Type: typ}}
	f, ok := s.Parse(data, big)
	return f["uid_block_ptr"], ok
}
