//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/arcadyan.rs and lzma_decompress of
// src/formats/lzma.rs of binwalk-ng.

package vendor

import (
	"io"

	"github.com/xo/magic/compress"
	"github.com/xo/magic/walk"
)

// Arcadyan is the signature "arcadyan" of binwalk.
//
// The data is LZMA, and the walker decodes it with the stream decoder of
// LZMA in the registry of github.com/xo/magic/compress. A program that does
// not import github.com/xo/magic/compress/xz has no such decoder, and the
// walker then rejects each candidate.
var Arcadyan = &walk.Signature{
	Name:        "arcadyan",
	Magic:       [][]byte{[]byte("\x00\xD5\x08\x00")},
	Description: "Arcadyan obfuscated LZMA",
	Parse:       parseArcadyan,
	Extract:     extractArcadyan,
}

// parseArcadyan is obfuscated_lzma_parser. The magic is 0x68 bytes into the
// data. The match has no size, as in binwalk.
func parseArcadyan(data []byte, off int) (walk.Match, error) {
	const magicOffset = 0x68
	if off < magicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - magicOffset
	if !extractArcadyan(data, start, nil).OK {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      start,
		Confidence:  walk.High,
		Description: "Arcadyan obfuscated LZMA",
	}, nil
}

// extractArcadyan is extract_obfuscated_lzma. It takes the data from off to
// the end, when it has more than 0x100 bytes and at most 0x1B0000, puts it in
// order, and decodes the LZMA data that starts 4 bytes into it.
func extractArcadyan(data []byte, off int, out *walk.Output) walk.Extraction {
	const (
		lzmaOffset = 4
		minSize    = 0x100
		maxSize    = 0x1B0000
	)
	available := len(data) - off
	if available > maxSize || available <= minSize {
		return walk.Extraction{}
	}
	return lzmaDecompress(arcadyanDeobfuscate(data[off:]), lzmaOffset, out)
}

// arcadyanDeobfuscate is arcadyan_deobfuscator. It swaps the block of 32
// bytes at 4 with the block of 32 bytes at 0x68. Then, in the block at 4,
// it swaps the two nibbles of each byte, and swaps each byte with the next.
// The data must have more than 0x88 bytes.
func arcadyanDeobfuscate(data []byte) []byte {
	const (
		blockSize = 32
		block1    = 4
		block2    = 0x68
	)
	out := make([]byte, 0, len(data))
	out = append(out, data[:block1]...)
	out = append(out, data[block2:block2+blockSize]...)
	out = append(out, data[block1+blockSize:block2]...)
	out = append(out, data[block1:block1+blockSize]...)
	out = append(out, data[block2+blockSize:]...)
	for i := block1; i < block1+blockSize; i++ {
		out[i] = out[i]<<4 | out[i]>>4
	}
	for i := block1; i < block1+blockSize; i += 2 {
		out[i], out[i+1] = out[i+1], out[i]
	}
	return out
}

// lzmaDecompress is lzma_decompress of lzma.rs: it decodes the LZMA stream
// at off in data to decompressed.bin, and reports the bytes of the stream.
// binwalk-ng copies the decoder to the file until the stream ends, so a
// stream that gives no byte is a success.
//
// binwalk asks liblzma for an auto decoder, which also reads xz. The data
// of arcadyan always starts with the properties of LZMA, so this reads only
// LZMA.
func lzmaDecompress(data []byte, off int, out *walk.Output) walk.Extraction {
	decode, ok := compress.LookupStream(compress.LZMA)
	if !ok {
		return walk.Extraction{}
	}
	// binwalk-ng makes the file before it decodes, so a stream that gives
	// no byte leaves an empty file.
	if out != nil && !out.CreateFile("decompressed.bin", nil) {
		return walk.Extraction{}
	}
	w := &lzmaWriter{out: out}
	n, err := decode(data[off:], w)
	if err != nil || w.failed {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true, Size: n, HasSize: true}
}

// lzmaWriter adds the bytes that the decoder gives to decompressed.bin when
// it has an output.
type lzmaWriter struct {
	out    *walk.Output
	failed bool
}

// Write implements io.Writer.
func (w *lzmaWriter) Write(p []byte) (int, error) {
	if w.out != nil && !w.out.AppendFile("decompressed.bin", p) {
		w.failed = true
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}
