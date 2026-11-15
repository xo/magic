//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/dlink_tlv.rs and
// src/structures/dlink_tlv.rs of binwalk.

package vendor

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// DLinkTLV is the signature "dlink_tlv" of binwalk.
var DLinkTLV = &walk.Signature{
	Name:        "dlink_tlv",
	Magic:       [][]byte{[]byte("\x64\x80\x19\x40")},
	Description: "D-Link TLV firmware",
	Parse:       parseDLinkTLV,
}

// parseDLinkTLV is dlink_tlv_parser. The MD5 checksum covers the 8 bytes
// before the data, and the data.
func parseDLinkTLV(data []byte, off int) (walk.Match, error) {
	const checksumOffset = 8
	h, ok := dlinkTLVHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off + h.headerSize - checksumOffset
	payload, ok := binwalk.Range(data, start, start+h.dataSize+checksumOffset)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	sum := md5.Sum(payload)
	if h.checksum != "" && hex.EncodeToString(sum[:]) != h.checksum {
		return walk.Match{}, walk.ErrNoMatch
	}
	desc := fmt.Sprintf("D-Link TLV firmware, model name: %s, board ID: %s, header size: %d bytes, data size: %d bytes",
		h.model, h.board, h.headerSize, h.dataSize)
	if crypt, ok := binwalk.From(data, off+h.headerSize); ok {
		if ssl, err := parseOpenSSL(crypt, 0); err == nil {
			desc += ", " + ssl.Description
		}
	}
	return walk.Match{
		Offset:      off,
		Size:        h.headerSize + h.dataSize,
		Confidence:  walk.High,
		Description: desc,
	}, nil
}

var dlinkTLVStruct = binwalk.Struct{
	{Name: "type", Type: "u32"},
	{Name: "length", Type: "u32"},
}

type dlinkTLVInfo struct {
	model, board, checksum string
	headerSize, dataSize   int
}

// dlinkTLVHeader is parse_dlink_tlv_header.
func dlinkTLVHeader(data []byte) (dlinkTLVInfo, bool) {
	const (
		maxString   = 0x20
		modelOffset = 4
		boardOffset = 0x24
		md5Offset   = 0x4C
		tlvOffset   = 0x6C
		headerSize  = 0x74
		dataType    = 1
	)
	if len(data) < headerSize {
		return dlinkTLVInfo{}, false
	}
	h := data[:headerSize]
	r := dlinkTLVInfo{
		board:    binwalk.CString(h[boardOffset : boardOffset+maxString]),
		model:    binwalk.CString(h[modelOffset : modelOffset+maxString]),
		checksum: binwalk.CString(h[md5Offset : md5Offset+maxString]),
	}
	if r.model == "" || r.board == "" {
		return dlinkTLVInfo{}, false
	}
	t, ok := dlinkTLVStruct.Parse(h[tlvOffset:], false)
	if !ok || t["type"] != dataType {
		return dlinkTLVInfo{}, false
	}
	r.dataSize = t["length"]
	r.headerSize = headerSize
	return r, true
}
