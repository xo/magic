//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/qcow.rs and src/structures/qcow.rs of
// binwalk.

package disk

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// QCOW is the signature "qcow" of binwalk.
var QCOW = &walk.Signature{
	Name:        "qcow",
	Short:       true,
	Magic:       [][]byte{[]byte("QFI\xFB")},
	Description: "QEMU QCOW Image",
	Parse:       parseQCOW,
}

// parseQCOW is qcow_parser. binwalk reads the header at the start of the
// data, not at the offset of the magic. The signature is short, so the two
// are the same.
func parseQCOW(data []byte, off int) (walk.Match, error) {
	h, ok := parseQCOWHeader(data)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Confidence: walk.Medium,
		Description: fmt.Sprintf("QEMU QCOW Image, version: %d, storage media size: %#x bytes, cluster block size: %#x bytes, encryption method: %s",
			h.version, h.mediaSize, 1<<h.clusterBits, h.encryption),
	}, nil
}

type qcowHeader struct {
	version     int
	mediaSize   uint64
	clusterBits int
	encryption  string
}

var qcowBase = binwalk.Struct{{Name: "magic", Type: "u32"}, {Name: "version", Type: "u32"}}

var qcowV1 = binwalk.Struct{
	{Name: "backing_filename_offset", Type: "u64"},
	{Name: "backing_filename_size", Type: "u32"},
	{Name: "modification_timestamp", Type: "u32"},
	{Name: "storage_media_size", Type: "u64"},
	{Name: "cluster_block_bits", Type: "u8"},
	{Name: "level2_table_bits", Type: "u8"},
	{Name: "reserved1", Type: "u16"},
	{Name: "encryption_method", Type: "u32"},
	{Name: "level1_table_offset", Type: "u64"},
}

var qcowV2 = binwalk.Struct{
	{Name: "backing_filename_offset", Type: "u64"},
	{Name: "backing_filename_size", Type: "u32"},
	{Name: "cluster_block_bits", Type: "u32"},
	{Name: "storage_media_size", Type: "u64"},
	{Name: "encryption_method", Type: "u32"},
	{Name: "level1_table_refs", Type: "u32"},
	{Name: "level1_table_offset", Type: "u64"},
	{Name: "refcount_table_offset", Type: "u64"},
	{Name: "refcount_table_clusters", Type: "u32"},
	{Name: "snapshot_count", Type: "u32"},
	{Name: "snapshot_offset", Type: "u64"},
}

var qcowV3 = append(qcowV2[:len(qcowV2):len(qcowV2)],
	binwalk.Field{Name: "incompatible_feature_flags", Type: "u64"},
	binwalk.Field{Name: "compatible_feature_flags", Type: "u64"},
	binwalk.Field{Name: "autoclear_feature_flags", Type: "u64"},
	binwalk.Field{Name: "refcount_order", Type: "u32"},
	binwalk.Field{Name: "file_hdr_size", Type: "u32"},
)

var qcowEncryption = map[int]string{0: "None", 1: "AES128-CBC", 2: "LUKS"}

// parseQCOWHeader is parse_qcow_header.
func parseQCOWHeader(data []byte) (qcowHeader, bool) {
	base, ok := qcowBase.Parse(data, true)
	if !ok {
		return qcowHeader{}, false
	}
	var s binwalk.Struct
	switch base["version"] {
	case 1:
		s = qcowV1
	case 2:
		s = qcowV2
	case 3:
		s = qcowV3
	default:
		return qcowHeader{}, false
	}
	h, ok := s.Parse(data[8:], true)
	if !ok {
		return qcowHeader{}, false
	}
	encryption, ok := qcowEncryption[h["encryption_method"]]
	if !ok {
		return qcowHeader{}, false
	}
	bits := h["cluster_block_bits"]
	if bits < 9 || bits > 21 {
		return qcowHeader{}, false
	}
	// The offsets that the version has must be on a cluster boundary.
	for _, name := range []string{"level1_table_offset", "refcount_table_offset", "snapshot_offset"} {
		if _, ok := h[name]; ok && u64(h, name)%(1<<bits) != 0 {
			return qcowHeader{}, false
		}
	}
	return qcowHeader{
		version:     base["version"],
		mediaSize:   u64(h, "storage_media_size"),
		clusterBits: bits,
		encryption:  encryption,
	}, true
}
