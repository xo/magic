//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/trx.rs of binwalk-ng.

package firmware

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// TRX is the signature "trx" of binwalk-ng.
var TRX = &walk.Signature{
	Name:        "trx",
	Magic:       [][]byte{[]byte("HDR0")},
	Description: "TRX firmware image",
	Parse:       parseTRX,
	Extract:     extractTRX,
}

// parseTRX is trx_parser.
func parseTRX(data []byte, off int) (walk.Match, error) {
	size, ok := trxSize(data, off)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	h, ok := trxHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       size,
		Confidence: walk.High,
		Description: fmt.Sprintf("TRX firmware image, version %d, partition count: %d, header size: %d bytes, total size: %d bytes",
			h.version, len(h.partitions), h.headerSize, size),
	}, nil
}

// trxSize is the dry run of extract_trx_partitions.
func trxSize(data []byte, off int) (int, bool) {
	r := extractTRX(data, off, nil)
	return r.Size, r.OK
}

// extractTRX is extract_trx_partitions. It accepts the image when the CRC
// of the data is good, and writes each partition to partition_<n>.bin. A
// partition runs to the next one, and the last one to the end of the image.
func extractTRX(data []byte, off int, out *walk.Output) walk.Extraction {
	const crcStart = 12
	hd, ok := binwalk.From(data, off)
	if !ok {
		return walk.Extraction{}
	}
	h, ok := trxHeader(hd)
	if !ok {
		return walk.Extraction{}
	}
	start := off + crcStart
	b, ok := binwalk.Range(data, start, start+h.totalSize-crcStart)
	if !ok {
		return walk.Extraction{}
	}
	// trx_crc32: the CRC of zlib with its final XOR undone.
	if int(binwalk.CRC32(b)^0xFFFFFFFF) != h.checksum {
		return walk.Extraction{}
	}
	r := walk.Extraction{OK: true, Size: h.totalSize, HasSize: true}
	if out == nil {
		return r
	}
	for i, p := range h.partitions {
		size := h.totalSize - p
		if i+1 < len(h.partitions) {
			size = h.partitions[i+1] - p
		}
		// The partitions need not be in order. A partition after this one
		// at a lower offset gives a negative size, and binwalk-ng stops.
		if size < 0 {
			r.OK = false
			break
		}
		r.OK = out.CarveFile(fmt.Sprintf("partition_%d.bin", i), data, off+p, size)
		if !r.OK {
			break
		}
	}
	return r
}

var trxStruct = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "total_size", Type: "u32"},
	{Name: "crc32", Type: "u32"},
	{Name: "flags", Type: "u16"},
	{Name: "version", Type: "u16"},
	{Name: "partition1_offset", Type: "u32"},
	{Name: "partition2_offset", Type: "u32"},
	{Name: "partition3_offset", Type: "u32"},
	{Name: "partition4_offset", Type: "u32"},
}

type trxInfo struct {
	version    int
	checksum   int
	totalSize  int
	headerSize int
	partitions []int // the offsets of the partitions that are not 0
}

// trxHeader is parse_trx_header.
func trxHeader(data []byte) (trxInfo, bool) {
	h, ok := trxStruct.Parse(data, false)
	if !ok {
		return trxInfo{}, false
	}
	total := h["total_size"]
	if h["partition1_offset"] > total || h["partition2_offset"] > total || h["partition3_offset"] > total {
		return trxInfo{}, false
	}
	// Only version 2 has a fourth partition. For version 1, those bytes are
	// after the header, and binwalk-ng does not check them.
	version := h["version"]
	if version == 2 && h["partition4_offset"] > total {
		return trxInfo{}, false
	}
	size := trxStruct.Size()
	if total <= size {
		return trxInfo{}, false
	}
	if version != 1 && version != 2 {
		return trxInfo{}, false
	}
	var partitions []int
	for _, name := range []string{"partition1_offset", "partition2_offset", "partition3_offset"} {
		if h[name] != 0 {
			partitions = append(partitions, h[name])
		}
	}
	if version == 2 {
		// Only version 2 has a fourth partition.
		if h["partition4_offset"] != 0 {
			partitions = append(partitions, h["partition4_offset"])
		}
	} else {
		// The header of version 1 has no field for a fourth partition.
		size -= 4
	}
	return trxInfo{
		version:    version,
		checksum:   h["crc32"],
		totalSize:  total,
		headerSize: size,
		partitions: partitions,
	}, true
}
