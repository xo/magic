//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/mh01.rs, src/structures/mh01.rs and
// src/extractors/mh01.rs of binwalk.

package vendor

import (
	"fmt"
	"strings"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// MH01 is the signature "mh01" of binwalk.
var MH01 = &walk.Signature{
	Name:        "mh01",
	Magic:       [][]byte{[]byte("MH01")},
	Description: "D-Link MH01 firmware image",
	Parse:       parseMH01,
	Extract:     extractMH01,
}

// extractMH01 is extract_mh01_image. binwalk first tries to decrypt the
// image with the keys of delink, and writes decrypted.bin when one of them
// works. The walker does not carry those keys yet (D51), so it always takes
// the other way of binwalk: it carves the IV, the signature and the
// encrypted data to iv.bin, signature.bin and encrypted.bin.
func extractMH01(data []byte, off int, out *walk.Output) walk.Extraction {
	b := data[off:]
	h, ok := mh01Header(b)
	if !ok {
		return walk.Extraction{}
	}
	r := walk.Extraction{OK: true, Size: h.totalSize, HasSize: true}
	if out != nil {
		r.OK = out.CarveFile("iv.bin", b, h.ivOffset, h.ivSize) &&
			out.CarveFile("signature.bin", b, h.signatureOffset, h.signatureSize) &&
			out.CarveFile("encrypted.bin", b, h.encryptedOffset, h.encryptedSize)
	}
	return r
}

// parseMH01 is mh01_parser. The encrypted data must be in the format of
// OpenSSL.
func parseMH01(data []byte, off int) (walk.Match, error) {
	h, ok := mh01Header(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	crypt, ok := binwalk.From(data, off+h.encryptedOffset)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	ssl, err := parseOpenSSL(crypt, 0)
	if err != nil {
		return walk.Match{}, err
	}
	return walk.Match{
		Offset:     off,
		Size:       h.totalSize,
		Confidence: walk.High,
		Description: fmt.Sprintf("D-Link MH01 firmware image, signed, encrypted with %s, IV: %s, total size: %d bytes",
			ssl.Description, h.iv, h.totalSize),
	}, nil
}

var mh01Struct = binwalk.Struct{
	{Name: "magic1", Type: "u32"},
	{Name: "signature_offset", Type: "u32"},
	{Name: "signature_size", Type: "u32"},
	{Name: "unknown1", Type: "u32"},
	{Name: "magic2", Type: "u32"},
	{Name: "iv_size", Type: "u32"},
	{Name: "encrypted_data_size", Type: "u32"},
	{Name: "unknown2", Type: "u32"},
}

type mh01Info struct {
	iv              string
	ivOffset        int
	ivSize          int
	signatureOffset int
	signatureSize   int
	encryptedOffset int
	encryptedSize   int
	totalSize       int
}

// mh01Header is parse_mh01_header. The structure is two headers of 16 bytes,
// and the IV, as a string of hex digits, follows them.
func mh01Header(data []byte) (mh01Info, bool) {
	const headerSize = 16
	h, ok := mh01Struct.Parse(data, false)
	if !ok || h["magic1"] != h["magic2"] {
		return mh01Info{}, false
	}
	ivOff := mh01Struct.Size()
	encOff := ivOff + h["iv_size"]
	b, ok := binwalk.Range(data, ivOff, encOff)
	if !ok {
		return mh01Info{}, false
	}
	iv := binwalk.CString(b)
	if len(iv) != h["iv_size"] {
		return mh01Info{}, false
	}
	return mh01Info{
		iv:              strings.TrimSpace(iv),
		ivOffset:        ivOff,
		ivSize:          h["iv_size"],
		signatureOffset: headerSize + h["signature_offset"],
		signatureSize:   h["signature_size"],
		encryptedOffset: encOff,
		encryptedSize:   h["encrypted_data_size"],
		totalSize:       headerSize + h["signature_offset"] + h["signature_size"],
	}, true
}
