//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/lz4.rs and src/structures/lz4.rs of
// binwalk. binwalk extracts LZ4 with the program lz4, from
// src/extractors/lz4.rs, and extractLZ4 writes what lz4 -d writes.

package compressed

import (
	"encoding/binary"
	"fmt"
	"math/bits"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
	"github.com/xo/magic/walk/internal/lz4"
)

// LZ4 is the signature "lz4" of binwalk: a frame of LZ4.
var LZ4 = &walk.Signature{
	Name:        "lz4",
	Magic:       [][]byte{[]byte("\x04\x22\x4D\x18")},
	Description: "LZ4 compressed data",
	Parse:       parseLZ4,
	Extract:     extractLZ4,
}

// The flags of the frame descriptor of LZ4.
const (
	lz4FlagDictionary      = 0b00000001
	lz4FlagContentChecksum = 0b00000100
	lz4FlagContentSize     = 0b00001000
	lz4FlagBlockChecksum   = 0b00010000
	lz4FlagIndependent     = 0b00100000
)

// lz4Header is LZ4FileHeader of structures/lz4.rs.
type lz4Header struct {
	size            int // the bytes of the header, to the end of its checksum
	blockChecksum   bool
	contentChecksum bool
}

var lz4Struct = binwalk.Struct{{Name: "magic", Type: "u32"}, {Name: "flags", Type: "u8"}, {Name: "bd", Type: "u8"}}

// parseLZ4Header is parse_lz4_file_header. The checksum of the header is
// the second byte of the xxHash-32 of the descriptor.
func parseLZ4Header(data []byte) (lz4Header, bool) {
	h, ok := lz4Struct.Parse(data, false)
	if !ok || h["flags"]&0b00000010 != 0 || h["bd"]&0b10001111 != 0 {
		return lz4Header{}, false
	}
	end := 6
	if h["flags"]&lz4FlagContentSize != 0 {
		end += 8
	}
	if h["flags"]&lz4FlagDictionary != 0 {
		end += 4
	}
	if end >= len(data) || data[end] != byte(xxh32(data[4:end])>>8) {
		return lz4Header{}, false
	}
	return lz4Header{
		size:            end + 1,
		blockChecksum:   h["flags"]&lz4FlagBlockChecksum != 0,
		contentChecksum: h["flags"]&lz4FlagContentChecksum != 0,
	}, true
}

// parseLZ4 is lz4_parser.
func parseLZ4(data []byte, off int) (walk.Match, error) {
	h, ok := parseLZ4Header(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	blocks, ok := binwalk.From(data, off+h.size)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	n, ok := lz4DataSize(blocks, h.blockChecksum)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := h.size + n
	if h.contentChecksum {
		size += 4
	}
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.Medium,
		Description: fmt.Sprintf("LZ4 compressed data, total size: %d bytes", size),
	}, nil
}

// lz4DataSize is get_lz4_data_size, with parse_lz4_block_header. binwalk
// counts a checksum after the mark of the end too, which the format does
// not have, so the size is 4 bytes over when the blocks have checksums.
func lz4DataSize(data []byte, checksum bool) (int, bool) {
	size, last := 0, -1
	for binwalk.OffsetSafe(len(data), size, last) {
		if len(data)-size < 4 {
			break
		}
		block := binary.LittleEndian.Uint32(data[size:])
		last = size
		size += 4 + int(block&0x7FFFFFFF)
		if checksum {
			size += 4
		}
		if block == 0 {
			return size, true
		}
	}
	return 0, false
}

// extractLZ4 writes what lz4 -d writes for the carved match: the data of
// its frames in decompressed.bin. lz4 checks the checksums of the header,
// of each block and of the content, and the size of the content. A frame
// can be followed by more frames, by skippable frames, or by bytes that
// lz4 ignores, as the 4 bytes that binwalk adds to a frame with checksums
// of blocks.
func extractLZ4(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := parseLZ4(data, off)
	if err != nil {
		return walk.Extraction{}
	}
	in := data[off : off+m.Size]
	w := &appender{out: out, name: "decompressed.bin"}
	for frames := 0; len(in) > 0; frames++ {
		if len(in) < 4 {
			return walk.Extraction{} // lz4 cannot read the magic.
		}
		switch magic := binary.LittleEndian.Uint32(in); {
		case magic == 0x184D2204:
			n, ok := lz4Frame(in, w)
			if !ok {
				return walk.Extraction{}
			}
			in = in[n:]
		case magic&0xFFFFFFF0 == 0x184D2A50:
			if len(in) < 8 {
				return walk.Extraction{}
			}
			n := 8 + int(binary.LittleEndian.Uint32(in[4:]))
			in = in[min(n, len(in)):]
		case frames == 0:
			return walk.Extraction{}
		default:
			// lz4 warns of the bytes after the frames, and succeeds.
			in = nil
		}
	}
	return walk.Extraction{OK: true, Size: m.Size, HasSize: true}
}

