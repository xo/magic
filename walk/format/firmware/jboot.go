//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/jboot.rs of binwalk-ng.

package firmware

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/format/internal/jbootsig"
	"github.com/xo/magic/walk/internal/binwalk"
)

// JBOOTARM is the signature "jboot_arm" of binwalk-ng.
var JBOOTARM = &walk.Signature{
	Name:        "jboot_arm",
	Magic:       [][]byte{[]byte("\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x42\x48")},
	Description: "JBOOT firmware header",
	Parse:       parseJBOOTARM,
}

// JBOOTSTAG is the signature "jboot_stag" of binwalk-ng.
var JBOOTSTAG = &walk.Signature{
	Name:        "jboot_stag",
	Magic:       [][]byte{[]byte("\x04\x04\x24\x2B"), []byte("\xFF\x04\x24\x2B")},
	Description: "JBOOT STAG header",
	Parse:       parseJBOOTSTAG,
}

// JBOOTSCH2 is the signature "jboot_sch2" of binwalk-ng.
var JBOOTSCH2 = &walk.Signature{
	Name: "jboot_sch2",
	Magic: [][]byte{
		[]byte("\x24\x21\x00\x02"),
		[]byte("\x24\x21\x01\x02"),
		[]byte("\x24\x21\x02\x02"),
		[]byte("\x24\x21\x03\x02"),
	},
	Description: "JBOOT SCH2 header",
	Parse:       parseJBOOTSCH2,
	Extract:     extractJBOOTSCH2,
}

