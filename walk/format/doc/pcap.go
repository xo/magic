//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/pcap.rs of binwalk-ng.

package doc

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// PcapNG is the signature "pcapng" of binwalk-ng.
var PcapNG = &walk.Signature{
	Name:        "pcapng",
	Magic:       [][]byte{[]byte("\x0A\x0D\x0D\x0A")},
	Description: "Pcap-NG capture file",
	Parse:       parsePcapNG,
	Extract:     extractPcapNG,
	NoRecurse:   true,
}

// extractPcapNG is pcapng_carver: it carves the capture to capture.pcapng.
func extractPcapNG(data []byte, off int, out *walk.Output) walk.Extraction {
	size, ok := pcapNGSize(data, off)
	if !ok {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: out == nil || out.CarveFile("capture.pcapng", data, off, size), Size: size, HasSize: true}
}

// parsePcapNG is pcapng_parser.
func parsePcapNG(data []byte, off int) (walk.Match, error) {
	r := extractPcapNG(data, off, nil)
	if !r.OK || !r.HasSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := r.Size
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("Pcap-NG capture file, total size: %d bytes", size),
		// A file that is only the capture needs no carving.
		Declined: off == 0 && size == len(data),
	}, nil
}

// pcapNGSize is the dry run of pcapng_carver: the size of the blocks. A
// capture has at least a section header block and one more block.
func pcapNGSize(data []byte, off int) (int, bool) {
	const minBlocks = 2
	blockSize, big, ok := parsePcapNGSection(data[off:])
	if !ok {
		return 0, false
	}
	// binwalk-ng compares the offset of the next block with the size of
	// all of the data, where binwalk compared it with the bytes from the
	// start of the capture.
	available := len(data)
	count := 1
	next, previous := off+blockSize, -1
	for binwalk.OffsetSafe(available, next, previous) {
		_, size, ok := parsePcapNGBlock(data[next:], big)
		if !ok {
			break
		}
		count++
		previous = next
		next += size
	}
	if count < minBlocks {
		return 0, false
	}
	return next - off, true
}

var (
	pcapNGBlockHeader = binwalk.Struct{{Name: "block_type", Type: "u32"}, {Name: "block_size", Type: "u32"}}
	pcapNGBlockFooter = binwalk.Struct{{Name: "block_size", Type: "u32"}}
	pcapNGSection     = binwalk.Struct{
		{Name: "block_type", Type: "u32"},
		{Name: "block_size", Type: "u32"},
		{Name: "endian_magic", Type: "u32"},
		{Name: "major_version", Type: "u16"},
		{Name: "minor_version", Type: "u16"},
		{Name: "section_length", Type: "u32"},
	}
)

// parsePcapNGBlock is parse_pcapng_block: the type and the size of the
// block, whose footer must repeat the size.
func parsePcapNGBlock(data []byte, big bool) (int, int, bool) {
	const reservedMask = 0x80000000
	h, ok := pcapNGBlockHeader.Parse(data, big)
	if !ok || h["block_type"]&reservedMask != 0 {
		return 0, 0, false
	}
	size := h["block_size"]
	// binwalk-ng rejects a size below the size of the footer. Range
	// rejects the negative start in the same way.
	start := size - pcapNGBlockFooter.Size()
	b, ok := binwalk.Range(data, start, start+pcapNGBlockFooter.Size())
	if !ok {
		return 0, 0, false
	}
	f, ok := pcapNGBlockFooter.Parse(b, big)
	if !ok || f["block_size"] != size {
		return 0, 0, false
	}
	return h["block_type"], size, true
}

// parsePcapNGSection is parse_pcapng_section_block: the size of the section
// header block, and whether the capture is big-endian.
func parsePcapNGSection(data []byte) (int, bool, bool) {
	const sectionType = 0x0A0D0D0A
	h, ok := pcapNGSection.Parse(data, false)
	if !ok {
		return 0, false, false
	}
	var big bool
	switch h["endian_magic"] {
	case 0x1A2B3C4D:
	case 0x4D3C2B1A:
		big = true
	default:
		return 0, false, false
	}
	typ, size, ok := parsePcapNGBlock(data, big)
	if !ok || typ != sectionType {
		return 0, false, false
	}
	return size, big, true
}