// lz4Frame decodes the frame at the start of in to w, as LZ4F_decompress
// of the library of LZ4 decodes it, and returns its size.
func lz4Frame(in []byte, w *appender) (int, bool) {
	h, ok := parseLZ4Header(in)
	if !ok {
		return 0, false
	}
	flags, bd := in[4], in[5]
	if flags>>6 != 1 || bd>>4 < 4 {
		return 0, false // a version or a size of block that LZ4 does not know
	}
	maxBlock := 1 << (8 + 2*int(bd>>4))
	var content uint64
	if flags&lz4FlagContentSize != 0 {
		content = binary.LittleEndian.Uint64(in[6:])
	}
	var (
		sum     xxh32State
		history []byte // the last 64 KiB, which a linked block can refer to
		total   uint64
	)
	sum.reset()
	pos := h.size
	for {
		if len(in)-pos < 4 {
			return 0, false
		}
		block := binary.LittleEndian.Uint32(in[pos:])
		pos += 4
		if block == 0 {
			break
		}
		size := int(block & 0x7FFFFFFF)
		if size > maxBlock || len(in)-pos < size {
			return 0, false
		}
		src := in[pos : pos+size]
		pos += size
		if h.blockChecksum {
			if len(in)-pos < 4 || binary.LittleEndian.Uint32(in[pos:]) != xxh32(src) {
				return 0, false
			}
			pos += 4
		}
		dec := src
		if block&0x80000000 == 0 {
			var err error
			if flags&lz4FlagIndependent != 0 || len(history) == 0 {
				dec, err = lz4.Block(src, maxBlock)
			} else {
				dec, err = linkedBlock(history, src, maxBlock)
			}
			if err != nil {
				return 0, false
			}
		}
		sum.write(dec)
		total += uint64(len(dec))
		if _, err := w.Write(dec); err != nil {
			return 0, false
		}
		if flags&lz4FlagIndependent == 0 {
			history = append(history, dec...)
			if len(history) > 64<<10 {
				history = append(history[:0], history[len(history)-64<<10:]...)
			}
		}
	}
	if content != 0 && total != content {
		return 0, false
	}
	if h.contentChecksum {
		if len(in)-pos < 4 || binary.LittleEndian.Uint32(in[pos:]) != sum.sum() {
			return 0, false
		}
		pos += 4
	}
	return pos, true
}

// linkedBlock decodes a block that can refer to the bytes of the blocks
// before it. lz4.Block decodes one block alone, so linkedBlock gives it the
// history as literals: it adds them to the literals of the first sequence
// of the block, and drops them from the result.
func linkedBlock(history, src []byte, limit int) ([]byte, error) {
	tok := src[0]
	n, i := int(tok>>4), 1
	if n == 15 {
		for {
			if i >= len(src) {
				return lz4.Block(src, limit) // it fails as the block does
			}
			b := src[i]
			i++
			n += int(b)
			if b != 255 {
				break
			}
		}
	}
	n += len(history)
	merged := make([]byte, 0, len(src)+len(history)+n/255+2)
	if n < 15 {
		merged = append(merged, byte(n)<<4|tok&15)
	} else {
		merged = append(merged, 0xF0|tok&15)
		for n -= 15; n >= 255; n -= 255 {
			merged = append(merged, 255)
		}
		merged = append(merged, byte(n))
	}
	merged = append(merged, history...)
	merged = append(merged, src[i:]...)
	out, err := lz4.Block(merged, len(history)+limit)
	if err != nil {
		return nil, err
	}
	return out[len(history):], nil
}

// The primes of xxHash-32.
const (
	xxPrime1 uint32 = 2654435761
	xxPrime2 uint32 = 2246822519
	xxPrime3 uint32 = 3266489917
	xxPrime4 uint32 = 668265263
	xxPrime5 uint32 = 374761393
)

// xxh32 is the xxHash-32 of b, with the seed 0, which LZ4 uses for each of
// its checksums.
func xxh32(b []byte) uint32 {
	var s xxh32State
	s.reset()
	s.write(b)
	return s.sum()
}

// xxh32State is the state of an xxHash-32 that gets its data in parts.
type xxh32State struct {
	v     [4]uint32
	buf   [16]byte
	n     int // the bytes in buf
	total uint64
}

func (s *xxh32State) reset() {
	var seed uint32
	*s = xxh32State{v: [4]uint32{seed + xxPrime1 + xxPrime2, seed + xxPrime2, seed, seed - xxPrime1}}
}

func xxRound(v, lane uint32) uint32 {
	return bits.RotateLeft32(v+lane*xxPrime2, 13) * xxPrime1
}

func (s *xxh32State) stripe(b []byte) {
	for i := range s.v {
		s.v[i] = xxRound(s.v[i], binary.LittleEndian.Uint32(b[4*i:]))
	}
}

func (s *xxh32State) write(b []byte) {
	s.total += uint64(len(b))
	if s.n > 0 {
		c := copy(s.buf[s.n:], b)
		s.n += c
		b = b[c:]
		if s.n < 16 {
			return
		}
		s.stripe(s.buf[:])
		s.n = 0
	}
	for ; len(b) >= 16; b = b[16:] {
		s.stripe(b)
	}
	s.n = copy(s.buf[:], b)
}

func (s *xxh32State) sum() uint32 {
	var h uint32
	if s.total >= 16 {
		h = bits.RotateLeft32(s.v[0], 1) + bits.RotateLeft32(s.v[1], 7) +
			bits.RotateLeft32(s.v[2], 12) + bits.RotateLeft32(s.v[3], 18)
	} else {
		h = s.v[2] + xxPrime5
	}
	h += uint32(s.total)
	b := s.buf[:s.n]
	for ; len(b) >= 4; b = b[4:] {
		h += binary.LittleEndian.Uint32(b) * xxPrime3
		h = bits.RotateLeft32(h, 17) * xxPrime4
	}
	for _, c := range b {
		h += uint32(c) * xxPrime5
		h = bits.RotateLeft32(h, 11) * xxPrime1
	}
	h ^= h >> 15
	h *= xxPrime2
	h ^= h >> 13
	h *= xxPrime3
	h ^= h >> 16
	return h
}
