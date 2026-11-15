//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/zstd.rs of binwalk-ng.

package compressed

import (
	"fmt"

	"github.com/xo/magic/compress"
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// Zstd is the signature "zstd" of binwalk-ng. binwalk-ng extracts it with
// its own code, through the crate zstd, which writes the data to
// decompressed.bin. The walker writes the same file, with the decoder of
// compress/zstd.
var Zstd = &walk.Signature{
	Name:        "zstd",
	Magic:       [][]byte{[]byte("\x28\xb5\x2f\xfd")},
	Description: "ZSTD compressed data",
	Parse:       parseZstd,
	Extract:     extractZstd,
}

var (
	zstdHeader = binwalk.Struct{{Name: "magic", Type: "u32"}, {Name: "frame_header_descriptor", Type: "u8"}}
	zstdBlock  = binwalk.Struct{{Name: "info_bits", Type: "u24"}}
)

// parseZstd is zstd_parser: it reads the block headers to the last block,
// and needs at least two blocks.
func parseZstd(data []byte, off int) (walk.Match, error) {
	const checksumSize, minBlocks = 4, 2
	h, ok := zstdHeader.Parse(data[off:], false)
	fhd := h["frame_header_descriptor"]
	if !ok || fhd&0b00011000 != 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	dictID := fhd & 0b11
	checksum := fhd&0b100 != 0
	single := fhd&0b100000 != 0
	content := (fhd & 0b11000000) >> 6
	next := off + zstdHeader.Size()
	if !single {
		next++
	}
	next += [4]int{0, 1, 2, 4}[dictID]
	switch {
	case content == 0 && single:
		next++
	case content == 1:
		next += 2
	case content == 2:
		next += 4
	case content == 3:
		next += 8
	}
	blocks, previous := 0, -1
	for binwalk.OffsetSafe(len(data), next, previous) {
		// parse_block_header
		b, ok := zstdBlock.Parse(data[next:], false)
		if !ok {
			break
		}
		info := b["info_bits"]
		typ := (info & 0b110) >> 1
		size := (info & 0xFFFFF8) >> 3
		if typ == 1 {
			size = 1 // RLE: one byte
		}
		if typ == 3 {
			break // reserved
		}
		blocks++
		previous = next
		next += zstdBlock.Size() + size
		if info&1 == 0 {
			continue
		}
		total := next - off
		if checksum {
			total += checksumSize
		}
		if blocks < minBlocks {
			break
		}
		return walk.Match{
			Offset:      off,
			Size:        total,
			Confidence:  walk.High,
			Description: fmt.Sprintf("ZSTD compressed data, total size: %d bytes", total),
		}, nil
	}
	return walk.Match{}, walk.ErrNoMatch
}

// extractZstd is zstd_decompress. It decodes the frame that the parser
// finds, so the bytes after the frame do not count. It fails when the frame
// ends past the data, and when it gives no data.
func extractZstd(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := parseZstd(data, off)
	if err != nil {
		return walk.Extraction{}
	}
	frame, ok := binwalk.Range(data, off, off+m.Size)
	if !ok {
		return walk.Extraction{}
	}
	s, ok := compress.LookupStream(compress.Zstd)
	if !ok {
		return walk.Extraction{}
	}
	w := &appender{out: out, name: "decompressed.bin"}
	if _, err := s(frame, w); err != nil || w.failed || w.n == 0 {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true, Size: m.Size, HasSize: true}
}
