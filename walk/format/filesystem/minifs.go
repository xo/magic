//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/minifs.rs of binwalk-ng.

package filesystem

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/xo/magic/compress"
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// MINIFS is the signature "minifs" of binwalk-ng, the file system of some
// TP-Link firmware.
//
// The chunks of data are xz or LZMA, and the walker decodes them with the
// stream decoders in the registry of github.com/xo/magic/compress. A program
// that does not import github.com/xo/magic/compress/xz has no such decoder.
// The walker then finds a MINIFS image, and does not extract it.
var MINIFS = &walk.Signature{
	Name:        "minifs",
	Magic:       [][]byte{[]byte("MINIFS")},
	Description: "MINIFS image",
	Parse:       parseMINIFS,
	Extract:     extractMINIFS,
}

// The sizes of the tables of MINIFS, in bytes.
const (
	minifsHeaderSize = 32 // MinifsHeaderBytes
	minifsFileSize   = 20 // FileEntryBytes
	minifsChunkSize  = 12 // ChunkEntryBytes
)

// minifsMaxExtracted is MAX_EXTRACTED_SIZE: the most bytes that the chunks
// of one image decode to.
const minifsMaxExtracted = 1 << 30

// minifsFile is MinifsFileEntry. path and name are the name table from the
// start of each, up to its end.
type minifsFile struct {
	path, name          []byte
	chunk, offset, size int
}

// minifsChunk is MinifsChunkEntry. offset is from the end of the chunk table.
type minifsChunk struct {
	offset, size, decompressed int
}

// minifsImage is MinifsImage.
type minifsImage struct {
	version, numFiles, nameTableSize uint32
	files                            []minifsFile
	chunks                           []minifsChunk
	chunksOffset                     int // the end of the chunk table
	size                             int
}

// parseMINIFSImage is parse_minifs_file. It reads the header, the name
// table, the file table and the chunk table, and checks that each chunk is
// in the data and that each file is in the data of its chunk.
func parseMINIFSImage(data []byte) (*minifsImage, bool) {
	be := binary.BigEndian
	if len(data) < minifsHeaderSize {
		return nil, false
	}
	img := &minifsImage{
		version:       be.Uint32(data[16:]),
		numFiles:      be.Uint32(data[20:]),
		nameTableSize: be.Uint32(data[28:]),
	}
	if img.numFiles == 0 {
		return nil, false
	}
	rest := data[minifsHeaderSize:]
	if uint64(img.nameTableSize) > uint64(len(rest)) {
		return nil, false
	}
	names, rest := rest[:img.nameTableSize], rest[img.nameTableSize:]
	if uint64(img.numFiles)*minifsFileSize > uint64(len(rest)) {
		return nil, false
	}
	table, rest := rest[:int(img.numFiles)*minifsFileSize], rest[int(img.numFiles)*minifsFileSize:]

	img.files = make([]minifsFile, 0, img.numFiles)
	maxChunk := 0
	for e := table; len(e) > 0; e = e[minifsFileSize:] {
		pathIndex, nameIndex := uint64(be.Uint32(e)), uint64(be.Uint32(e[4:]))
		if pathIndex > uint64(len(names)) || nameIndex > uint64(len(names)) {
			return nil, false
		}
		f := minifsFile{
			path:   names[pathIndex:],
			name:   names[nameIndex:],
			chunk:  int(be.Uint32(e[8:])),
			offset: int(be.Uint32(e[12:])),
			size:   int(be.Uint32(e[16:])),
		}
		// A file name that is empty, at the end of the table or at a NUL,
		// is not valid.
		if len(f.name) == 0 || f.name[0] == 0 {
			return nil, false
		}
		maxChunk = max(maxChunk, f.chunk)
		img.files = append(img.files, f)
	}

	count := uint64(maxChunk) + 1
	if count*minifsChunkSize > uint64(len(rest)) {
		return nil, false
	}
	toc, rest := rest[:int(count)*minifsChunkSize], rest[int(count)*minifsChunkSize:]
	img.chunksOffset = len(data) - len(rest)

	img.chunks = make([]minifsChunk, 0, count)
	last := 0
	for e := toc; len(e) > 0; e = e[minifsChunkSize:] {
		c := minifsChunk{
			offset:       int(be.Uint32(e)),
			size:         int(be.Uint32(e[4:])),
			decompressed: int(be.Uint32(e[8:])),
		}
		end := c.offset + c.size
		if c.size == 0 || end > len(rest) {
			return nil, false
		}
		last = max(last, end)
		img.chunks = append(img.chunks, c)
	}
	for _, f := range img.files {
		if f.offset+f.size > img.chunks[f.chunk].decompressed {
			return nil, false
		}
	}
	img.size = img.chunksOffset + last
	return img, true
}

