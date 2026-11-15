//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/dxbc.rs of binwalk-ng.

package exec

import (
	"bytes"
	"encoding/binary"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// DXBC is the signature "dxbc" of binwalk-ng.
var DXBC = &walk.Signature{
	Name:        "dxbc",
	Magic:       [][]byte{[]byte("DXBC")},
	Description: "DirectX shader bytecode",
	Parse:       parseDXBC,
	Extract:     extractDXBC,
	NoRecurse:   true,
}

// extractDXBC is extract_dxbc_file: it carves the shader to shader.dxbc.
func extractDXBC(data []byte, off int, out *walk.Output) walk.Extraction {
	size, _, ok := parseDXBCHeader(data[off:])
	if !ok {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: out == nil || out.CarveFile("shader.dxbc", data, off, size), Size: size, HasSize: true}
}

// parseDXBC is dxbc_parser.
func parseDXBC(data []byte, off int) (walk.Match, error) {
	size, chunks, ok := parseDXBCHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	model := "Unknown Shader Model"
	switch {
	case containsChunk(chunks, "SHDR"):
		model = "Shader Model 4"
	case containsChunk(chunks, "SHEX"):
		model = "Shader Model 5"
	}
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.High,
		Description: "DirectX shader bytecode, " + model,
	}, nil
}

// containsChunk reports whether id is in chunks.
func containsChunk(chunks [][]byte, id string) bool {
	for _, c := range chunks {
		if bytes.Equal(c, []byte(id)) {
			return true
		}
	}
	return false
}

var dxbcHeader = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "signature_p1", Type: "u64"},
	{Name: "signature_p2", Type: "u64"},
	{Name: "one", Type: "u32"},
	{Name: "total_size", Type: "u32"},
	{Name: "chunk_count", Type: "u32"},
}

// parseDXBCHeader is parse_dxbc_header. It returns the total size and the
// identifier of each chunk.
func parseDXBCHeader(data []byte) (int, [][]byte, bool) {
	h, ok := dxbcHeader.Parse(data, false)
	// binwalk-ng allows 1 to 32 chunks, so that garbage does not make a
	// long loop.
	if !ok || h["one"] != 1 || h["chunk_count"] == 0 || h["chunk_count"] > 32 {
		return 0, nil, false
	}
	total := h["total_size"]
	if total == 0 || total > len(data) {
		return 0, nil, false
	}
	// The table and each chunk are in the container, so that a small
	// total size does not take bytes from the data after it.
	end := dxbcHeader.Size()
	if end+h["chunk_count"]*4 > total {
		return 0, nil, false
	}
	var chunks [][]byte
	for i := range h["chunk_count"] {
		p := data[end+i*4 : end+i*4+4]
		off := int(binary.LittleEndian.Uint32(p))
		if off+4 > total {
			return 0, nil, false
		}
		chunks = append(chunks, data[off:off+4])
	}
	return total, chunks, true
}
