//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/arj.rs of binwalk-ng.
//
// binwalk-ng extracts an ARJ archive with 7z, from src/formats/sevenzip.rs,
// but the walker has no extractor for it. The size of the match is the
// size of the main header only. binwalk-ng carves that many bytes, and 7z
// cannot open them as an archive. So binwalk-ng extracts no file from an
// ARJ archive, and the walker extracts none too.

package archive

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// ARJ is the signature "arj" of binwalk-ng.
var ARJ = &walk.Signature{
	Name:        "arj",
	Magic:       [][]byte{[]byte("\x60\xea")},
	Description: "ARJ archive data",
	Parse:       parseARJ,
}

// The offsets and sizes of an ARJ header. The fixed fields are 34 bytes.
// The data block of the header starts after the magic and the size of the
// basic header, at arjData, and its fixed part is arjMinHeader bytes.
const (
	arjData      = 4
	arjFixed     = 34
	arjMinHeader = arjFixed - arjData
)

// The names of the hosts, the methods and the types of file of ARJ.
var (
	arjHosts = []string{
		"MS-DOS", "PRIMOS", "UNIX", "AMIGA", "MAC-OS", "OS/2", "APPLE GS",
		"ATARI ST", "NeXT", "VAX VMS", "WIN95", "WINNT",
	}
	arjMethods = map[byte]string{
		0: "stored", 1: "compressed most", 2: "compressed", 3: "compressed faster",
		4: "compressed fastest", 8: "no data", 9: "no data",
	}
	arjTypes = []string{
		"binary", "7-bit text", "comment header", "directory", "volume label",
		"chapter", "UNIX special",
	}
)

// parseARJ is arj_parser, with parse_arj_header.
func parseARJ(data []byte, off int) (walk.Match, error) {
	b := data[off:]
	if len(b) < arjFixed {
		return walk.Match{}, walk.ErrNoMatch
	}
	basic := int(binary.LittleEndian.Uint16(b[2:]))
	if basic < arjMinHeader || arjData+basic+4 > len(b) {
		return walk.Match{}, walk.ErrNoMatch
	}
	// The CRC of the header follows the data block of the header.
	crcStart := arjData + basic
	crc := binary.LittleEndian.Uint32(b[crcStart:])
	first, version, minVersion := int(b[4]), b[5], b[6]
	host, f, method, typ := b[7], b[8], b[9], b[10]
	// Writers of today give versions over 16, so binwalk-ng checks only that
	// the versions are not 0. The CRC of the header rejects random bytes.
	if version == 0 || minVersion == 0 || version < minVersion {
		return walk.Match{}, walk.ErrNoMatch
	}
	flags := "no password"
	if f&0x01 != 0 {
		flags = "password"
	}
	if f&0x04 != 0 {
		flags += "|multi-volume"
	}
	if f&0x10 != 0 {
		flags += "|slash-switched"
	}
	if f&0x20 != 0 {
		flags += "|backup"
	}
	methodName, ok := arjMethods[method]
	if int(host) >= len(arjHosts) || !ok || int(typ) >= len(arjTypes) {
		return walk.Match{}, walk.ErrNoMatch
	}
	// binwalk-ng reads each size as an i32, and rejects a negative one.
	date := binary.LittleEndian.Uint32(b[12:])
	compressed := int32(binary.LittleEndian.Uint32(b[16:]))
	uncompressed := int32(binary.LittleEndian.Uint32(b[20:]))
	if compressed < 0 || uncompressed < 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	// The size of the first header is the offset of the name in the data
	// block of the header.
	if first < arjMinHeader || first >= basic {
		return walk.Match{}, walk.ErrNoMatch
	}
	name := binwalk.CString(b[arjData+first : crcStart])
	size := crcStart + 4
	if binwalk.CRC32(b[arjData:crcStart]) != crc {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       size,
		Confidence: walk.High,
		Description: fmt.Sprintf("ARJ archive data, header size: %d, version %d, minimum version to extract: %d, "+
			"flags: %s, compression method: %s, file type: %s, original name: %s, original file date: %s, "+
			"compressed file size: %d, uncompressed file size: %d, os: %s",
			size, version, minVersion, flags, methodName, arjTypes[typ], name,
			arjDate(date), compressed, uncompressed, arjHosts[host]),
		Declined: arjTypes[typ] != "comment header",
	}, nil
}

// arjDate is the date of an ARJ header, a date and time of MS-DOS, as
// dos_datetime_to_epoch and epoch_to_string give it. When the date or the
// time is not valid, binwalk-ng takes the field as seconds since the epoch.
func arjDate(dos uint32) string {
	year := 1980 + int(dos>>25&0x7f)
	month := time.Month(dos >> 21 & 0x0f)
	day := int(dos >> 16 & 0x1f)
	hour, minute, second := int(dos>>11&0x1f), int(dos>>5&0x3f), int(dos&0x1f)*2
	t := time.Date(year, month, day, hour, minute, second, 0, time.UTC)
	if t.Year() != year || t.Month() != month || t.Day() != day ||
		t.Hour() != hour || t.Minute() != minute || t.Second() != second {
		t = time.Unix(int64(dos), 0).UTC()
	}
	return t.Format(time.DateTime)
}
