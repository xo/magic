//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/seama.rs of binwalk-ng.

package firmware

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// SEAMA is the signature "seama" of binwalk-ng.
var SEAMA = &walk.Signature{
	Name: "seama",
	// The magic in big-endian and in little-endian order.
	Magic: [][]byte{
		[]byte("\x5E\xA3\xA4\x17\x00\x00"),
		[]byte("\x17\xA4\xA3\x5E\x00\x00"),
	},
	Description: "SEAMA firmware header",
	Parse:       parseSEAMA,
}

// parseSEAMA is seama_parser.
func parseSEAMA(data []byte, off int) (walk.Match, error) {
	h, ok := seamaHeader(data[off:])
	if !ok || len(data) < off+h.headerSize+h.dataSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        h.headerSize,
		Confidence:  walk.Low,
		Description: fmt.Sprintf("SEAMA firmware header, header size: %d bytes, data size: %d bytes", h.headerSize, h.dataSize),
	}, nil
}

var seamaStruct = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "description_size", Type: "u32"},
	{Name: "data_size", Type: "u32"},
	{Name: "unknown1", Type: "u64"},
	{Name: "unknown2", Type: "u64"},
}

type seamaInfo struct {
	headerSize int
	dataSize   int
}

// seamaHeader is parse_seama_header. It tries little-endian order first,
// then big-endian order.
func seamaHeader(data []byte) (seamaInfo, bool) {
	const magic = 0x5EA3A417
	h, ok := seamaStruct.Parse(data, false)
	if !ok {
		return seamaInfo{}, false
	}
	if h["magic"] != magic {
		if h, ok = seamaStruct.Parse(data, true); !ok {
			return seamaInfo{}, false
		}
	}
	if h["magic"] != magic {
		return seamaInfo{}, false
	}
	total := seamaStruct.Size() + h["description_size"]
	if len(data) < total {
		return seamaInfo{}, false
	}
	return seamaInfo{headerSize: total, dataSize: h["data_size"]}, true
}
