//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/vxworks.rs, src/structures/vxworks.rs
// and src/extractors/vxworks.rs of binwalk.

package exec

import (
	"encoding/json"
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// WindKernel is the signature "wind_kernel" of binwalk: the version string
// of a VxWorks WIND kernel.
var WindKernel = &walk.Signature{
	Name:        "wind_kernel",
	Magic:       [][]byte{[]byte("WIND version ")},
	Description: "VxWorks WIND kernel version",
	Parse:       parseWindKernel,
}

// VxWorksSymtab is the signature "vxworks_symtab" of binwalk.
var VxWorksSymtab = &walk.Signature{
	Name: "vxworks_symtab",
	// The type and the group of an entry of the symbol table, in both
	// orders of bytes.
	Magic: [][]byte{
		[]byte("\x00\x00\x05\x00\x00\x00\x00\x00"),
		[]byte("\x00\x00\x07\x00\x00\x00\x00\x00"),
		[]byte("\x00\x00\x09\x00\x00\x00\x00\x00"),
		[]byte("\x00\x05\x00\x00\x00\x00\x00\x00"),
		[]byte("\x00\x07\x00\x00\x00\x00\x00\x00"),
		[]byte("\x00\x09\x00\x00\x00\x00\x00\x00"),
	},
	Description: "VxWorks symbol table",
	Parse:       parseVxWorksSymtab,
	Extract:     extractVxWorksSymtab,
	NoRecurse:   true,
}

// parseWindKernel is wind_kernel_parser.
func parseWindKernel(data []byte, off int) (walk.Match, error) {
	const magicSize = 13
	b, ok := binwalk.From(data, off+magicSize)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	version := binwalk.CString(b)
	if version == "" {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        magicSize + len(version),
		Confidence:  walk.Low,
		Description: "VxWorks WIND kernel version " + version,
	}, nil
}

// parseVxWorksSymtab is symbol_table_parser. The magic is 8 bytes into the
// table.
func parseVxWorksSymtab(data []byte, off int) (walk.Match, error) {
	const magicOffset = 8
	if off < magicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - magicOffset
	size, ok := vxWorksSymtabSize(data, start)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      start,
		Size:        size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("VxWorks symbol table, total size: %d bytes", size),
	}, nil
}

var vxWorksSymtabEntry = binwalk.Struct{
	{Name: "name_ptr", Type: "u32"},
	{Name: "value_ptr", Type: "u32"},
	{Name: "type", Type: "u32"},
	{Name: "group", Type: "u32"},
}

// vxWorksSymtabSize is the dry run of extract_symbol_table.
func vxWorksSymtabSize(data []byte, off int) (int, bool) {
	r := extractVxWorksSymtab(data, off, nil)
	return r.Size, r.OK
}

// vxWorksSymbol is VxWorksSymbolTableEntry. The JSON of binwalk has its
// fields in this order.
type vxWorksSymbol struct {
	Size    int    `json:"size"`
	Name    int    `json:"name"`
	Value   int    `json:"value"`
	Symtype string `json:"symtype"`
}

// vxWorksSymbolTypes are the types of symbol that parse_symtab_entry allows.
var vxWorksSymbolTypes = map[int]string{
	0x500: "function",
	0x700: "initialized data",
	0x900: "uninitialized data",
}

// extractVxWorksSymtab is extract_symbol_table. It reads the entries of the
// symbol table at off up to the first one that is not valid, and accepts
// the table when it has at least 250 of them. It writes the entries to
// symtab.json, in the JSON of serde_json::to_string_pretty.
func extractVxWorksSymtab(data []byte, off int, out *walk.Output) walk.Extraction {
	const minEntries = 250
	var entries []vxWorksSymbol
	next, previous := off, -1
	// get_symtab_endianness: the type field is 0x00000500 or so, so the
	// byte at 9 is 0 in big endian order. binwalk reads no entry when the
	// byte is not there.
	if off+9 < len(data) {
		big := data[off+9] == 0
		for binwalk.OffsetSafe(len(data), next, previous) {
			// parse_symtab_entry
			e, ok := vxWorksSymtabEntry.Parse(data[next:], big)
			if !ok {
				break
			}
			typ, ok := vxWorksSymbolTypes[e["type"]]
			if !ok || e["name_ptr"] == 0 || e["value_ptr"] == 0 {
				break
			}
			entries = append(entries, vxWorksSymbol{
				Size:    vxWorksSymtabEntry.Size(),
				Name:    e["name_ptr"],
				Value:   e["value_ptr"],
				Symtype: typ,
			})
			previous = next
			next += vxWorksSymtabEntry.Size()
		}
	}
	if len(entries) < minEntries {
		return walk.Extraction{}
	}
	r := walk.Extraction{OK: true, Size: next - off, HasSize: true}
	if out != nil {
		b, err := json.MarshalIndent(entries, "", "  ")
		// The entries always convert. binwalk keeps the success of the
		// dry run when they do not.
		if err == nil {
			r.OK = out.CreateFile("symtab.json", b)
		}
	}
	return r
}
