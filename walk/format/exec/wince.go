//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/wince.rs, src/structures/wince.rs and
// src/extractors/wince.rs of binwalk.

package exec

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// WinCE is the signature "wince" of binwalk.
var WinCE = &walk.Signature{
	Name:        "wince",
	Magic:       [][]byte{[]byte("B000FF\n")},
	Description: "Windows CE binary image",
	Parse:       parseWinCE,
	Extract:     extractWinCE,
}

// parseWinCE is wince_parser.
func parseWinCE(data []byte, off int) (walk.Match, error) {
	size, ok := winCESize(data, off)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	h, ok := winCEHeader.Parse(data[off:], false)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       size,
		Confidence: walk.High,
		Description: fmt.Sprintf("Windows CE binary image, base address: 0x%X, image size: %d bytes, file size: %d bytes",
			h["image_start"], h["image_size"], size),
	}, nil
}

var (
	// winCEHeader is the structure of parse_wince_header.
	winCEHeader = binwalk.Struct{
		{Name: "magic_p1", Type: "u32"},
		{Name: "magic_p2", Type: "u24"},
		{Name: "image_start", Type: "u32"},
		{Name: "image_size", Type: "u32"},
	}
	// winCEBlock is the structure of parse_wince_block_header.
	winCEBlock = binwalk.Struct{
		{Name: "address", Type: "u32"},
		{Name: "size", Type: "u32"},
		{Name: "checksum", Type: "u32"},
	}
)

// winCESize is the dry run of wince_dump.
func winCESize(data []byte, off int) (int, bool) {
	r := extractWinCE(data, off, nil)
	return r.Size, r.OK
}

// extractWinCE is wince_dump. It accepts the image when its blocks end
// with the end block, and the first block is at the base address of the
// header. It writes the data of each block to a file named after the
// address of the block in hexadecimal, such as 80001000.bin.
func extractWinCE(data []byte, off int, out *walk.Output) walk.Extraction {
	image, ok := binwalk.From(data, off)
	if !ok {
		return walk.Extraction{}
	}
	h, ok := winCEHeader.Parse(image, false)
	if !ok {
		return walk.Extraction{}
	}
	blocks, ok := binwalk.From(image, winCEHeader.Size())
	if !ok {
		return walk.Extraction{}
	}
	entries, total, ok := winCEBlocks(blocks)
	// The address of the first block is the base address of the header.
	if !ok || entries[0].address != h["image_start"] {
		return walk.Extraction{}
	}
	r := walk.Extraction{OK: true, Size: winCEHeader.Size() + total, HasSize: true}
	if out == nil {
		return r
	}
	for _, b := range entries {
		if !out.CarveFile(fmt.Sprintf("%X.bin", b.address), blocks, b.offset, b.size) {
			r.OK = false
			break
		}
	}
	return r
}

// winCEBlockInfo is BlockInfo: the address of a block, and the offset and
// the size of its data in the data of the blocks.
type winCEBlockInfo struct {
	address int
	offset  int
	size    int
}

// winCEBlocks is process_wince_blocks. It returns the blocks before the end
// block, and the size of all the blocks with the end block.
func winCEBlocks(data []byte) ([]winCEBlockInfo, int, bool) {
	// binwalk wants more than this many blocks, to be sure of the image.
	const minEntries = 5
	var entries []winCEBlockInfo
	total := 0
	off, previous := 0, -1
	for binwalk.OffsetSafe(len(data), off, previous) {
		b, ok := winCEBlock.Parse(data[off:], false)
		if !ok {
			break
		}
		total += winCEBlock.Size()
		// A block of address 0 ends the image.
		if b["address"] == 0 {
			if len(entries) > minEntries {
				return entries, total, true
			}
			break
		}
		total += b["size"]
		entries = append(entries, winCEBlockInfo{
			address: b["address"],
			offset:  off + winCEBlock.Size(),
			size:    b["size"],
		})
		previous = off
		off += winCEBlock.Size() + b["size"]
	}
	return nil, 0, false
}
