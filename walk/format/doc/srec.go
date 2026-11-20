//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/srec.rs of binwalk-ng.

package doc

import (
	"fmt"

	"github.com/xo/magic/walk"
)

// SRecord is the signature "srecord" of binwalk-ng. The magic is a header
// record that holds the text "HDR" in hex.
var SRecord = &walk.Signature{
	Name:        "srecord",
	Magic:       [][]byte{[]byte("S00600004844521B")},
	Description: "Motorola S-record",
	Parse:       parseSRecord,
	Extract:     extractSRecord,
}

// SRecordGeneric is the signature "srecord_generic" of binwalk-ng. Its
// magic is short, so it matches only at the start of the data.
var SRecordGeneric = &walk.Signature{
	Name:        "srecord_generic",
	Short:       true,
	Magic:       [][]byte{[]byte("S0")},
	Description: "Motorola S-record (generic)",
	Parse:       parseSRecord,
	Extract:     extractSRecord,
}

// parseSRecord is srec_parser. It reads records up to the first that it
// cannot read, the first with a bad checksum, or the first terminal record.
// The records must hold at least one data record. The line end of the
// terminal record gives the origin OS.
func parseSRecord(data []byte, off int) (walk.Match, error) {
	rest := data[off:]
	os := "Unknown"
	sawData := false
	for {
		r, next, ok := takeSRecord(rest)
		if !ok || !r.checksumValid() {
			break
		}
		rest = next
		if r.typ.isData() {
			sawData = true
		}
		if r.typ.isTerminal() {
			os = "Unix"
			if r.crlf {
				os = "Windows"
			}
			break
		}
	}
	if !sawData {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := len(data) - off - len(rest)
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("Motorola S-record, origin OS: %s, total size: %d bytes", os, size),
	}, nil
}

// extractSRecord is extract_srec: it decodes the data records to
// s-record.bin. As binwalk-ng, it writes each record at its address in the
// file, so a gap between records is a hole, which reads as zeros.
func extractSRecord(data []byte, off int, out *walk.Output) walk.Extraction {
	const name = "s-record.bin"
	if out != nil && !out.CreateFile(name, nil) {
		return walk.Extraction{}
	}
	rest := data[off:]
	sawData := false
	for {
		r, next, ok := takeSRecord(rest)
		if !ok || !r.checksumValid() {
			break
		}
		if r.typ.isData() {
			sawData = true
			if len(r.hexData) > 0 && out != nil && !out.WriteFileAt(name, r.decode(), int64(r.addr)) {
				return walk.Extraction{}
			}
		}
		rest = next
		if r.typ.isTerminal() {
			break
		}
	}
	consumed := len(data) - off - len(rest)
	return walk.Extraction{OK: consumed > 0 && sawData, Size: consumed, HasSize: true}
}

// srecType is RecordType: the digit after the "S" of a record.
type srecType byte

// addrSize is addr_size: the bytes of the address of the record.
func (t srecType) addrSize() int {
	switch t {
	case '2', '6', '8':
		return 3
	case '3', '7':
		return 4
	}
	return 2
}

// isTerminal is is_terminal.
func (t srecType) isTerminal() bool {
	return t == '7' || t == '8' || t == '9'
}

// isData is is_data.
func (t srecType) isData() bool {
	return t == '1' || t == '2' || t == '3'
}

// srecRecord is Record.
type srecRecord struct {
	typ srecType
	// sum is the sum of the bytes of the checksum, without the data.
	sum     byte
	addr    uint32
	hexData []byte // the data, in hex digits that are not yet checked
	crlf    bool   // the line ends in CR LF, and not in LF
}

// checksumValid is checksum_valid: the data must be hex, and the sum of the
// bytes must be 0xFF.
func (r srecRecord) checksumValid() bool {
	sum := r.sum
	for i := 0; i < len(r.hexData); i += 2 {
		b, ok := hexByte(r.hexData[i:])
		if !ok {
			return false
		}
		sum += b
	}
	return sum == 0xFF
}

// decode gives the bytes of the data, which checksumValid checked.
func (r srecRecord) decode() []byte {
	out := make([]byte, len(r.hexData)/2)
	for i := range out {
		out[i], _ = hexByte(r.hexData[2*i:])
	}
	return out
}

// takeSRecord is take_srec_record: it reads one record and its line end,
// and the empty lines after it, and returns the rest of b.
func takeSRecord(b []byte) (srecRecord, []byte, bool) {
	const headerSize = 4
	if len(b) < headerSize || b[0] != 'S' {
		return srecRecord{}, nil, false
	}
	t := srecType(b[1])
	if t < '0' || t > '9' || t == '4' {
		return srecRecord{}, nil, false
	}
	n, ok := hexByte(b[2:])
	if !ok {
		return srecRecord{}, nil, false
	}
	count, addrSize, rest := int(n), t.addrSize(), b[headerSize:]
	// The count holds the address and the checksum, and the rest holds the
	// hex digits of the count and a new line.
	if count < addrSize+1 || len(rest) < count*2+1 {
		return srecRecord{}, nil, false
	}
	r := srecRecord{typ: t, sum: n}
	for i := range addrSize {
		a, ok := hexByte(rest[2*i:])
		if !ok {
			return srecRecord{}, nil, false
		}
		r.sum += a
		r.addr = r.addr<<8 | uint32(a)
	}
	rest = rest[2*addrSize:]
	dataLen := count - addrSize - 1
	r.hexData = rest[:2*dataLen]
	rest = rest[2*dataLen:]
	c, ok := hexByte(rest)
	if !ok {
		return srecRecord{}, nil, false
	}
	r.sum += c
	rest = rest[2:]
	r.crlf, rest, ok = takeNewline(rest)
	if !ok {
		return srecRecord{}, nil, false
	}
	for {
		_, next, ok := takeNewline(rest)
		if !ok {
			break
		}
		rest = next
	}
	return r, rest, true
}

// takeNewline is take_newline: it takes an LF or a CR LF from the start of
// b, and reports whether it was a CR LF.
func takeNewline(b []byte) (bool, []byte, bool) {
	switch {
	case len(b) >= 1 && b[0] == '\n':
		return false, b[1:], true
	case len(b) >= 2 && b[0] == '\r' && b[1] == '\n':
		return true, b[2:], true
	}
	return false, b, false
}

// hexByte is HexByte::get: the byte of the two hex digits at the start of
// b. The caller makes sure that b holds two bytes.
func hexByte(b []byte) (byte, bool) {
	hi, ok1 := hexDigit(b[0])
	lo, ok2 := hexDigit(b[1])
	return hi<<4 | lo, ok1 && ok2
}

// hexDigit is decode_hex_digit.
func hexDigit(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}
