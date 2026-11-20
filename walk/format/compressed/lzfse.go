//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/lzfse.rs of binwalk-ng. binwalk-ng
// extracts LZFSE with its own code, through the crate lzfse, which calls
// the reference code of Apple. The walker decodes the data with
// github.com/xo/magic/compress/lzfse, a port of that code (D68).

package compressed

import (
	"errors"
	"fmt"

	"github.com/xo/magic/compress/lzfse"
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// LZFSE is the signature "lzfse" of binwalk-ng.
var LZFSE = &walk.Signature{
	Name:        "lzfse",
	Magic:       [][]byte{[]byte("bvx-"), []byte("bvx1"), []byte("bvx2"), []byte("bvxn")},
	Description: "LZFSE compressed data",
	Parse:       parseLZFSE,
	Extract:     extractLZFSE,
}

// parseLZFSE is lzfse_parser. It reads the blocks to the block of the end
// of the stream.
func parseLZFSE(data []byte, off int) (walk.Match, error) {
	next, previous := off, -1
	for binwalk.OffsetSafe(len(data), next, previous) {
		previous = next
		header, size, eof, _, ok := lzfseBlock(data[next:])
		if !ok {
			continue // the next test of the loop fails, as in binwalk
		}
		next += header + size
		if eof {
			total := next - off
			return walk.Match{
				Offset:      off,
				Size:        total,
				Confidence:  walk.High,
				Description: fmt.Sprintf("LZFSE compressed data, total size: %d bytes", total),
			}, nil
		}
	}
	return walk.Match{}, walk.ErrNoMatch
}

var (
	lzfseType = binwalk.Struct{{Name: "block_type", Type: "u32"}}
	lzfseRaw  = binwalk.Struct{{Name: "magic", Type: "u32"}, {Name: "n_raw_bytes", Type: "u32"}}
	lzfseLZVN = binwalk.Struct{{Name: "magic", Type: "u32"}, {Name: "n_raw_bytes", Type: "u32"}, {Name: "n_payload_bytes", Type: "u32"}}
	lzfseV1   = binwalk.Struct{
		{Name: "magic", Type: "u32"},
		{Name: "n_raw_bytes", Type: "u32"},
		{Name: "n_payload_bytes", Type: "u32"},
		{Name: "n_literals", Type: "u32"},
		{Name: "n_matches", Type: "u32"},
		{Name: "n_literal_payload_bytes", Type: "u32"},
		{Name: "n_lmd_payload_bytes", Type: "u32"},
		{Name: "literal_bits", Type: "u32"},
		{Name: "literal_state", Type: "u64"},
		{Name: "lmd_bits", Type: "u32"},
		{Name: "l_state", Type: "u16"},
		{Name: "m_state", Type: "u16"},
		{Name: "d_state", Type: "u16"},
	}
	lzfseV2 = binwalk.Struct{
		{Name: "magic", Type: "u32"},
		{Name: "uncompressed_size", Type: "u32"},
		{Name: "packed_field_1", Type: "u64"},
		{Name: "packed_field_2", Type: "u64"},
		{Name: "header_size", Type: "u32"},
		{Name: "state_fields", Type: "u32"},
	}
)

// lzfseBlock is parse_lzfse_block_header: the size of the header and of
// the data of the block at the start of data, whether it ends the stream,
// and the bytes that it decodes to.
func lzfseBlock(data []byte) (header, size int, eof bool, raw int, ok bool) {
	t, ok := lzfseType.Parse(data, false)
	if !ok {
		return 0, 0, false, 0, false
	}
	switch t["block_type"] {
	case 0x24787662: // bvx$, the end of the stream
		return 4, 0, true, 0, true
	case 0x2d787662: // bvx-, data that is not compressed
		if h, ok := lzfseRaw.Parse(data, false); ok {
			return 8, h["n_raw_bytes"], false, h["n_raw_bytes"], true
		}
	case 0x31787662: // bvx1
		if h, ok := lzfseV1.Parse(data, false); ok {
			return 770, h["n_literal_payload_bytes"] + h["n_lmd_payload_bytes"], false, h["n_raw_bytes"], true
		}
	case 0x32787662: // bvx2
		if h, ok := lzfseV2.Parse(data, false); ok {
			const mask = 1<<20 - 1
			lmd := h["packed_field_2"] >> 40 & mask
			literal := h["packed_field_1"] >> 20 & mask
			return h["header_size"], lmd + literal, false, h["uncompressed_size"], true
		}
	case 0x6e787662: // bvxn, LZVN
		if h, ok := lzfseLZVN.Parse(data, false); ok {
			return 12, h["n_payload_bytes"], false, h["n_raw_bytes"], true
		}
	}
	return 0, 0, false, 0, false
}

// lzfseMaxOutput is MAX_DST_SIZE of lzfse_decompress: binwalk-ng reads no
// more blocks once their bytes would pass 100 MiB.
const lzfseMaxOutput = 100 << 20

// extractLZFSE is lzfse_decompress. It adds the sizes of the blocks up to
// the block of the end, and decodes those bytes into a buffer one byte
// larger than the sum of the sizes that the headers give. The data is valid
// when it decodes to exactly that sum.
func extractLZFSE(data []byte, off int, out *walk.Output) walk.Extraction {
	src := data[off:]
	rest, total := src, 0
	for {
		header, size, eof, raw, ok := lzfseBlock(rest)
		if !ok {
			break
		}
		block := header + size
		if block == 0 || total+raw > lzfseMaxOutput {
			break
		}
		total += raw
		if block > len(rest) {
			break
		}
		rest = rest[block:]
		if eof {
			break
		}
	}
	src = src[:len(src)-len(rest)]
	// lzfse_decode_buffer returns 0 when the data is damaged, and the size
	// of its buffer when the buffer is full. So damaged data is valid when
	// the headers give a sum of 0.
	b, err := lzfse.Decode(nil, src, total+1)
	n := len(b)
	switch {
	case errors.Is(err, lzfse.ErrLimit):
		n = total + 1
	case err != nil:
		n, b = 0, nil
	}
	if n != total {
		return walk.Extraction{}
	}
	if out != nil && !out.CreateFile("decompressed.bin", b) {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true, Size: total, HasSize: true}
}
