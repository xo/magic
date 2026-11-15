//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/dpapi.rs of binwalk-ng.

package crypto

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"slices"

	"github.com/xo/magic/walk"
)

// DPAPI is the signature "dpapi" of binwalk-ng.
var DPAPI = &walk.Signature{
	Name:        "dpapi",
	Short:       true,
	Magic:       [][]byte{[]byte("\x01\x00\x00\x00\xD0\x8c\x9d\xdf\x01\x15\xd1\x11\x8c\x7a\x00\xc0\x4f\xc2\x97\xeb")},
	Description: "DPAPI blob data",
	Parse:       parseDPAPI,
}

// parseDPAPI is dpapi_parser. The description of binwalk-ng is one string
// literal over four lines of its source, so it holds three new lines, each
// with the spaces that start the next line of the source.
func parseDPAPI(data []byte, off int) (walk.Match, error) {
	h, ok := dpapiHeader(data[off:])
	if !ok || h.blobSize > len(data)-off {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       h.blobSize,
		Confidence: walk.Medium,
		Description: fmt.Sprintf("DPAPI blob data, header_size: %d, blob_size: %d, version: %d, provider_id: %s, master_key_version: %d,\n"+
			"             master_key_id: %s, flags: %d, description_len: %d, crypto_algorithm: %d, crypto_alg_len: %d,\n"+
			"             salt_len: %d, hmac_key_len: %d, hash_algorithm: %d, hash_alg_len: %d, hmac2_key_len: %d,\n"+
			"             data_len: %d, sign_len: %d",
			h.headerSize, h.blobSize, h.version, h.providerID, h.masterKeyVersion,
			h.masterKeyID, h.flags, h.descriptionLen, h.cryptoAlgorithm, h.cryptoAlgLen,
			h.saltLen, h.hmacKeyLen, h.hashAlgorithm, h.hashAlgLen, h.hmac2KeyLen,
			h.dataLen, h.signLen),
	}, nil
}

// dpapiInfo is DPAPIBlobHeader. The two GUIDs are u128 values of binwalk-ng,
// in decimal.
type dpapiInfo struct {
	headerSize, blobSize                  int
	providerID, masterKeyID               string
	version, masterKeyVersion, flags      uint32
	cryptoAlgorithm, hashAlgorithm        uint32
	descriptionLen, cryptoAlgLen, saltLen int
	hmacKeyLen, hashAlgLen, hmac2KeyLen   int
	dataLen, signLen                      int
}

// The sizes of the parts of the header: DPAPIHeaderP1 to DPAPIHeaderP6.
const (
	dpapiP1Size = 48
	dpapiP2Size = 12
	dpapiP3Size = 4
	dpapiP4Size = 12
	dpapiP5Size = 4
	dpapiP6Size = 4
)

// dpapiHeader is parse_dpapi_blob_header. Each length is a u32, so no sum
// can overflow an int here, where binwalk-ng checks each sum.
func dpapiHeader(data []byte) (dpapiInfo, bool) {
	le := binary.LittleEndian
	if len(data) < dpapiP1Size {
		return dpapiInfo{}, false
	}
	h := dpapiInfo{
		headerSize:       dpapiP1Size + dpapiP2Size + dpapiP3Size + dpapiP4Size + dpapiP5Size + dpapiP6Size,
		version:          le.Uint32(data[0:]),
		providerID:       u128String(data[4:20]),
		masterKeyVersion: le.Uint32(data[20:]),
		masterKeyID:      u128String(data[24:40]),
		flags:            le.Uint32(data[40:]),
		descriptionLen:   int(le.Uint32(data[44:])),
	}
	if h.descriptionLen%2 != 0 {
		return dpapiInfo{}, false
	}
	off := dpapiP1Size
	if h.descriptionLen > len(data)-off {
		return dpapiInfo{}, false
	}
	if h.descriptionLen != 0 && !utf16NullTerminated(data[off:off+h.descriptionLen]) {
		return dpapiInfo{}, false
	}
	off += h.descriptionLen

	// part reads n bytes at off, or fails as ref_from_prefix does.
	part := func(n int) ([]byte, bool) {
		if off > len(data) || len(data)-off < n {
			return nil, false
		}
		return data[off : off+n], true
	}

	p2, ok := part(dpapiP2Size)
	if !ok {
		return dpapiInfo{}, false
	}
	h.cryptoAlgorithm = le.Uint32(p2[0:])
	h.cryptoAlgLen = int(le.Uint32(p2[4:]))
	h.saltLen = int(le.Uint32(p2[8:]))
	off += dpapiP2Size + h.saltLen

	p3, ok := part(dpapiP3Size)
	if !ok {
		return dpapiInfo{}, false
	}
	h.hmacKeyLen = int(le.Uint32(p3))
	off += dpapiP3Size + h.hmacKeyLen

	p4, ok := part(dpapiP4Size)
	if !ok {
		return dpapiInfo{}, false
	}
	h.hashAlgorithm = le.Uint32(p4[0:])
	h.hashAlgLen = int(le.Uint32(p4[4:]))
	h.hmac2KeyLen = int(le.Uint32(p4[8:]))
	off += dpapiP4Size + h.hmac2KeyLen

	p5, ok := part(dpapiP5Size)
	if !ok {
		return dpapiInfo{}, false
	}
	h.dataLen = int(le.Uint32(p5))
	off += dpapiP5Size + h.dataLen

	p6, ok := part(dpapiP6Size)
	if !ok {
		return dpapiInfo{}, false
	}
	h.signLen = int(le.Uint32(p6))
	h.blobSize = off + dpapiP6Size + h.signLen
	return h, true
}

// u128String gives the little endian u128 in b in decimal, as Rust prints it.
func u128String(b []byte) string {
	be := slices.Clone(b)
	slices.Reverse(be)
	return new(big.Int).SetBytes(be).String()
}

// utf16NullTerminated is is_null_terminated_utf16: b is valid little endian
// UTF-16 up to its first NUL, and holds only NUL characters after it. The
// length of b is even here.
func utf16NullTerminated(b []byte) bool {
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	i := 0
	for {
		if i >= len(units) {
			return false
		}
		u := units[i]
		switch {
		case u == 0:
			for _, v := range units[i+1:] {
				if v != 0 {
					return false
				}
			}
			return true
		case u >= 0xD800 && u < 0xDC00:
			// A high surrogate needs a low surrogate after it.
			if i+1 >= len(units) || units[i+1] < 0xDC00 || units[i+1] >= 0xE000 {
				return false
			}
			i += 2
		case u >= 0xDC00 && u < 0xE000:
			// A low surrogate alone is not valid.
			return false
		default:
			i++
		}
	}
}
