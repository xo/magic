// Copyright (c) Ian F. Darwin 1986-1995.
// Software written by Ian F. Darwin and others;
// maintained 1995-present by Christos Zoulas and others.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice immediately at the beginning of the file, without modification,
//    this list of conditions, and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED. IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE FOR
// ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
// This file translates src/file.h, src/apprentice.c of file to Go, at the
// commit that docs/upstream/upstreams.tsv names (D29, D48).

package rules

import (
	"bytes"
	"encoding/binary"
)

// Entry is one line of a rule, compiled. Its fields are the fields of struct
// magic in file.h of file 5.48, because the compiler must give the same
// entries as libmagic, and Encode writes them in its layout. The form of the
// tables that the engine reads can differ (D35).
type Entry struct {
	Flag      Flag
	ContLevel uint8
	// Factor and FactorOp are the strength annotation !:strength, such as
	// "+ 10". FactorOp is 0 when the rule has none.
	Factor   uint8
	FactorOp byte
	// Reln is the relation: '=', '!', '<', '>', '&', '^' or 'x'.
	Reln   byte
	Vallen uint8
	Type   Type
	// InType, InOp and InOffset describe an indirect offset, such as
	// (0x3c.l+4).
	InType   Type
	InOp     uint8
	MaskOp   uint8
	Offset   int32
	InOffset int32
	Lineno   uint32
	// Mask is the mask of a number type. A string type uses the same bits
	// for StrRange, the low 32, and StrFlags, the high 32, as the union of
	// file.h does.
	Mask  uint64
	Value [128]byte
	Desc  [64]byte
	MIME  [80]byte
	Apple [8]byte
	Ext   [120]byte
}

// Flag holds the flags of an entry, as the flag field of struct magic.
type Flag uint16

// The flags of an entry.
const (
	FlagIndir       Flag = 0x01 // the offset is indirect, (...)
	FlagOffAdd      Flag = 0x02 // the offset is relative to the last match, &
	FlagIndirOffAdd Flag = 0x04 // the indirect offset is relative, (&...)
	FlagUnsigned    Flag = 0x08 // the comparison is unsigned
	FlagNoSpace     Flag = 0x10 // no space before the description, \b
	FlagBinTest     Flag = 0x20 // the rule is for binary data
	FlagTextTest    Flag = 0x40 // the rule is for text
	FlagOffNegative Flag = 0x80 // the offset is from the end of the data
	FlagOffPositive Flag = 0x100
)

// The flags of a string type, in StrFlags.
const (
	StrCompactWhitespace         uint32 = 1 << 0
	StrCompactOptionalWhitespace uint32 = 1 << 1
	StrIgnoreLowercase           uint32 = 1 << 2
	StrIgnoreUppercase           uint32 = 1 << 3
	RegexOffsetStart             uint32 = 1 << 4
	StrTextTest                  uint32 = 1 << 5
	StrBinTest                   uint32 = 1 << 6
	PString1LE                   uint32 = 1 << 7
	PString2BE                   uint32 = 1 << 8
	PString2LE                   uint32 = 1 << 9
	PString4BE                   uint32 = 1 << 10
	PString4LE                   uint32 = 1 << 11
	RegexLineCount               uint32 = 1 << 11
	PStringLengthIncludesItself  uint32 = 1 << 12
	StrTrim                      uint32 = 1 << 13
	StrFullWord                  uint32 = 1 << 14

	// pstringLen is PSTRING_LEN of file.h: the bits that give the size of
	// the length of a Pascal string.
	pstringLen = PString1LE | PString2LE | PString2BE | PString4LE | PString4BE

	// IndirectRelative is the flag of the modifier /r of an indirect rule.
	IndirectRelative uint32 = 1 << 0
)

// The operators of a mask or an indirect offset, in MaskOp and InOp.
const (
	OpAnd      uint8 = 0
	OpOr       uint8 = 1
	OpXor      uint8 = 2
	OpAdd      uint8 = 3
	OpMinus    uint8 = 4
	OpMultiply uint8 = 5
	OpDivide   uint8 = 6
	OpModulo   uint8 = 7
	OpMask     uint8 = 0x07
	OpSigned   uint8 = 0x20
	OpInverse  uint8 = 0x40
	OpIndirect uint8 = 0x80
)

// StrRange is the range of a string type, such as the 100 of search/100.
func (e *Entry) StrRange() uint32 { return uint32(e.Mask) }

// StrFlags holds the modifiers of a string type.
func (e *Entry) StrFlags() uint32 { return uint32(e.Mask >> 32) }

func (e *Entry) setStrRange(v uint32) { e.Mask = e.Mask&^0xffffffff | uint64(v) }

func (e *Entry) setStrFlags(v uint32) { e.Mask = e.Mask&0xffffffff | uint64(v)<<32 }

// DescString returns the description, up to its first NUL.
func (e *Entry) DescString() string { return cString(e.Desc[:]) }

// MIMEString returns the MIME type, up to its first NUL.
func (e *Entry) MIMEString() string { return cString(e.MIME[:]) }

// cString returns b up to its first NUL.
func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

// EntrySize is the size of struct magic, and of each entry of magic.mgc.
const EntrySize = 432

// Encode writes e in the layout of struct magic on a little-endian machine,
// which is the layout of magic.mgc on such a machine.
func (e *Entry) Encode() [EntrySize]byte {
	var b [EntrySize]byte
	le := binary.LittleEndian
	le.PutUint16(b[0:], uint16(e.Flag))
	b[2] = e.ContLevel
	b[3] = e.Factor
	b[4] = e.Reln
	b[5] = e.Vallen
	b[6] = byte(e.Type)
	b[7] = byte(e.InType)
	b[8] = e.InOp
	b[9] = e.MaskOp
	// b[10] is cond, which is 0 because ENABLE_CONDITIONALS is not set.
	b[11] = e.FactorOp
	le.PutUint32(b[12:], uint32(e.Offset))
	le.PutUint32(b[16:], uint32(e.InOffset))
	le.PutUint32(b[20:], e.Lineno)
	le.PutUint64(b[24:], e.Mask)
	copy(b[32:160], e.Value[:])
	copy(b[160:224], e.Desc[:])
	copy(b[224:304], e.MIME[:])
	copy(b[304:312], e.Apple[:])
	copy(b[312:432], e.Ext[:])
	return b
}