// parseJBOOTARM is jboot_arm_parser. The magic is 48 bytes into the header.
func parseJBOOTARM(data []byte, off int) (walk.Match, error) {
	const magicOffset = 48
	// binwalk subtracts in a usize. Before 48 bytes, it wraps, and the slice
	// that follows is out of range, so binwalk rejects the candidate too.
	if off < magicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - magicOffset
	h, ok := jbootsig.ParseARM(data[start:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     start,
		Size:       h.HeaderSize,
		Confidence: walk.Medium,
		Description: fmt.Sprintf("JBOOT firmware header, header size: %d bytes, ROM ID: %s, erase offset: 0x%X, erase size: 0x%X, data flash offset: 0x%X, data size: 0x%X",
			h.HeaderSize, h.RomID, h.EraseOffset, h.EraseSize, h.DataOffset, h.DataSize),
	}, nil
}

// parseJBOOTSTAG is jboot_stag_parser.
func parseJBOOTSTAG(data []byte, off int) (walk.Match, error) {
	h, ok := jbootSTAGHeader(data[off:])
	// The header describes a kernel, which must fit in the data.
	if !ok || off+h.headerSize+h.imageSize > len(data) {
		return walk.Match{}, walk.ErrNoMatch
	}
	kind := "factory image"
	if h.sysupgrade {
		kind = "system upgrade image"
	}
	return walk.Match{
		Offset: off,
		// The size is only the header, because the confidence is low.
		Size:       h.headerSize,
		Confidence: walk.Low,
		Description: fmt.Sprintf("JBOOT STAG header, %s, header size: %d bytes, kernel data size: %d bytes",
			kind, h.headerSize, h.imageSize),
	}, nil
}

var jbootSTAGStruct = binwalk.Struct{
	{Name: "cmark", Type: "u8"},
	{Name: "id", Type: "u8"},
	{Name: "magic", Type: "u16"},
	{Name: "timestamp", Type: "u32"},
	{Name: "image_size", Type: "u32"},
	{Name: "image_checksum", Type: "u16"},
	{Name: "header_checksum", Type: "u16"},
}

type jbootSTAGInfo struct {
	headerSize int
	imageSize  int
	sysupgrade bool
}

// jbootSTAGHeader is parse_jboot_stag_header. The cmark of a factory image
// is 0xFF, and the cmark of a system upgrade image is its id.
func jbootSTAGHeader(data []byte) (jbootSTAGInfo, bool) {
	const factory = 0xFF
	h, ok := jbootSTAGStruct.Parse(data, false)
	if !ok {
		return jbootSTAGInfo{}, false
	}
	r := jbootSTAGInfo{headerSize: jbootSTAGStruct.Size(), imageSize: h["image_size"]}
	if r.imageSize <= r.headerSize {
		return jbootSTAGInfo{}, false
	}
	r.sysupgrade = h["cmark"] == h["id"]
	if h["cmark"] != factory && !r.sysupgrade {
		return jbootSTAGInfo{}, false
	}
	return r, true
}

// parseJBOOTSCH2 is jboot_sch2_parser.
func parseJBOOTSCH2(data []byte, off int) (walk.Match, error) {
	size, ok := jbootSCH2Size(data, off)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	h, ok := jbootSCH2Header(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       size,
		Confidence: walk.High,
		Description: fmt.Sprintf("JBOOT SCH2 header, header size: %d bytes, kernel size: %d bytes, kernel compression: %s, kernel entry point: 0x%X",
			h.headerSize, h.kernelSize, h.compression, h.entryPoint),
	}, nil
}

// jbootSCH2Size is the dry run of extract_jboot_sch2_kernel.
func jbootSCH2Size(data []byte, off int) (int, bool) {
	r := extractJBOOTSCH2(data, off, nil)
	return r.Size, r.OK
}

// extractJBOOTSCH2 is extract_jboot_sch2_kernel. It accepts the header and
// the kernel when the CRC of the kernel is good, and writes the kernel to
// kernel.bin.
func extractJBOOTSCH2(data []byte, off int, out *walk.Output) walk.Extraction {
	hd, ok := binwalk.From(data, off)
	if !ok {
		return walk.Extraction{}
	}
	h, ok := jbootSCH2Header(hd)
	if !ok {
		return walk.Extraction{}
	}
	start := off + h.headerSize
	kernel, ok := binwalk.Range(data, start, start+h.kernelSize)
	if !ok || binwalk.CRC32(kernel) != uint32(h.kernelCRC) {
		return walk.Extraction{}
	}
	r := walk.Extraction{OK: true, Size: h.headerSize + h.kernelSize, HasSize: true}
	if out != nil {
		r.OK = out.CarveFile("kernel.bin", data, start, h.kernelSize)
	}
	return r
}

var jbootSCH2Struct = binwalk.Struct{
	{Name: "magic", Type: "u16"},
	{Name: "compression_type", Type: "u8"},
	{Name: "version", Type: "u8"},
	{Name: "ram_entry_address", Type: "u32"},
	{Name: "kernel_image_size", Type: "u32"},
	{Name: "kernel_image_crc", Type: "u32"},
	{Name: "ram_start_address", Type: "u32"},
	{Name: "rootfs_flash_address", Type: "u32"},
	{Name: "rootfs_size", Type: "u32"},
	{Name: "rootfs_crc", Type: "u32"},
	{Name: "header_crc", Type: "u32"},
	{Name: "header_size", Type: "u16"},
	{Name: "cmd_line_size", Type: "u16"},
}

var jbootSCH2Compression = map[int]string{0: "none", 1: "jz", 2: "gzip", 3: "lzma"}

type jbootSCH2Info struct {
	headerSize  int
	compression string
	kernelSize  int
	entryPoint  int
	kernelCRC   int
}

// jbootSCH2Header is parse_jboot_sch2_header.
func jbootSCH2Header(data []byte) (jbootSCH2Info, bool) {
	const version = 2
	size := jbootSCH2Struct.Size()
	h, ok := jbootSCH2Struct.Parse(data, false)
	if !ok || h["version"] != version || h["header_size"] != size {
		return jbootSCH2Info{}, false
	}
	compression, ok := jbootSCH2Compression[h["compression_type"]]
	if !ok {
		return jbootSCH2Info{}, false
	}
	hdr, ok := binwalk.Range(data, 0, h["header_size"])
	if !ok || jbootSCH2HeaderCRC(hdr) != h["header_crc"] {
		return jbootSCH2Info{}, false
	}
	return jbootSCH2Info{
		headerSize:  size,
		compression: compression,
		kernelSize:  h["kernel_image_size"],
		entryPoint:  h["ram_entry_address"],
		kernelCRC:   h["kernel_image_crc"],
	}, true
}

// jbootSCH2HeaderCRC is sch2_header_crc: the CRC of the header, with the
// field of the CRC as zeros, or 0 when the header is too short.
func jbootSCH2HeaderCRC(hdr []byte) int {
	const (
		crcStart = 32
		crcEnd   = 36
	)
	if len(hdr) <= crcEnd {
		return 0
	}
	b := make([]byte, len(hdr))
	copy(b, hdr)
	clear(b[crcStart:crcEnd])
	return int(binwalk.CRC32(b))
}
