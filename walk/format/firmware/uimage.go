//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/uimage.rs, src/structures/uimage.rs
// and src/extractors/uimage.rs of binwalk.

package firmware

import (
	"fmt"
	"strings"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// UImage is the signature "uimage" of binwalk.
var UImage = &walk.Signature{
	Name: "uimage",
	// The standard magic of uImage, and the magic of OpenWrt.
	Magic:       [][]byte{[]byte("\x27\x05\x19\x56"), []byte("OKLI")},
	Description: "uImage firmware image",
	Parse:       parseUImage,
	Extract:     extractUImage,
}

// parseUImage is uimage_parser.
func parseUImage(data []byte, off int) (walk.Match, error) {
	size, ok := uimageSize(data, off)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	h, ok := uimageHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	m := walk.Match{
		Offset:     off,
		Size:       size,
		Confidence: walk.High,
		Description: fmt.Sprintf("uImage firmware image, header size: %d bytes, data size: %d bytes, compression: %s, CPU: %s, OS: %s, image type: %s, load address: 0x%X, entry point: 0x%X, creation time: %s, image name: \"%s\"",
			h.headerSize, h.dataSize, h.compression, h.cpu, h.os, h.imageType,
			h.loadAddress, h.entryPoint, binwalk.EpochString(uint32(h.timestamp)), h.name),
		// A header with a bad CRC, or with no data, needs no extraction.
		Declined: !h.crcValid || h.dataSize == 0,
	}
	if !h.crcValid {
		// A header at the start of the file is still fairly sure.
		m.Confidence = walk.Low
		if off == 0 {
			m.Confidence = walk.Medium
		}
		m.Description += ", invalid checksum"
	}
	return m, nil
}

// uimageSize is the dry run of extract_uimage.
func uimageSize(data []byte, off int) (int, bool) {
	r := extractUImage(data, off, nil)
	return r.Size, r.OK
}

// extractUImage is extract_uimage. The size is that of the header, and of
// the data when its CRC is good. It writes the data to a file named after
// the image, with each space as "_", or to uimage_data.bin when the image
// has no name. It writes nothing when the CRC of the data is bad.
func extractUImage(data []byte, off int, out *walk.Output) walk.Extraction {
	hd, ok := binwalk.From(data, off)
	if !ok {
		return walk.Extraction{}
	}
	h, ok := uimageHeader(hd)
	if !ok {
		return walk.Extraction{}
	}
	start := off + h.headerSize
	image, ok := binwalk.Range(data, start, start+h.dataSize)
	if !ok {
		return walk.Extraction{}
	}
	r := walk.Extraction{OK: true, Size: h.headerSize, HasSize: true}
	crcValid := binwalk.CRC32(image) == uint32(h.dataCRC)
	if crcValid {
		r.Size += h.dataSize
	}
	if crcValid && out != nil {
		name := "uimage_data"
		if h.name != "" {
			name = strings.ReplaceAll(h.name, " ", "_")
		}
		r.OK = out.CreateFile(name+".bin", image)
	}
	return r
}

var uimageStruct = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "header_crc", Type: "u32"},
	{Name: "creation_timestamp", Type: "u32"},
	{Name: "data_size", Type: "u32"},
	{Name: "load_address", Type: "u32"},
	{Name: "entry_point_address", Type: "u32"},
	{Name: "data_crc", Type: "u32"},
	{Name: "os_type", Type: "u8"},
	{Name: "cpu_type", Type: "u8"},
	{Name: "image_type", Type: "u8"},
	{Name: "compression_type", Type: "u8"},
}

var uimageOS = map[int]string{
	1: "OpenBSD", 2: "NetBSD", 3: "FreeBSD", 4: "4.4BSD", 5: "Linux",
	6: "SVR4", 7: "Esix", 8: "Solaris", 9: "Irix", 10: "SCO", 11: "Dell",
	12: "NCR", 13: "LynxOS", 14: "VxWorks", 15: "pSOS", 16: "QNX",
	17: "Firmware", 18: "RTEMS", 19: "ARTOS", 20: "Unity OS",
	21: "INTEGRITY", 22: "OSE", 23: "Plan 9", 24: "OpenRTOS",
	25: "ARM Trusted Firmware", 26: "Trusted Execution Environment",
	27: "OpenSBI", 28: "EFI Firmware", 29: "ELF Image",
}

