//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/dxbc.rs, src/structures/dxbc.rs and
// src/extractors/dxbc.rs of binwalk.

package exec

import (
	"bytes"
	"encoding/binary"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// DXBC is the signature "dxbc" of binwalk.
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
	// binwalk allows no more than 32 chunks, so that garbage does not
	// make a long loop.
	if !ok || h["one"] != 1 || h["chunk_count"] > 32 {
		return 0, nil, false
	}
	end := dxbcHeader.Size()
	var chunks [][]byte
	for i := range h["chunk_count"] {
		p, ok := binwalk.Range(data, end+i*4, end+i*4+4)
		if !ok {
			return 0, nil, false
		}
		off := int(binary.LittleEndian.Uint32(p))
		id, ok := binwalk.Range(data, off, off+4)
		if !ok {
			return 0, nil, false
		}
		chunks = append(chunks, id)
	}
	return h["total_size"], chunks, true
}
