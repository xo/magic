//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/pe.rs and src/structures/pe.rs of
// binwalk.

package exec

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// PE is the signature "pe" of binwalk.
var PE = &walk.Signature{
	Name: "pe",
	// The first 16 bytes of a DOS header, from e_magic to e_ss, as most
	// files have them.
	Magic: [][]byte{
		[]byte("\x4d\x5a\x90\x00\x03\x00\x00\x00\x04\x00\x00\x00\xff\xff\x00\x00"),
		[]byte("\x4d\x5a\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"),
	},
	Description: "Windows PE binary",
	Parse:       parsePE,
}

// parsePE is pe_parser.
func parsePE(data []byte, off int) (walk.Match, error) {
	machine, ok := parsePEHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Confidence:  walk.Medium,
		Description: "Windows PE binary, machine type: " + machine,
	}, nil
}

var peDOSHeader = func() binwalk.Struct {
	s := binwalk.Struct{
		{Name: "e_magic", Type: "u16"},
		{Name: "e_cblp", Type: "u16"},
		{Name: "e_cp", Type: "u16"},
		{Name: "e_crlc", Type: "u16"},
		{Name: "e_cparhdr", Type: "u16"},
		{Name: "e_minalloc", Type: "u16"},
		{Name: "e_maxalloc", Type: "u16"},
		{Name: "e_ss", Type: "u16"},
		{Name: "e_sp", Type: "u16"},
		{Name: "e_csum", Type: "u16"},
		{Name: "e_ip", Type: "u16"},
		{Name: "e_cs", Type: "u16"},
		{Name: "e_lfarlc", Type: "u16"},
		{Name: "e_ovno", Type: "u16"},
	}
	for i := 1; i <= 4; i++ {
		s = append(s, binwalk.Field{Name: fmt.Sprintf("e_res_%d", i), Type: "u16"})
	}
	s = append(s, binwalk.Field{Name: "e_oemid", Type: "u16"}, binwalk.Field{Name: "e_oeminfo", Type: "u16"})
	for i := 5; i <= 14; i++ {
		s = append(s, binwalk.Field{Name: fmt.Sprintf("e_res_%d", i), Type: "u16"})
	}
	return append(s, binwalk.Field{Name: "e_lfanew", Type: "u32"})
}()

var peHeader = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "machine", Type: "u16"},
	{Name: "number_of_sections", Type: "u16"},
	{Name: "timestamp", Type: "u32"},
	{Name: "symbol_table_ptr", Type: "u32"},
	{Name: "number_of_symbols", Type: "u32"},
	{Name: "optional_header_size", Type: "u16"},
	{Name: "characteristics", Type: "u16"},
}

var peMachines = map[int]string{
	0:      "Unknown",
	0x184:  "Alpha32",
	0x284:  "Alpha64",
	0x1D3:  "Matsushita AM33",
	0x8664: "Intel x86-64",
	0x1C0:  "ARM",
	0xAA64: "ARM-64",
	0x1C4:  "ARM Thumb2",
	0xEBC:  "EFI",
	0x14C:  "Intel x86",
	0x200:  "Intel Itanium",
	0x6232: "LoongArch 32-bit",
	0x6264: "LoongArch 64-bit",
	0x9041: "Mitsubishi M32R",
	0x266:  "MIPS16",
	0x366:  "MIPS with FPU",
	0x466:  "MIPS16 with FPU",
	0x1F0:  "PowerPC",
	0x1F1:  "PowerPC with FPU",
	0x5032: "RISC-V 32-bit",
	0x5064: "RISC-V 64-bit",
	0x5128: "RISC-V 128-bit",
	0x1A2:  "Hitachi SH3",
	0x1A3:  "Hitachi SH3 DSP",
	0x1A6:  "Hitachi SH4",
	0x1A8:  "Hitachi SH5",
	0x1C2:  "Thumb",
	0x169:  "MIPS WCEv2",
}

// parsePEHeader is parse_pe_header. It returns the name of the machine.
func parsePEHeader(data []byte) (string, bool) {
	const peMagic = 0x00004550
	dos, ok := peDOSHeader.Parse(data, false)
	if !ok {
		return "", false
	}
	// The reserved fields are all 0.
	for i := 1; i <= 14; i++ {
		if dos[fmt.Sprintf("e_res_%d", i)] != 0 {
			return "", false
		}
	}
	start := dos["e_lfanew"]
	raw, ok := binwalk.Range(data, start, start+peHeader.Size())
	if !ok {
		return "", false
	}
	h, ok := peHeader.Parse(raw, false)
	if !ok || h["magic"] != peMagic {
		return "", false
	}
	machine, ok := peMachines[h["machine"]]
	return machine, ok
}
