//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/linux.rs and src/structures/linux.rs of
// binwalk.

package exec

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// LinuxKernel is the signature "linux_kernel" of binwalk: the version string
// of a Linux kernel.
var LinuxKernel = &walk.Signature{
	Name:        "linux_kernel",
	Magic:       [][]byte{[]byte("Linux version ")},
	Description: "Linux kernel version",
	Parse:       parseLinuxKernel,
}

// LinuxBootImage is the signature "linux_boot_image" of binwalk.
var LinuxBootImage = &walk.Signature{
	Name:        "linux_boot_image",
	Magic:       [][]byte{[]byte("\xb8\xc0\x07\x8e\xd8\xb8\x00\x90\x8e\xc0\xb9\x00\x01\x29\xf6\x29")},
	Description: "Linux kernel boot image",
	Parse:       parseLinuxBootImage,
}

// LinuxARMZImage is the signature "linux_arm_zimage" of binwalk.
var LinuxARMZImage = &walk.Signature{
	Name:        "linux_arm_zimage",
	Magic:       [][]byte{[]byte("\x18\x28\x6F\x01"), []byte("\x01\x6F\x28\x18")},
	Description: "Linux ARM boot executable zImage",
	Parse:       parseLinuxARMZImage,
}

// LinuxARM64BootImage is the signature "linux_arm64_boot_image" of binwalk.
var LinuxARM64BootImage = &walk.Signature{
	Name:        "linux_arm64_boot_image",
	Magic:       [][]byte{[]byte("\x00\x00\x00\x00\x00\x00\x00\x00ARMd")},
	Description: "Linux kernel ARM64 boot image",
	Parse:       parseLinuxARM64BootImage,
}

// parseLinuxARMZImage is linux_arm_zimage_parser, with
// parse_linux_arm_zimage_header. The magic is 36 bytes into the image,
// after 8 nop instructions.
func parseLinuxARMZImage(data []byte, off int) (walk.Match, error) {
	const (
		magicOffset = 36
		nopLE       = 0xE1A00000
		nopBE       = 0x0000A0E1
	)
	if off < magicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - magicOffset
	nops, ok := zImageNops.Parse(data[start:], false)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	nop := nops["nop1"]
	for _, f := range zImageNops {
		if nops[f.Name] != nop {
			return walk.Match{}, walk.ErrNoMatch
		}
	}
	var endian string
	switch nop {
	case nopLE:
		endian = "little"
	case nopBE:
		endian = "big"
	default:
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      start,
		Confidence:  walk.Medium,
		Description: "Linux ARM boot executable zImage, " + endian + " endian",
	}, nil
}

var zImageNops = binwalk.Struct{
	{Name: "nop1", Type: "u32"},
	{Name: "nop2", Type: "u32"},
	{Name: "nop3", Type: "u32"},
	{Name: "nop4", Type: "u32"},
	{Name: "nop5", Type: "u32"},
	{Name: "nop6", Type: "u32"},
	{Name: "nop7", Type: "u32"},
	{Name: "nop8", Type: "u32"},
}

var arm64BootHeader = binwalk.Struct{
	{Name: "code0", Type: "u32"},
	{Name: "code1", Type: "u32"},
	{Name: "image_load_offset", Type: "u64"},
	{Name: "image_size", Type: "u64"},
	{Name: "flags", Type: "u64"},
	{Name: "reserved1", Type: "u64"},
	{Name: "reserved2", Type: "u64"},
	{Name: "reserved3", Type: "u64"},
	{Name: "magic", Type: "u32"},
	{Name: "pe_offset", Type: "u32"},
}

// parseLinuxARM64BootImage is linux_arm64_boot_image_parser, with
// parse_linux_arm64_boot_image_header. The magic is 0x30 bytes into the
// image.
func parseLinuxARM64BootImage(data []byte, off int) (walk.Match, error) {
	const (
		magicOffset   = 0x30
		flagsReserved = 0xFFFFFFFFFFFFFFF0
		flagsBig      = 1
	)
	if off < magicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - magicOffset
	img := data[start:]
	h, ok := arm64BootHeader.Parse(img, false)
	if !ok || h["reserved1"] != 0 || h["reserved2"] != 0 || h["reserved3"] != 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	// There is a PE header at pe_offset.
	pe, ok := binwalk.Range(img, h["pe_offset"], h["pe_offset"]+2)
	if !ok || string(pe) != "PE" {
		return walk.Match{}, walk.ErrNoMatch
	}
	// Parse holds a u64 in an int, so the fields of 64 bits go back to
	// uint64 here.
	flags := uint64(h["flags"])
	if flags&flagsReserved != 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	endian := "little"
	if flags&flagsBig != 0 {
		endian = "big"
	}
	return walk.Match{
		Offset:     start,
		Size:       arm64BootHeader.Size(),
		Confidence: walk.Medium,
		Description: fmt.Sprintf("Linux kernel ARM64 boot image, %s endian, effective image size: %d bytes",
			endian, uint64(h["image_size"])),
	}, nil
}

// parseLinuxBootImage is linux_boot_image_parser. The string "!HdrS" is 514
// bytes after the magic.
func parseLinuxBootImage(data []byte, off int) (walk.Match, error) {
	const hdrsOffset = 514
	b, ok := binwalk.Range(data, off+hdrsOffset, off+hdrsOffset+5)
	if !ok || string(b) != "!HdrS" {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Confidence:  walk.Low,
		Description: "Linux kernel boot image",
	}, nil
}

// parseLinuxKernel is linux_kernel_version_parser. The version string is
// like this:
//
//	Linux version 4.9.241 (root@server2) (gcc version 10.0.1 ...) #755 SMP Wed Nov 4 03:59:02 +03 2020\n
func parseLinuxKernel(data []byte, off int) (walk.Match, error) {
	const (
		minFileSize      = 100 * 1024
		minVersionLength = 75
	)
	// The size of the file removes small text files that have the magic.
	if len(data) <= minFileSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	v := binwalk.CString(data[off:])
	if len(v) <= minVersionLength || !strings.Contains(v, "gcc ") || !strings.Contains(v, "@") || !strings.HasSuffix(v, "\n") {
		return walk.Match{}, walk.ErrNoMatch
	}
	// The version has periods at the expected places.
	if v[15] != '.' || (v[17] != '.' && v[18] != '.') {
		return walk.Match{}, walk.ErrNoMatch
	}
	m := walk.Match{Offset: off, Confidence: walk.Low}
	// With a symbol table, the whole file is a raw kernel, for
	// vmlinux-to-elf. Without one, the match is the version string only,
	// and needs no extraction.
	symtab := hasLinuxSymbolTable(data)
	if symtab {
		m.Offset, m.Size = 0, len(data)
	} else {
		m.Size, m.Declined = len(v), true
	}
	// strings.TrimSpace and str::trim of Rust both trim the Unicode
	// White_Space characters.
	m.Description = fmt.Sprintf("%s, has symbol table: %t", strings.TrimSpace(v), symtab)
	return m, nil
}

// hasLinuxSymbolTable is has_linux_symbol_table: it reports whether the
// magic of a symbol table, which vmlinux-to-elf looks for too, is in data
// once. The matches can overlap, as with find_overlapping_iter.
func hasLinuxSymbolTable(data []byte) bool {
	magic := []byte("\x000\x001\x002\x003\x004\x005\x006\x007\x008\x009\x00")
	n := 0
	for i := 0; ; i++ {
		j := bytes.Index(data[i:], magic)
		if j < 0 {
			break
		}
		if n++; n > 1 {
			return false
		}
		i += j
	}
	return n == 1
}
