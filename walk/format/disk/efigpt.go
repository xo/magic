//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/efigpt.rs and src/structures/efigpt.rs
// of binwalk.

package disk

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// EFIGPT is the signature "efigpt" of binwalk.
var EFIGPT = &walk.Signature{
	Name:        "efigpt",
	Magic:       [][]byte{[]byte("\x55\xAAEFI PART")},
	Description: "EFI Global Partition Table",
	Parse:       parseEFIGPT,
}

// parseEFIGPT is efigpt_parser.
func parseEFIGPT(data []byte, off int) (walk.Match, error) {
	const magicOffset = 0x1FE
	if off < magicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - magicOffset
	// binwalk counts the available bytes from the magic, not from the start.
	available := uint64(len(data) - off)
	total, ok := efiGPTSize(data[start:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	// Some images have partitions past the end of the file. The image then
	// ends at the end of the file.
	total = min(total, available)
	return walk.Match{
		Offset:      start,
		Size:        int(total),
		Confidence:  walk.High,
		Description: fmt.Sprintf("EFI Global Partition Table, total size: %d", total),
	}, nil
}

const efiBlockSize = 512

var efiGPTHeader = binwalk.Struct{
	{Name: "magic", Type: "u64"},
	{Name: "revision", Type: "u32"},
	{Name: "header_size", Type: "u32"},
	{Name: "header_crc", Type: "u32"},
	{Name: "reserved", Type: "u32"},
	{Name: "my_lba", Type: "u64"},
	{Name: "alternate_lba", Type: "u64"},
	{Name: "first_usable_lba", Type: "u64"},
	{Name: "last_usable_lba", Type: "u64"},
	{Name: "disk_guid_p1", Type: "u64"},
	{Name: "disk_guid_p2", Type: "u64"},
	{Name: "partition_entry_lba", Type: "u64"},
	{Name: "partition_entry_count", Type: "u32"},
	{Name: "partition_entry_size", Type: "u32"},
	{Name: "partition_entries_crc", Type: "u32"},
}

var efiGPTEntry = binwalk.Struct{
	{Name: "type_guid_p1", Type: "u64"},
	{Name: "type_guid_p2", Type: "u64"},
	{Name: "partition_guid_p1", Type: "u64"},
	{Name: "partition_guid_p2", Type: "u64"},
	{Name: "starting_lba", Type: "u64"},
	{Name: "ending_lba", Type: "u64"},
	{Name: "attributes", Type: "u64"},
}

// efiGPTSize is parse_efigpt_header: the end of the farthest partition. The
// sums wrap as the usize of binwalk does.
func efiGPTSize(data []byte) (uint64, bool) {
	const revision = 0x00010000
	gpt, ok := binwalk.From(data, efiBlockSize)
	if !ok {
		return 0, false
	}
	h, ok := efiGPTHeader.Parse(gpt, false)
	if !ok || h["reserved"] != 0 || h["revision"] != revision {
		return 0, false
	}
	start := u64(h, "partition_entry_lba") * efiBlockSize
	end := start + u64(h, "partition_entry_count")*u64(h, "partition_entry_size")
	if start > end || end > uint64(len(data)) {
		return 0, false
	}
	entries := data[start:end]
	if binwalk.CRC32(entries) != uint32(h["partition_entries_crc"]) {
		return 0, false
	}
	var total uint64
	next, previous := 0, -1
	for binwalk.OffsetSafe(len(entries), next, previous) {
		// parse_gpt_partition_entry
		e, ok := efiGPTEntry.Parse(entries[next:], false)
		if ok && e["type_guid_p1"] != 0 && e["type_guid_p2"] != 0 {
			first := u64(e, "starting_lba") * efiBlockSize
			last := u64(e, "ending_lba") * efiBlockSize
			if first < last && last > total {
				total = last
			}
		}
		previous = next
		next += h["partition_entry_size"]
	}
	return total, total > 0
}
