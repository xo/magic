//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/matter_ota.rs of binwalk-ng.

package vendor

import (
	"encoding/hex"
	"fmt"
	"unicode/utf8"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// MatterOTA is the signature "matter_ota" of binwalk.
var MatterOTA = &walk.Signature{
	Name:        "matter_ota",
	Short:       true,
	Magic:       [][]byte{[]byte("\x1e\xf1\xee\x1b")},
	Description: "Matter OTA firmware",
	Parse:       parseMatterOTA,
	Extract:     extractMatterOTA,
}

// extractMatterOTA is extract_matter_ota: it carves the payload after the
// header to matter_payload.bin. The payload must end in the data.
func extractMatterOTA(data []byte, off int, out *walk.Output) walk.Extraction {
	h, ok := matterOTAHeader(data[off:])
	if !ok {
		return walk.Extraction{}
	}
	start := off + matterOTAStruct.Size() + h.headerSize
	r := walk.Extraction{Size: h.totalSize, HasSize: true}
	// A payload size above the largest int is negative here, and Range
	// rejects it, as binwalk-ng rejects a sum that overflows.
	b, ok := binwalk.Range(data, start, start+h.payloadSize)
	if !ok {
		return r
	}
	r.OK = out == nil || out.CarveFile("matter_payload.bin", b, 0, len(b))
	return r
}

// parseMatterOTA is matter_ota_parser.
func parseMatterOTA(data []byte, off int) (walk.Match, error) {
	h, ok := matterOTAHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	// binwalk keeps each value in a usize. A u64 above the largest int is
	// negative here, so the text shows it as unsigned, as binwalk does.
	return walk.Match{
		Offset:     off,
		Size:       h.headerSize,
		Confidence: walk.High,
		Description: fmt.Sprintf("Matter OTA firmware, total size: %d bytes, tlv header size: %d bytes, vendor id: 0x%x, product id: 0x%x, version: %s, payload size: %d bytes, digest type: %d, payload digest: %s",
			uint64(h.totalSize), h.headerSize, uint64(h.vendorID), uint64(h.productID),
			h.version, uint64(h.payloadSize), uint64(h.digestType), h.digest),
	}, nil
}

var matterOTAStruct = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "total_size", Type: "u64"},
	{Name: "header_size", Type: "u32"},
}

type matterOTAInfo struct {
	totalSize   int
	headerSize  int
	vendorID    int
	productID   int
	version     string
	payloadSize int
	digestType  int
	digest      string
}

// matterOTAHeader is parse_matter_ota_header. It reads the elements in
// order, and a later element replaces an earlier one of the same tag. An
// element of a known tag must have the type of that tag, and an element of
// an unknown tag is ignored.
func matterOTAHeader(data []byte) (matterOTAInfo, bool) {
	const (
		tagVendorID        = 0
		tagProductID       = 1
		tagVersionString   = 3
		tagPayloadSize     = 4
		tagImageDigestType = 8
		tagImageDigest     = 9
	)
	h, ok := matterOTAStruct.Parse(data, false)
	if !ok {
		return matterOTAInfo{}, false
	}
	r := matterOTAInfo{totalSize: h["total_size"], headerSize: h["header_size"]}
	start := matterOTAStruct.Size()
	b, ok := binwalk.Range(data, start, start+r.headerSize)
	if !ok {
		return matterOTAInfo{}, false
	}
	for len(b) > 0 {
		tag, v, n, ok := tlvElement(b)
		if !ok {
			return matterOTAInfo{}, false
		}
		b = b[n:]
		want := tlvUnsigned
		switch tag {
		case tagVersionString:
			want = tlvString
		case tagImageDigest:
			want = tlvOctets
		case tagVendorID, tagProductID, tagPayloadSize, tagImageDigestType:
		default:
			continue
		}
		if v.kind != want {
			return matterOTAInfo{}, false
		}
		switch tag {
		case tagVendorID:
			r.vendorID = v.n
		case tagProductID:
			r.productID = v.n
		case tagVersionString:
			r.version = v.s
		case tagPayloadSize:
			r.payloadSize = v.n
		case tagImageDigestType:
			r.digestType = v.n
		case tagImageDigest:
			r.digest = hex.EncodeToString(v.b)
		}
	}
	// binwalk-ng keeps the sizes in a usize, and rejects a sum that
	// overflows. The sum here is in a uint64, which holds the same bits.
	sum := uint64(r.payloadSize) + uint64(start) + uint64(r.headerSize)
	if sum < uint64(r.payloadSize) || sum != uint64(r.totalSize) {
		return matterOTAInfo{}, false
	}
	return r, true
}

// The kinds of the values of the TLV elements, as Value of matter_ota.rs.
const (
	tlvStruct = iota
	tlvEnd
	tlvUnsigned
	tlvString
	tlvOctets
)

// tlvValue is Value of matter_ota.rs.
type tlvValue struct {
	kind int
	n    int
	s    string
	b    []byte
}

// tlvWidth is the width of an integer, or of the length of a string, of a
// TLV element, by the low 2 bits of its type.
var tlvWidth = [4]binwalk.Struct{
	{{Name: "field", Type: "u8"}},
	{{Name: "field", Type: "u16"}},
	{{Name: "field", Type: "u32"}},
	{{Name: "field", Type: "u64"}},
}

// tlvElement is parse_tlv_element. It returns the tag, or -1 when the element
// has none, the value, and the size of the element.
func tlvElement(data []byte) (int, tlvValue, int, bool) {
	if len(data) == 0 {
		return 0, tlvValue{}, 0, false
	}
	typ := data[0] & 0x1f
	width := tlvWidth[typ&3]
	var tag, off int
	switch data[0] >> 5 {
	case 0:
		tag, off = -1, 1
	case 1:
		if len(data) < 2 {
			return 0, tlvValue{}, 0, false
		}
		tag, off = int(data[1]), 2
	default:
		return 0, tlvValue{}, 0, false
	}
	field := data[off:]
	switch {
	case typ == 0b1_0101:
		return tag, tlvValue{kind: tlvStruct}, off, true
	case typ == 0b1_1000:
		return tag, tlvValue{kind: tlvEnd}, off, true
	case typ >= 0b0_0100 && typ <= 0b0_0111:
		f, ok := width.Parse(field, false)
		if !ok {
			return 0, tlvValue{}, 0, false
		}
		return tag, tlvValue{kind: tlvUnsigned, n: f["field"]}, off + width.Size(), true
	case typ >= 0b0_1100 && typ <= 0b0_1111:
		f, ok := width.Parse(field, false)
		if !ok {
			return 0, tlvValue{}, 0, false
		}
		n := f["field"]
		b, ok := binwalk.Range(field, width.Size(), width.Size()+n)
		if !ok {
			return 0, tlvValue{}, 0, false
		}
		// The string must be valid UTF-8, and it holds every byte, NUL too.
		if !utf8.Valid(b) {
			return 0, tlvValue{}, 0, false
		}
		return tag, tlvValue{kind: tlvString, s: string(b)}, off + width.Size() + n, true
	case typ >= 0b1_0000 && typ <= 0b1_0011:
		f, ok := width.Parse(field, false)
		if !ok {
			return 0, tlvValue{}, 0, false
		}
		n := f["field"]
		b, ok := binwalk.Range(field, width.Size(), width.Size()+n)
		if !ok {
			return 0, tlvValue{}, 0, false
		}
		return tag, tlvValue{kind: tlvOctets, b: b}, off + width.Size() + n, true
	}
	return 0, tlvValue{}, 0, false
}
