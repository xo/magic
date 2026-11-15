//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/csman.rs of binwalk-ng.

package vendor

import (
	"bytes"
	"fmt"
	"io"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
	"github.com/xo/magic/walk/internal/flate"
)

// CSman is the signature "csman" of binwalk.
var CSman = &walk.Signature{
	Name:        "csman",
	Short:       true,
	Magic:       [][]byte{[]byte("SC"), []byte("CS")},
	Description: "CSman DAT file",
	Parse:       parseCSman,
	Extract:     extractCSman,
}

// parseCSman is csman_parser. It is the dry run of the extractor.
func parseCSman(data []byte, off int) (walk.Match, error) {
	r := extractCSman(data, off, nil)
	if !r.OK || !r.HasSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        r.Size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("CSman DAT file, total size: %d bytes", r.Size),
	}, nil
}

// csmanMaxSize is the most bytes that the compressed entries can decompress
// to, as in binwalk. It stops a compression bomb.
const csmanMaxSize = 100 * 1024 * 1024

// extractCSman is extract_csman_dat. It writes each entry to a file named
// with its key in hex, such as 0000ABCD.dat. A later entry with the same key
// gets a number after the name, such as 0000ABCD.dat_1.
func extractCSman(data []byte, off int, out *walk.Output) walk.Extraction {
	const compressedHeaderSize = 2
	h, ok := csmanHeader(data[off:])
	if !ok {
		return walk.Extraction{}
	}
	start := off + h.headerSize
	raw, ok := binwalk.Range(data, start, start+h.dataSize)
	if !ok {
		return walk.Extraction{}
	}
	entries := raw
	if h.compressed {
		b, ok := binwalk.From(raw, compressedHeaderSize)
		if !ok {
			return walk.Extraction{}
		}
		if entries, ok = inflateLimit(b, csmanMaxSize); !ok {
			return walk.Extraction{}
		}
	}
	var list []csmanEntry
	found := false
	next, previous := 0, -1
	for binwalk.OffsetSafe(len(entries), next, previous) {
		e, ok := csmanParseEntry(entries[next:], h.big)
		if !ok {
			break
		}
		if e.eof {
			// The last entry is the mark of the end, after one entry or
			// more.
			found = len(list) > 0
			break
		}
		list = append(list, e)
		previous = next
		next += e.size
	}
	if !found {
		return walk.Extraction{}
	}
	r := walk.Extraction{OK: true, Size: h.headerSize + h.dataSize, HasSize: true}
	if out == nil {
		return r
	}
	seen := make(map[int]int)
	for _, e := range list {
		name := fmt.Sprintf("%08X.dat", e.key)
		if n, ok := seen[e.key]; ok {
			name = fmt.Sprintf("%s_%d", name, n)
		}
		seen[e.key]++
		if !out.CreateFile(name, e.value) {
			r.OK = false
			break
		}
	}
	return r
}

// inflateLimit is decompress_to_vec_with_limit of miniz_oxide: it decodes
// the raw deflate data at the start of src, and ignores the data after the
// last block. It fails when the data is not valid, ends early, or decodes to
// more than limit bytes. The inflater of walk/internal/flate accepts the
// data that miniz_oxide accepts.
func inflateLimit(src []byte, limit int) ([]byte, bool) {
	// The reader holds no resource, so it needs no Close.
	r := flate.NewReader(bytes.NewReader(src))
	var b bytes.Buffer
	n, err := io.Copy(&b, io.LimitReader(r, int64(limit)+1))
	if err != nil || n > int64(limit) {
		return nil, false
	}
	return b.Bytes(), true
}

type csmanInfo struct {
	compressed bool
	big        bool
	dataSize   int
	headerSize int
}

var csmanStruct = binwalk.Struct{
	{Name: "magic", Type: "u16"},
	{Name: "unknown1", Type: "u16"},
	{Name: "compressed_size", Type: "u32"},
	{Name: "unknown2", Type: "u32"},
	{Name: "decompressed_size", Type: "u32"},
}

// csmanHeader is parse_csman_header. The magic "CS" is a header in little
// endian order, and "SC" is in big endian order. The payload must end in
// the data, and a compressed payload must start with the first byte of
// zlib.
func csmanHeader(data []byte) (csmanInfo, bool) {
	const (
		compressedMagic = 0x78
		littleMagic     = 0x4353
	)
	h, ok := csmanStruct.Parse(data, true)
	if !ok {
		return csmanInfo{}, false
	}
	r := csmanInfo{big: true}
	if h["magic"] == littleMagic {
		// The second parse cannot fail, because the first one did not.
		h, _ = csmanStruct.Parse(data, false)
		r.big = false
	}
	r.dataSize = h["compressed_size"]
	r.headerSize = csmanStruct.Size()
	r.compressed = h["compressed_size"] != h["decompressed_size"]
	payload, ok := binwalk.Range(data, r.headerSize, r.headerSize+r.dataSize)
	if !ok {
		return csmanInfo{}, false
	}
	if r.compressed && (len(payload) == 0 || payload[0] != compressedMagic) {
		return csmanInfo{}, false
	}
	return r, true
}

// csmanEntry is CSManEntry.
type csmanEntry struct {
	size  int
	eof   bool
	key   int
	value []byte
}

var (
	csmanEntryStruct = binwalk.Struct{
		{Name: "key", Type: "u32"},
		{Name: "size", Type: "u16"},
	}
	csmanLastStruct = binwalk.Struct{{Name: "eof", Type: "u32"}}
)

// csmanParseEntry is parse_csman_entry: a key of 4 bytes, a size of 2 bytes
// and the value. The mark of the end is 4 zero bytes. As in binwalk, it is
// the end only when fewer than 6 bytes are left. Otherwise it is an entry
// with the key 0.
func csmanParseEntry(data []byte, big bool) (csmanEntry, bool) {
	if h, ok := csmanEntryStruct.Parse(data, big); ok {
		start := csmanEntryStruct.Size()
		v, ok := binwalk.Range(data, start, start+h["size"])
		if !ok {
			return csmanEntry{}, false
		}
		return csmanEntry{key: h["key"], value: v, size: start + len(v)}, true
	}
	if h, ok := csmanLastStruct.Parse(data, big); ok && h["eof"] == 0 {
		return csmanEntry{eof: true, size: csmanLastStruct.Size()}, true
	}
	return csmanEntry{}, false
}