var uimageCPU = map[int]string{
	1: "Alpha", 2: "ARM", 3: "Intel x86", 4: "IA64", 5: "MIPS32",
	6: "MIPS64", 7: "PowerPC", 8: "IBM S390", 10: "SuperH", 11: "Sparc",
	12: "Sparc64", 13: "M68K", 14: "Nios-32", 15: "MicroBlaze",
	16: "Nios-II", 17: "Blackfin", 18: "AVR32", 19: "ST200", 20: "Sandbox",
	21: "NDS32", 22: "OpenRISC", 23: "ARM64", 24: "ARC", 25: "x86-64",
	26: "Xtensa", 27: "RISC-V",
}

var uimageCompression = map[int]string{
	0: "none", 1: "gzip", 2: "bzip2", 3: "lzma", 4: "lzo", 5: "lz4", 6: "zstd",
}

var uimageImageType = map[int]string{
	1:  "Standalone Program",
	2:  "OS Kernel Image",
	3:  "RAMDisk Image",
	4:  "Multi-File Image",
	5:  "Firmware Image",
	6:  "Script file",
	7:  "Filesystem Image",
	8:  "Binary Flat Device Tree Blob",
	9:  "Kirkwood Boot Image",
	10: "Freescale IMXBoot Image",
	11: "Davinci UBL Image",
	12: "TI OMAP Config Header Image",
	13: "TI Davinci AIS Image",
	14: "OS Kernel Image",
	15: "Freescale PBL Boot Image",
	16: "Freescale MXSBoot Image",
	17: "TI Keystone GPHeader Image",
	18: "ATMEL ROM bootable Image",
	19: "Altera SOCFPGA CV/AV Preloader",
	20: "x86 setup.bin Image",
	21: "x86 setup.bin Image",
	22: "A list of typeless images",
	23: "Rockchip Boot Image",
	24: "Rockchip SD card",
	25: "Rockchip SPI image",
	26: "Xilinx Zynq Boot Image",
	27: "Xilinx ZynqMP Boot Image",
	28: "Xilinx ZynqMP Boot Image (bif)",
	29: "FPGA Image",
	30: "VYBRID .vyb Image",
	31: "Trusted Execution Environment OS Image",
	32: "Firmware Image with HABv4 IVT",
	33: "TI Power Management Micro-Controller Firmware",
	34: "STMicroelectronics STM32 Image",
	35: "Altera SOCFPGA A10 Preloader",
	36: "MediaTek BootROM loadable Image",
	37: "Freescale IMX8MBoot Image",
	38: "Freescale IMX8Boot Image",
	39: "Coprocessor Image for remoteproc",
	40: "Allwinner eGON Boot Image",
	41: "Allwinner TOC0 Boot Image",
	42: "Binary Flat Device Tree Blob in a Legacy Image",
	43: "Renesas SPKG image",
	44: "StarFive SPL image",
}

type uimageInfo struct {
	headerSize  int
	name        string
	dataSize    int
	dataCRC     int
	loadAddress int
	entryPoint  int
	timestamp   int
	compression string
	cpu         string
	os          string
	imageType   string
	crcValid    bool
}

// uimageHeader is parse_uimage_header.
func uimageHeader(data []byte) (uimageInfo, bool) {
	const (
		headerSize = 64
		nameOffset = 32
	)
	h, ok := uimageStruct.Parse(data, true)
	if !ok {
		return uimageInfo{}, false
	}
	osName, ok1 := uimageOS[h["os_type"]]
	cpu, ok2 := uimageCPU[h["cpu_type"]]
	imageType, ok3 := uimageImageType[h["image_type"]]
	compression, ok4 := uimageCompression[h["compression_type"]]
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return uimageInfo{}, false
	}
	hdr, ok := binwalk.Range(data, 0, headerSize)
	if !ok {
		return uimageInfo{}, false
	}
	return uimageInfo{
		headerSize: headerSize,
		// The name runs to the first NUL, which can be past the header.
		name:        binwalk.CString(data[nameOffset:]),
		dataSize:    h["data_size"],
		dataCRC:     h["data_crc"],
		timestamp:   h["creation_timestamp"],
		loadAddress: h["load_address"],
		entryPoint:  h["entry_point_address"],
		compression: compression,
		cpu:         cpu,
		os:          osName,
		imageType:   imageType,
		crcValid:    uimageHeaderCRC(hdr) == h["header_crc"],
	}, true
}

// uimageHeaderCRC is calculate_uimage_header_checksum: the CRC of the
// header, with the field of the CRC as zeros.
func uimageHeaderCRC(hdr []byte) int {
	b := make([]byte, len(hdr))
	copy(b, hdr)
	clear(b[4:8])
	return int(binwalk.CRC32(b))
}
