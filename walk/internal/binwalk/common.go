//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/common.rs and src/structures/common.rs of binwalk.

// Package binwalk holds the helpers that the parsers of binwalk share: the
// reader of structures, the check of offsets, and the readers of strings.
package binwalk

import (
	"encoding/binary"
	"hash/crc32"
	"strings"
	"time"
	"unicode/utf8"
)

// Field is one field of a structure: its name and its type, one of "u8",
// "u16", "u24", "u32" and "u64".
type Field struct {
	Name string
	Type string
}

// Struct is a structure of fields with no padding.
type Struct []Field

// Fields is a parsed structure: the value of each field by its name.
type Fields map[string]int

// typeSize is type_to_size of structures/common.rs.
func typeSize(typ string) (int, bool) {
	switch typ {
	case "u8":
		return 1, true
	case "u16":
		return 2, true
	case "u24":
		return 3, true
	case "u32":
		return 4, true
	case "u64":
		return 8, true
	}
	return 0, false
}

// Size is size of structures/common.rs: the bytes of the fields of a known
// type. A field of an unknown type counts as 0 bytes, as in binwalk.
func (s Struct) Size() int {
	n := 0
	for _, f := range s {
		if size, ok := typeSize(f.Type); ok {
			n += size
		}
	}
	return n
}

// Parse is parse of structures/common.rs. It reads the fields of s from the
// start of data, in big-endian order when big is true. It fails when data is
// shorter than s, or a field has an unknown type.
//
// binwalk holds each value in a usize. A u64 above the largest int becomes
// negative here, which binwalk never sees on a 64-bit system.
func (s Struct) Parse(data []byte, big bool) (Fields, bool) {
	size := s.Size()
	if len(data) < size {
		return nil, false
	}
	var order binary.ByteOrder = binary.LittleEndian
	if big {
		order = binary.BigEndian
	}
	out := make(Fields, len(s))
	off := 0
	for _, f := range s {
		n, ok := typeSize(f.Type)
		if !ok {
			return nil, false
		}
		b := data[off : off+n]
		var v uint64
		switch n {
		case 1:
			v = uint64(b[0])
		case 2:
			v = uint64(order.Uint16(b))
		case 3:
			if big {
				v = uint64(b[0])<<16 | uint64(b[1])<<8 | uint64(b[2])
			} else {
				v = uint64(b[2])<<16 | uint64(b[1])<<8 | uint64(b[0])
			}
		case 4:
			v = uint64(order.Uint32(b))
		case 8:
			v = order.Uint64(b)
		}
		out[f.Name] = int(v)
		off += n
	}
	return out, true
}

// OffsetSafe is is_offset_safe of common.rs. It reports whether next is
// inside the available bytes, and past the previous offset when there is one.
// A previous offset of -1 means none.
func OffsetSafe(available, next, previous int) bool {
	if previous >= 0 && previous >= next {
		return false
	}
	return next < available
}

// From is data.get(start..) of Rust: the bytes from start, or false when
// start is past the end.
func From(data []byte, start int) ([]byte, bool) {
	if start < 0 || start > len(data) {
		return nil, false
	}
	return data[start:], true
}

// Range is data.get(start..end) of Rust: the bytes from start to end, or
// false when the range does not fit in data.
func Range(data []byte, start, end int) ([]byte, bool) {
	if start < 0 || start > end || end > len(data) {
		return nil, false
	}
	return data[start:end], true
}

// CStringBytes is get_cstring_bytes of common.rs: the bytes up to the first
// NUL, or all of them.
func CStringBytes(data []byte) []byte {
	for i, c := range data {
		if c == 0 {
			return data[:i]
		}
	}
	return data
}

// CString is get_cstring of common.rs: the bytes up to the first NUL as a
// string, as String::from_utf8_lossy of Rust gives it.
func CString(data []byte) string {
	return Lossy(CStringBytes(data))
}

// Lossy is String::from_utf8_lossy of Rust. Each maximal part of b that is
// not valid UTF-8, but starts a valid sequence, becomes one U+FFFD. So
// "\xe2\x82" gives one U+FFFD, and "\xff\xff" gives two. Go's
// strings.ToValidUTF8 gives one for any run of bad bytes.
func Lossy(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	for len(b) > 0 {
		r, n := utf8.DecodeRune(b)
		if r != utf8.RuneError || n > 1 {
			sb.Write(b[:n])
			b = b[n:]
			continue
		}
		sb.WriteRune(utf8.RuneError)
		b = b[maximalSubpart(b):]
	}
	return sb.String()
}

// maximalSubpart is the length of the maximal ill-formed subpart at the
// start of b, which does not hold a valid sequence: the lead byte, and the
// continuation bytes that a valid sequence could have after it.
func maximalSubpart(b []byte) int {
	var need int
	lo, hi := byte(0x80), byte(0xbf)
	switch c := b[0]; {
	case c >= 0xc2 && c <= 0xdf:
		need = 1
	case c == 0xe0:
		need, lo = 2, 0xa0
	case c == 0xed:
		need, hi = 2, 0x9f
	case c >= 0xe1 && c <= 0xef:
		need = 2
	case c == 0xf0:
		need, lo = 3, 0x90
	case c == 0xf4:
		need, hi = 3, 0x8f
	case c >= 0xf1 && c <= 0xf3:
		need = 3
	default:
		return 1
	}
	n := 1
	for ; n <= need && n < len(b); n++ {
		if b[n] < lo || b[n] > hi {
			break
		}
		lo, hi = 0x80, 0xbf
	}
	return n
}

// IsASCIINumber is is_ascii_number of common.rs.
func IsASCIINumber(c byte) bool {
	return c >= '0' && c <= '9'
}

// IsPrintableASCII is is_printable_ascii of common.rs. It counts the bytes
// from 0x0A to 0x7E, as binwalk does.
func IsPrintableASCII(c byte) bool {
	return c >= 0x0a && c <= 0x7e
}

// CRC32 is crc32 of common.rs, the CRC-32 of zlib.
func CRC32(data []byte) uint32 {
	return crc32.ChecksumIEEE(data)
}

// EpochString is epoch_to_string of common.rs: the time in UTC, as
// "1970-01-01 00:00:00".
func EpochString(epoch uint32) string {
	return time.Unix(int64(epoch), 0).UTC().Format(time.DateTime)
}

// Truncate is the precision of a string in the format of Rust, as {:.100}:
// at most n characters of s.
func Truncate(s string, n int) string {
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
}
