//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates the parser of src/formats/efigpt.rs of binwalk-ng.

package disk

import (
	"fmt"
	"math"

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
	available := uint64(len(data) - start)
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

// efiGPTSize is parse_efigpt_header: the end of the farthest partition.
// binwalk-ng rejects the header when an offset of the table overflows, and
// leaves out a partition whose offsets overflow.
func efiGPTSize(data []byte) (uint64, bool) {
	const (
		revision = 0x00010000
		// The field of the CRC of the header, which the CRC counts as 0.
		crcOffset = 16
		crcLen    = 4
	)
	gpt, ok := binwalk.From(data, efiBlockSize)
	if !ok {
		return 0, false
	}
	h, ok := efiGPTHeader.Parse(gpt, false)
	if !ok || h["reserved"] != 0 || h["revision"] != revision {
		return 0, false
	}
	// The CRC of the header covers header_size bytes, which hold the fields
	// and fit in one block.
	n := h["header_size"]
	if n < efiGPTHeader.Size() || n > efiBlockSize {
		return 0, false
	}
	hdr, ok := binwalk.Range(gpt, 0, n)
	if !ok {
		return 0, false
	}
	c := make([]byte, n)
	copy(c, hdr)
	clear(c[crcOffset : crcOffset+crcLen])
	if binwalk.CRC32(c) != uint32(h["header_crc"]) {
		return 0, false
	}
	start, ok := efiLBAOffset(u64(h, "partition_entry_lba"))
	if !ok {
		return 0, false
	}
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
			first, ok1 := efiLBAOffset(u64(e, "starting_lba"))
			last, ok2 := efiLBAOffset(u64(e, "ending_lba"))
			if ok1 && ok2 && first < last && last > total {
				total = last
			}
		}
		previous = next
		next += h["partition_entry_size"]
	}
	return total, total > 0
}

// efiLBAOffset is lba_to_offset: the offset of a block, and false when it
// overflows 64 bits.
func efiLBAOffset(lba uint64) (uint64, bool) {
	if lba > math.MaxUint64/efiBlockSize {
		return 0, false
	}
	return lba * efiBlockSize, true
}