// parseMINIFS is minifs_parser. It reads the tables, and does not decode
// the chunks.
func parseMINIFS(data []byte, off int) (walk.Match, error) {
	img, ok := parseMINIFSImage(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       img.size,
		Confidence: walk.High,
		Description: fmt.Sprintf("MINIFS image, version: %d, files: %d, name table size: %d bytes",
			img.version, img.numFiles, img.nameTableSize),
	}, nil
}

// extractMINIFS is extract_minifs. It decodes the chunks in order, and
// writes each file in the folder of its path. The files of a chunk keep the
// order of the file table.
func extractMINIFS(data []byte, off int, out *walk.Output) walk.Extraction {
	data = data[off:]
	img, ok := parseMINIFSImage(data)
	if !ok {
		return walk.Extraction{}
	}
	// binwalk-ng sorts the files by chunk with a stable sort.
	order := make([][]minifsFile, len(img.chunks))
	for _, f := range img.files {
		order[f.chunk] = append(order[f.chunk], f)
	}
	total := 0
	for i, files := range order {
		if len(files) == 0 {
			continue
		}
		total += img.chunks[i].decompressed
		if total > minifsMaxExtracted {
			return walk.Extraction{}
		}
	}
	for i, files := range order {
		if len(files) == 0 {
			continue
		}
		decoded, ok := decodeMINIFSChunk(data, img, img.chunks[i])
		if !ok {
			return walk.Extraction{}
		}
		for _, f := range files {
			if out == nil {
				continue
			}
			dir := binwalk.CString(f.path)
			if !out.CreateDirectory(dir) {
				return walk.Extraction{}
			}
			// safe_path_join drops a leading slash of the file name, and
			// Output cleans the path in the same way.
			name := dir + "/" + binwalk.CString(f.name)
			if !out.CreateFile(name, decoded[f.offset:f.offset+f.size]) {
				return walk.Extraction{}
			}
		}
	}
	return walk.Extraction{OK: true, Size: img.size, HasSize: true}
}

// errMINIFSLimit stops a decoder that gives more data than the chunk holds.
var errMINIFSLimit = errors.New("minifs: chunk too long")

// decodeMINIFSChunk is decode_chunk. binwalk-ng decodes a chunk with the
// auto decoder of liblzma, and takes one byte more than the chunk holds. The
// chunk is valid when the stream ends without an error and gives exactly the
// size of the chunk.
//
// The auto decoder reads xz when the first byte is 0xFD, lzip when the data
// starts with "LZIP", and LZMA otherwise. The walker has no lzip decoder, so
// it rejects a chunk of lzip.
func decodeMINIFSChunk(data []byte, img *minifsImage, c minifsChunk) ([]byte, bool) {
	start := img.chunksOffset + c.offset
	src := data[start : start+c.size]
	if bytes.HasPrefix(src, []byte("LZIP")) {
		return nil, false
	}
	method := compress.LZMA
	if src[0] == 0xfd {
		method = compress.XZ
	}
	decode, ok := compress.LookupStream(method)
	if !ok {
		return nil, false
	}
	w := &minifsWriter{limit: c.decompressed}
	if _, err := decode(src, w); err != nil || len(w.buf) != c.decompressed {
		return nil, false
	}
	return w.buf, true
}

// minifsWriter keeps up to limit bytes, and fails past them.
type minifsWriter struct {
	buf   []byte
	limit int
}

// Write implements io.Writer.
func (w *minifsWriter) Write(p []byte) (int, error) {
	if len(w.buf)+len(p) > w.limit {
		return 0, errMINIFSLimit
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}
