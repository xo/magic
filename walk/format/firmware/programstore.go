//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/program_store.rs of binwalk-ng.

package firmware

import (
	"encoding/binary"
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// The layout of ProgramStoreHeaderRaw: the offsets of its fields, and its
// size.
const (
	psNameOffset  = 20
	psPadOffset   = 68
	psLen1Offset  = 76
	psLen2Offset  = 80
	psHCSOffset   = 84
	psResOffset   = 86
	psHeaderSize  = 92
	psNameSize    = 48
	psMagicOffset = psPadOffset - 1
)

// ProgramStore is the signature "program_store" of binwalk-ng: a Broadcom
// ProgramStore firmware image.
var ProgramStore = &walk.Signature{
	Name: "program_store",
	// The magic is the NUL that ends the name, and the 8 bytes of padding
	// after it, all zeros.
	Magic:       [][]byte{make([]byte, 9)},
	Description: "Broadcom ProgramStore firmware image",
	Parse:       parseProgramStore,
	Extract:     extractProgramStore,
	// The first byte of the name, which is not 0, is 47 bytes before the
	// magic. This is NONZERO_BEFORE_MAGIC of binwalk-ng.
	ZeroMagicNonZero: psMagicOffset - psNameOffset,
}

// psCompression is the Display of Compression, by the value of the field.
var psCompression = []string{"none", "LZ", "LZO", "reserved", "NRV2B", "LZMA"}

// programStoreHeader is ProgramStoreHeader. The payload is split when split
// is true.
type programStoreHeader struct {
	sig         int
	compression string
	split       bool
	len, len1   int
	len2        int
	major       int
	minor       int
	timestamp   uint32
	totalLen    int
	loadAddress int
	filename    string
}

// parseProgramStore is program_store_parser.
func parseProgramStore(data []byte, off int) (walk.Match, error) {
	if off < psMagicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - psMagicOffset
	h, ok := parseProgramStoreHeader(data[start:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	desc := fmt.Sprintf("Broadcom ProgramStore firmware image, signature: 0x%04X, compression: %s, load address: 0x%08X, revision: %d.%d, build time: %s, filename: \"%s\"",
		h.sig, h.compression, h.loadAddress, h.major, h.minor, binwalk.EpochString(h.timestamp), h.filename)
	if h.split {
		desc += ", split image"
	}
	return walk.Match{
		Offset:      start,
		Size:        h.totalLen,
		Confidence:  walk.High,
		Description: desc,
	}, nil
}

// crc16GENIBUS is crc16_genibus: CRC-16/GENIBUS.
func crc16GENIBUS(data []byte) int {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return int(crc ^ 0xFFFF)
}

// parseProgramStoreHeader is parse_program_store_header.
func parseProgramStoreHeader(data []byte) (programStoreHeader, bool) {
	// The first release of eCos, 1998-09-01. A build time before it is not
	// valid, but 0 is.
	const minTimestamp = 904_608_000
	if len(data) < psHeaderSize {
		return programStoreHeader{}, false
	}
	be := binary.BigEndian
	payloadLen := int(be.Uint32(data[12:]))
	if payloadLen == 0 || payloadLen > len(data)-psHeaderSize {
		return programStoreHeader{}, false
	}
	// The name is not empty, and ends with a NUL.
	name := data[psNameOffset : psNameOffset+psNameSize]
	if name[0] == 0 || name[psNameSize-1] != 0 {
		return programStoreHeader{}, false
	}
	split, comp := data[2], data[3]
	if int(comp) >= len(psCompression) || split > 1 {
		return programStoreHeader{}, false
	}
	for _, b := range data[psPadOffset:psLen1Offset] {
		if b != 0 {
			return programStoreHeader{}, false
		}
	}
	if be.Uint16(data[psResOffset:]) != 0 {
		return programStoreHeader{}, false
	}
	addr := int(be.Uint32(data[16:]))
	if addr&0x3 != 0 {
		return programStoreHeader{}, false
	}
	t := be.Uint32(data[8:])
	if t != 0 && t < minTimestamp {
		return programStoreHeader{}, false
	}
	len1 := int(be.Uint32(data[psLen1Offset:]))
	len2 := int(be.Uint32(data[psLen2Offset:]))
	if split == 0 {
		// A single payload. len1 is 0 or the length, and len2 is 0.
		if len2 != 0 || (len1 != 0 && len1 != payloadLen) {
			return programStoreHeader{}, false
		}
	} else if len1+len2 > payloadLen {
		return programStoreHeader{}, false
	}
	if crc16GENIBUS(data[:psHCSOffset]) != int(be.Uint16(data[psHCSOffset:])) {
		return programStoreHeader{}, false
	}
	return programStoreHeader{
		sig:         int(be.Uint16(data)),
		compression: psCompression[comp],
		split:       split == 1,
		len:         payloadLen,
		len1:        len1,
		len2:        len2,
		major:       int(be.Uint16(data[4:])),
		minor:       int(be.Uint16(data[6:])),
		timestamp:   t,
		totalLen:    psHeaderSize + payloadLen,
		loadAddress: addr,
		filename:    binwalk.CString(name),
	}, true
}

// extractProgramStore is extract_program_store. It writes the payload to a
// file named after the image. A split payload goes to two files, with ".1"
// and ".2" after the name. The second part ends at the end of the image,
// and is not written when it is empty.
func extractProgramStore(data []byte, off int, out *walk.Output) walk.Extraction {
	h, ok := parseProgramStoreHeader(data[off:])
	if !ok {
		return walk.Extraction{}
	}
	r := walk.Extraction{OK: true, Size: h.totalLen, HasSize: true}
	if out == nil {
		return r
	}
	start := off + psHeaderSize
	if !h.split {
		// A single payload is len1 when it is not 0, which equals len.
		r.OK = out.CarveFile(h.filename, data, start, h.len)
		return r
	}
	if !out.CarveFile(h.filename+".1", data, start, h.len1) {
		r.OK = false
		return r
	}
	if h.len2 > 0 && !out.CarveFile(h.filename+".2", data, off+h.totalLen-h.len2, h.len2) {
		r.OK = false
	}
	return r
}
