//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/dmg.rs and src/structures/dmg.rs of
// binwalk.

package disk

import (
	"bytes"
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// DMG is the signature "dmg" of binwalk. The magic is the start of the
// footer: the magic, version 4 and the size of the footer, 0x200. A DMG has
// no header of a standard format.
var DMG = &walk.Signature{
	Name:        "dmg",
	Magic:       [][]byte{[]byte("koly\x00\x00\x00\x04\x00\x00\x02\x00")},
	Description: "Apple Disk iMaGe",
	Parse:       parseDMG,
}

// parseDMG is dmg_parser. A DMG holds the image data, then the XML data,
// then the footer. A signed DMG has more data of variable size before the
// footer, so binwalk finds the start from the XML data, which it searches
// for in all of the data. The sum of the lengths wraps as the usize of
// binwalk does.
func parseDMG(data []byte, off int) (walk.Match, error) {
	h, ok := dmgFooter.Parse(data[off:], true)
	if !ok || h["header_size"] != dmgFooter.Size() {
		return walk.Match{}, walk.ErrNoMatch
	}
	dataLen := u64(h, "data_fork_length")
	if dataLen+u64(h, "xml_length") > uint64(off) {
		return walk.Match{}, walk.ErrNoMatch
	}
	xml, ok := findXMLPropertyList(data)
	if !ok || uint64(xml) < dataLen {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := off + dmgFooter.Size()
	return walk.Match{
		Offset: xml - int(dataLen),
		Size:   size,
		// binwalk adds 1 to high, so that a DMG wins over the compressed
		// data that it starts with.
		Confidence:  walk.High + 1,
		Description: fmt.Sprintf("Apple Disk iMaGe, total size: %d bytes", size),
	}, nil
}

// findXMLPropertyList is find_xml_property_list: the first "<?xml" that
// starts 1024 bytes of valid UTF-8 that hold the key "blkx".
func findXMLPropertyList(data []byte) (int, bool) {
	const minLength = 1024
	sig := []byte("<?xml")
	for i := 0; ; i++ {
		j := bytes.Index(data[i:], sig)
		if j < 0 {
			return 0, false
		}
		i += j
		b, ok := binwalk.Range(data, i, i+minLength)
		if ok && utf8.Valid(b) && bytes.Contains(b, []byte("<key>blkx</key>")) {
			return i, true
		}
	}
}

// dmgFooter is the footer of a DMG, in parse_dmg_footer.
var dmgFooter = func() binwalk.Struct {
	s := binwalk.Struct{
		{Name: "magic", Type: "u32"},
		{Name: "version", Type: "u32"},
		{Name: "header_size", Type: "u32"},
		{Name: "flags", Type: "u32"},
		{Name: "running_data_fork_offset", Type: "u64"},
		{Name: "data_fork_offset", Type: "u64"},
		{Name: "data_fork_length", Type: "u64"},
		{Name: "rsrc_fork_offset", Type: "u64"},
		{Name: "rsrc_fork_length", Type: "u64"},
		{Name: "segment_number", Type: "u32"},
		{Name: "segment_count", Type: "u32"},
		{Name: "segment_id_p1", Type: "u64"},
		{Name: "segment_id_p2", Type: "u64"},
		{Name: "data_checksum_type", Type: "u32"},
		{Name: "data_checksum_size", Type: "u32"},
	}
	for i := 1; i <= 32; i++ {
		s = append(s, binwalk.Field{Name: "data_checksum_" + strconv.Itoa(i), Type: "u32"})
	}
	s = append(s,
		binwalk.Field{Name: "xml_offset", Type: "u64"},
		binwalk.Field{Name: "xml_length", Type: "u64"},
	)
	for i := 1; i <= 15; i++ {
		s = append(s, binwalk.Field{Name: "reserved_" + strconv.Itoa(i), Type: "u64"})
	}
	s = append(s,
		binwalk.Field{Name: "checksum_type", Type: "u32"},
		binwalk.Field{Name: "checksum_size", Type: "u32"},
	)
	for i := 1; i <= 32; i++ {
		s = append(s, binwalk.Field{Name: "checksum_" + strconv.Itoa(i), Type: "u32"})
	}
	return append(s,
		binwalk.Field{Name: "image_variant", Type: "u32"},
		binwalk.Field{Name: "sector_count", Type: "u64"},
		binwalk.Field{Name: "reserved_16", Type: "u32"},
		binwalk.Field{Name: "reserved_17", Type: "u32"},
		binwalk.Field{Name: "reserved_18", Type: "u32"},
	)
}()
