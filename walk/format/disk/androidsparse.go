//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/androidsparse.rs,
// src/structures/androidsparse.rs and src/extractors/androidsparse.rs of
// binwalk.

package disk

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// AndroidSparse is the signature "android_sparse" of binwalk.
var AndroidSparse = &walk.Signature{
	Name:        "android_sparse",
	Magic:       [][]byte{[]byte("\x3A\xFF\x26\xED")},
	Description: "Android sparse image",
	Parse:       parseAndroidSparse,
	Extract:     extractAndroidSparse,
}

// parseAndroidSparse is android_sparse_parser.
func parseAndroidSparse(data []byte, off int) (walk.Match, error) {
	r := extractAndroidSparse(data, off, nil)
	if !r.OK || !r.HasSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	h, ok := parseSparseHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := r.Size
	return walk.Match{
		Offset:     off,
		Size:       size,
		Confidence: walk.High,
		Description: fmt.Sprintf("Android sparse image, version %d.%d, header size: %d, block size: %d, chunk count: %d, total size: %d bytes",
			h["major_version"], h["minor_version"], h["header_size"], h["block_size"], h["total_chunks"], size),
	}, nil
}

var sparseHeader = binwalk.Struct{
	{Name: "magic", Type: "u32"},
	{Name: "major_version", Type: "u16"},
	{Name: "minor_version", Type: "u16"},
	{Name: "header_size", Type: "u16"},
	{Name: "chunk_header_size", Type: "u16"},
	{Name: "block_size", Type: "u32"},
	{Name: "block_count", Type: "u32"},
	{Name: "total_chunks", Type: "u32"},
	{Name: "checksum", Type: "u32"},
}

var sparseChunk = binwalk.Struct{
	{Name: "chunk_type", Type: "u16"},
	{Name: "reserved", Type: "u16"},
	{Name: "output_block_count", Type: "u32"},
	{Name: "total_size", Type: "u32"},
}

// parseSparseHeader is parse_android_sparse_header.
func parseSparseHeader(data []byte) (binwalk.Fields, bool) {
	h, ok := sparseHeader.Parse(data, false)
	if !ok || h["major_version"] != 1 || h["minor_version"] != 0 ||
		h["header_size"] != sparseHeader.Size() || h["chunk_header_size"] != sparseChunk.Size() ||
		h["block_size"]%4 != 0 {
		return nil, false
	}
	return h, true
}

// sparseChunkHeader is AndroidSparseChunkHeader.
type sparseChunkHeader struct {
	dataSize   int
	blockCount int
	raw        bool
	fill       bool
	dontCare   bool
}

// parseSparseChunk is parse_android_sparse_chunk_header.
func parseSparseChunk(data []byte) (sparseChunkHeader, bool) {
	const (
		typeRaw      = 0xCAC1
		typeFill     = 0xCAC2
		typeDontCare = 0xCAC3
		typeCRC      = 0xCAC4
	)
	c, ok := sparseChunk.Parse(data, false)
	if !ok || c["reserved"] != 0 || c["total_size"] < sparseChunk.Size() {
		return sparseChunkHeader{}, false
	}
	size := c["total_size"] - sparseChunk.Size()
	switch c["chunk_type"] {
	case typeRaw:
	case typeFill, typeCRC:
		ok = size == 4
	case typeDontCare:
		ok = size == 0
	default:
		ok = false
	}
	return sparseChunkHeader{
		dataSize:   size,
		blockCount: c["output_block_count"],
		raw:        c["chunk_type"] == typeRaw,
		fill:       c["chunk_type"] == typeFill,
		dontCare:   c["chunk_type"] == typeDontCare,
	}, ok
}

// extractAndroidSparse is extract_android_sparse: it writes the blocks of
// the chunks to unsparsed.img. The size of a dry run can reach past the end
// of the data, because a dry run reads only the headers of the chunks.
func extractAndroidSparse(data []byte, off int, out *walk.Output) walk.Extraction {
	const (
		outName = "unsparsed.img"
		// binwalk makes no image larger than 16 GiB.
		maxUnsparsed = 16 << 30
	)
	h, ok := parseSparseHeader(data[off:])
	if !ok {
		return walk.Extraction{}
	}
	// The product of two u32 values fits in a uint64, as in a usize.
	if u64(h, "block_count")*u64(h, "block_size") > maxUnsparsed {
		return walk.Extraction{}
	}
	count, written := 0, 0
	next, previous := off+h["header_size"], -1
	for binwalk.OffsetSafe(len(data), next, previous) {
		c, ok := parseSparseChunk(data[next:])
		if !ok {
			break
		}
		// A chunk cannot hold more blocks than the header.
		written += c.blockCount
		if written > h["block_count"] {
			break
		}
		if c.raw && uint64(c.blockCount)*u64(h, "block_size") != uint64(c.dataSize) {
			break
		}
		if out != nil {
			start := next + sparseChunk.Size()
			b, ok := binwalk.Range(data, start, start+c.dataSize)
			if !ok || !extractSparseChunk(h["block_size"], c, b, outName, out) {
				break
			}
		}
		count++
		previous = next
		next += sparseChunk.Size() + c.dataSize
	}
	// Every chunk that the header counts must be valid.
	if count != h["total_chunks"] {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true, Size: next - off, HasSize: true}
}

// extractSparseChunk is extract_chunk: it adds the blocks of one chunk to
// the file name. A CRC chunk adds nothing.
//
// binwalk adds one block at a time. When a block has no bytes, it adds an
// empty block once for each block of the chunk, which can be billions of
// times. The walker adds it once, which gives the same file.
func extractSparseChunk(blockSize int, c sparseChunkHeader, b []byte, name string, out *walk.Output) bool {
	var block []byte
	switch {
	case c.raw:
		// A raw chunk holds its blocks as they are.
		return out.AppendFile(name, b)
	case c.fill:
		// A fill chunk repeats its 4 bytes over each block.
		if len(b) == 0 {
			return false
		}
		for i := 0; i < blockSize; i += len(b) {
			block = append(block, b...)
		}
	case c.dontCare:
		// The blocks of a chunk that does not care are zeros.
		block = make([]byte, blockSize)
	default:
		return true
	}
	n := c.blockCount
	if len(block) == 0 {
		n = min(n, 1)
	}
	for range n {
		if !out.AppendFile(name, block) {
			return false
		}
	}
	return true
}
