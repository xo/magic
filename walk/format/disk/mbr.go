//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/mbr.rs, src/structures/mbr.rs and
// src/extractors/mbr.rs of binwalk.

package disk

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// mbrMagicOffset is the offset of the magic from the start of the MBR.
const mbrMagicOffset = 0x1FE

// MBR is the signature "mbr" of binwalk.
var MBR = &walk.Signature{
	Name:        "mbr",
	Short:       true,
	Magic:       [][]byte{[]byte("\x55\xAA")},
	MagicOffset: mbrMagicOffset,
	Description: "DOS Master Boot Record",
	Parse:       parseMBR,
	Extract:     extractMBR,
}

// parseMBR is mbr_parser.
func parseMBR(data []byte, off int) (walk.Match, error) {
	if off != mbrMagicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - mbrMagicOffset
	r := extractMBR(data, start, nil)
	if !r.OK || !r.HasSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	h, ok := parseMBRImage(data[start:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	m := walk.Match{
		Offset:      start,
		Size:        r.Size,
		Confidence:  walk.Medium,
		Description: "DOS Master Boot Record",
	}
	for _, p := range h.partitions {
		// The extraction of a partition at the start of the MBR holds the
		// MBR again, and then the walk of members does not end. binwalk
		// declines the extraction.
		if p.start == start {
			m.Declined = true
		}
		m.Description += ", partition: " + p.name
	}
	m.Description += fmt.Sprintf(", image size: %d bytes", m.Size)
	return m, nil
}

// extractMBR is extract_mbr_partitions: it carves each partition to
// "<name>_partition.<n>", where n counts the partitions from 0.
//
// binwalk carves a partition from its start in data, not from its start
// after off. The signature is short, so off is always 0.
func extractMBR(data []byte, off int, out *walk.Output) walk.Extraction {
	h, ok := parseMBRImage(data[off:])
	if !ok || len(h.partitions) == 0 || len(data)-off < h.imageSize {
		return walk.Extraction{}
	}
	r := walk.Extraction{OK: true, Size: h.imageSize, HasSize: true}
	if out != nil {
		for i, p := range h.partitions {
			r.OK = out.CarveFile(fmt.Sprintf("%s_partition.%d", p.name, i), data, p.start, p.size)
			if !r.OK {
				break
			}
		}
	}
	return r
}

type mbrPartition struct {
	start int
	size  int
	name  string
}

type mbrHeader struct {
	imageSize  int
	partitions []mbrPartition
}

var mbrEntry = binwalk.Struct{
	{Name: "status", Type: "u8"},
	{Name: "chs_start", Type: "u24"},
	{Name: "os_type", Type: "u8"},
	{Name: "chs_end", Type: "u24"},
	{Name: "lba_start", Type: "u32"},
	{Name: "lba_size", Type: "u32"},
}

var mbrOSTypes = map[int]string{
	0x07: "NTFS_IFS_HPFS_exFAT",
	0x0B: "FAT32",
	0x0C: "FAT32",
	0x43: "Linux",
	0x4D: "QNX Primary Volume",
	0x4E: "QNX Secondary Volume",
	0x81: "Minix",
	0x83: "Linux",
	0x8E: "Linux LVM",
	0x96: "ISO-9660",
	0xB1: "QNXv6 File System",
	0xB2: "QNXv6 File System",
	0xB3: "QNXv6 File System",
	0xEE: "EFI GPT Protective",
	0xEF: "EFI System Partition",
}

// parseMBRImage is parse_mbr_image.
func parseMBRImage(data []byte) (mbrHeader, bool) {
	const (
		blockSize   = 512
		minSize     = blockSize * 2
		count       = 4
		tableOffset = 446
	)
	var h mbrHeader
	table, ok := binwalk.Range(data, tableOffset, tableOffset+mbrEntry.Size()*count)
	if !ok {
		return h, false
	}
	for i := range count {
		e, ok := mbrEntry.Parse(table[i*mbrEntry.Size():], false)
		if !ok {
			return h, false
		}
		if e["os_type"] == 0 && e["lba_size"] == 0 {
			continue
		}
		if e["status"] != 0 && e["status"] != 0x80 {
			continue
		}
		name, ok := mbrOSTypes[e["os_type"]]
		if !ok {
			name = "Unknown"
		}
		start, size := e["lba_start"]*blockSize, e["lba_size"]*blockSize
		end := start + size
		// Partitions past the end of the data are left out, as in binwalk.
		if end > len(data) {
			continue
		}
		if start != 0 {
			h.partitions = append(h.partitions, mbrPartition{start: start, size: size, name: name})
		}
		h.imageSize = max(h.imageSize, end)
	}
	if len(h.partitions) == 0 || h.imageSize <= minSize {
		return h, false
	}
	return h, true
}
