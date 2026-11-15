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
// This file translates src/encoding.c, src/ascmagic.c of file 5.48 to Go (D29).

package engine

import (
	"github.com/xo/magic/internal/rules"
)

// This file is what encoding.c and ascmagic.c do: they decide whether data
// is text, in which encoding, and what kind of text.

// The classes of text_chars in encoding.c.
const (
	charF = iota // never in text
	charT        // in plain ASCII text
	charI        // in ISO-8859 text
	charX        // in non-ISO extended ASCII, such as Mac or IBM PC
)

// textChars is text_chars of encoding.c.
var textChars = func() [256]uint8 {
	var t [256]uint8
	for _, c := range []byte{7, 8, 9, 10, 11, 12, 13, 26, 27} {
		t[c] = charT
	}
	for c := 0x20; c < 0x7f; c++ {
		t[c] = charT
	}
	for c := 0x80; c < 0xa0; c++ {
		t[c] = charX
	}
	t[0x85] = charT
	for c := 0xa0; c <= 0xff; c++ {
		t[c] = charI
	}
	return t
}()

// encodingResult is what file_encoding finds.
type encodingResult struct {
	text     bool   // the return value: the data looks like text
	code     string // the name of the encoding, for the description
	codeMime string // the name of the encoding, for MIME
	typ      string // "text" or "binary"
	ubuf     []rune // the characters, decoded
}

// encoding is file_encoding of encoding.c. It looks at the first
// encodingMax bytes.
func encoding(buf []byte) encodingResult {
	r := encodingResult{text: true, typ: "text", code: "unknown", codeMime: "binary"}
	if buf == nil {
		r.text = false
		return r
	}
	if len(buf) > encodingMax {
		buf = buf[:encodingMax]
	}
	var ok bool
	switch {
	case looksTable(buf, &r.ubuf, func(t uint8) bool { return t == charT }):
		if looksUTF7(buf) > 0 {
			r.ubuf = r.ubuf[:0]
			r.code, r.codeMime = "Unicode text, UTF-7", "utf-7"
		} else {
			r.code, r.codeMime = "ASCII", "us-ascii"
		}
	case len(buf) > 3 && buf[0] == 0xef && buf[1] == 0xbb && buf[2] == 0xbf && looksUTF8(buf[3:], &r.ubuf) > 0:
		r.code, r.codeMime = "Unicode text, UTF-8 (with BOM)", "utf-8"
	case looksUTF8(buf, &r.ubuf) > 1:
		r.code, r.codeMime = "Unicode text, UTF-8", "utf-8"
	default:
		if t := looksUCS32(buf, &r.ubuf); t != 0 {
			if t == 1 {
				r.code, r.codeMime = "Unicode text, UTF-32, little-endian", "utf-32le"
			} else {
				r.code, r.codeMime = "Unicode text, UTF-32, big-endian", "utf-32be"
			}
			break
		}
		if t := looksUCS16(buf, &r.ubuf); t != 0 {
			if t == 1 {
				r.code, r.codeMime = "Unicode text, UTF-16, little-endian", "utf-16le"
			} else {
				r.code, r.codeMime = "Unicode text, UTF-16, big-endian", "utf-16be"
			}
			break
		}
		if looksTable(buf, &r.ubuf, func(t uint8) bool { return t == charT || t == charI }) {
			r.code, r.codeMime = "ISO-8859", "iso-8859-1"
			break
		}
		if looksTable(buf, &r.ubuf, func(t uint8) bool { return t != charF }) {
			r.code, r.codeMime = "Non-ISO extended-ASCII", "unknown-8bit"
			break
		}
		nbuf := make([]byte, len(buf))
		for i, c := range buf {
			nbuf[i] = ebcdicToASCII[c]
		}
		if ok = looksTable(nbuf, &r.ubuf, func(t uint8) bool { return t == charT }); ok {
			r.code, r.codeMime = "EBCDIC", "ebcdic"
		} else if looksTable(nbuf, &r.ubuf, func(t uint8) bool { return t == charT || t == charI }) {
			r.code, r.codeMime = "International EBCDIC", "ebcdic"
		} else {
			r.text = false
			r.typ = "binary"
			r.code, r.codeMime = "unknown", "binary"
		}
	}
	return r
}

// looksTable is the LOOKS macro of encoding.c: whether each byte has a
// class that ok accepts. It decodes the bytes into ubuf as it goes, and
// leaves what it decoded when it fails, as libmagic does.
func looksTable(buf []byte, ubuf *[]rune, ok func(uint8) bool) bool {
	*ubuf = (*ubuf)[:0]
	for _, c := range buf {
		if !ok(textChars[c]) {
			return false
		}
		*ubuf = append(*ubuf, rune(c))
	}
	return true
}

// looksUTF7 is looks_utf7.
func looksUTF7(buf []byte) int {
	if len(buf) > 4 && buf[0] == '+' && buf[1] == '/' && buf[2] == 'v' {
		switch buf[3] {
		case '8', '9', '+', '/':
			return 1
		}
	}
	return -1
}

// looksUTF8 is file_looks_utf8: -1 for bytes that are not UTF-8, 0 for
// UTF-8 with control characters, 1 for ASCII and 2 for UTF-8 with a
// character above ASCII. A sequence that the end cuts counts as good.
func looksUTF8(buf []byte, ubuf *[]rune) int {
	if ubuf != nil {
		*ubuf = (*ubuf)[:0]
	}
	gotone, ctrl := false, false
	for i := 0; i < len(buf); i++ {
		c := buf[i]
		if c&0x80 == 0 {
			if textChars[c] != charT {
				ctrl = true
			}
			if ubuf != nil {
				*ubuf = append(*ubuf, rune(c))
			}
			continue
		}
		if c&0x40 == 0 {
			return -1
		}
		var lo, hi byte = 0x80, 0xbf
		switch {
		case c < 0xc2 || c > 0xf4:
			return -1
		case c == 0xe0:
			lo = 0xa0
		case c == 0xed:
			hi = 0x9f
		case c == 0xf0:
			lo = 0x90
		case c == 0xf4:
			hi = 0x8f
		}
		var u rune
		following := 0
		switch {
		case c&0x20 == 0:
			u, following = rune(c&0x1f), 1
		case c&0x10 == 0:
			u, following = rune(c&0x0f), 2
		default:
			u, following = rune(c&0x07), 3
		}
		for n := range following {
			i++
			if i >= len(buf) {
				return utf8Result(ctrl, gotone)
			}
			if n == 0 && (buf[i] < lo || buf[i] > hi) {
				return -1
			}
			if buf[i]&0x80 == 0 || buf[i]&0x40 != 0 {
				return -1
			}
			u = u<<6 + rune(buf[i]&0x3f)
		}
		if ubuf != nil {
			*ubuf = append(*ubuf, u)
		}
		gotone = true
	}
	return utf8Result(ctrl, gotone)
}

func utf8Result(ctrl, gotone bool) int {
	switch {
	case ctrl:
		return 0
	case gotone:
		return 2
	}
	return 1
}

// looksUCS16 is looks_ucs16: 1 for UTF-16 little-endian, 2 for big-endian,
// with a byte order mark, and 0 for neither.
func looksUCS16(bf []byte, ubuf *[]rune) int {
	if len(bf) < 2 {
		return 0
	}
	bigend := 0
	switch {
	case bf[0] == 0xff && bf[1] == 0xfe:
	case bf[0] == 0xfe && bf[1] == 0xff:
		bigend = 1
	default:
		return 0
	}
	*ubuf = (*ubuf)[:0]
	var hi uint32
	for i := 2; i+1 < len(bf); i += 2 {
		var uc uint32
		if bigend == 1 {
			uc = uint32(bf[i+1]) | uint32(bf[i])<<8
		} else {
			uc = uint32(bf[i]) | uint32(bf[i+1])<<8
		}
		if uc == 0xfffe || uc == 0xffff || uc >= 0xfdd0 && uc <= 0xfdef {
			return 0
		}
		if hi != 0 {
			if uc < 0xdc00 || uc > 0xdfff {
				return 0
			}
			uc = 0x10000 + 0x400*(hi-1) + (uc - 0xdc00)
			hi = 0
		}
		if uc < 128 && textChars[uc] != charT {
			return 0
		}
		*ubuf = append(*ubuf, rune(uc))
		if uc >= 0xd800 && uc <= 0xdbff {
			hi = uc - 0xd800 + 1
		}
		if uc >= 0xdc00 && uc <= 0xdfff {
			return 0
		}
	}
	return 1 + bigend
}

// looksUCS32 is looks_ucs32.
func looksUCS32(bf []byte, ubuf *[]rune) int {
	if len(bf) < 4 {
		return 0
	}
	bigend := 0
	switch {
	case bf[0] == 0xff && bf[1] == 0xfe && bf[2] == 0 && bf[3] == 0:
	case bf[0] == 0 && bf[1] == 0 && bf[2] == 0xfe && bf[3] == 0xff:
		bigend = 1
	default:
		return 0
	}
	*ubuf = (*ubuf)[:0]
	for i := 4; i+3 < len(bf); i += 4 {
		var u uint32
		if bigend == 1 {
			u = uint32(bf[i+3]) | uint32(bf[i+2])<<8 | uint32(bf[i+1])<<16 | uint32(bf[i])<<24
		} else {
			u = uint32(bf[i]) | uint32(bf[i+1])<<8 | uint32(bf[i+2])<<16 | uint32(bf[i+3])<<24
		}
		*ubuf = append(*ubuf, rune(u))
		if u == 0xfffe {
			return 0
		}
		if u < 128 && textChars[u] != charT {
			return 0
		}
	}
	return 1 + bigend
}

// ebcdicToASCII is ebcdic_to_ascii of encoding.c.
var ebcdicToASCII = [256]byte{
	0, 1, 2, 3, 156, 9, 134, 127, 151, 141, 142, 11, 12, 13, 14, 15,
	16, 17, 18, 19, 157, 133, 8, 135, 24, 25, 146, 143, 28, 29, 30, 31,
	128, 129, 130, 131, 132, 10, 23, 27, 136, 137, 138, 139, 140, 5, 6, 7,
	144, 145, 22, 147, 148, 149, 150, 4, 152, 153, 154, 155, 20, 21, 158, 26,
	' ', 160, 161, 162, 163, 164, 165, 166, 167, 168, 213, '.', '<', '(', '+', '|',
	'&', 169, 170, 171, 172, 173, 174, 175, 176, 177, '!', '$', '*', ')', ';', '~',
	'-', '/', 178, 179, 180, 181, 182, 183, 184, 185, 203, ',', '%', '_', '>', '?',
	186, 187, 188, 189, 190, 191, 192, 193, 194, '`', ':', '#', '@', '\'', '=', '"',
	195, 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 196, 197, 198, 199, 200, 201,
	202, 'j', 'k', 'l', 'm', 'n', 'o', 'p', 'q', 'r', '^', 204, 205, 206, 207, 208,
	209, 229, 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', 210, 211, 212, '[', 214, 215,
	216, 217, 218, 219, 220, 221, 222, 223, 224, 225, 226, 227, 228, ']', 230, 231,
	'{', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 232, 233, 234, 235, 236, 237,
	'}', 'J', 'K', 'L', 'M', 'N', 'O', 'P', 'Q', 'R', 238, 239, 240, 241, 242, 243,
	'\\', 159, 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z', 244, 245, 246, 247, 248, 249,
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 250, 251, 252, 253, 254, 255,
}

// maxLineLen is MAXLINELEN of ascmagic.c.
const maxLineLen = 300

// trimNULs is trim_nuls: the length of buf without the NULs at its end,
// but at least 1.
func trimNULs(buf []byte) int {
	n := len(buf)
	for n > 1 && buf[n-1] == 0 {
		n--
	}
	return n
}

// ascmagic is file_ascmagic: it describes text, with the rules for text
// first.
func (st *state) ascmagic(b *buffer, text bool) int {
	n := trimNULs(b.data)
	// Keep an even length for UTF-16 little-endian.
	if n&1 == 1 && len(b.data)&1 == 0 {
		n++
	}
	bb := &buffer{data: b.data[:n], file: b.file}
	enc := encoding(bb.data)
	if !enc.text {
		return 0
	}
	return st.ascmagicWithEncoding(bb, enc.ubuf, enc.code, enc.typ, text)
}

// ascmagicWithEncoding is file_ascmagic_with_encoding.
func (st *state) ascmagicWithEncoding(b *buffer, ubuf []rune, code, typ string, text bool) int {
	nbytes := trimNULs(b.data)
	if nbytes <= 1 {
		return 0
	}
	rv := -1
	needSep := false
	if len(ubuf) > 0 && st.flags&NoCheckSoft == 0 {
		// Run the rules for text on the text as UTF-8.
		u, ok := encodeUTF8(ubuf)
		if !ok {
			return 0
		}
		bb := &buffer{data: u, file: b.file}
		if rv = st.softmagic(bb, rules.FlagTextTest, text); rv == 0 {
			rv = -1
		} else {
			needSep = true
		}
		if st.flags&(Apple|Extension) != 0 {
			if rv == -1 {
				return 0
			}
			return 1
		}
	}
	if st.flags&(Apple|Extension) != 0 {
		return 0
	}

	var nCRLF, nLF, nCR, nNEL, longLines int
	seenCR, escapes, backspace := false, false, false
	lastLineEnd := -1
	for i, c := range ubuf {
		if c == '\n' {
			if seenCR {
				nCRLF++
			} else {
				nLF++
			}
			lastLineEnd = i
		} else if seenCR {
			nCR++
		}
		seenCR = c == '\r'
		if seenCR {
			lastLineEnd = i
		}
		if c == 0x85 {
			nNEL++
			lastLineEnd = i
		}
		if i > lastLineEnd+maxLineLen {
			if ll := i - lastLineEnd; ll > longLines {
				longLines = ll
			}
		}
		if c == 033 {
			escapes = true
		}
		if c == '\b' {
			backspace = true
		}
	}
	if seenCR && nCR == 0 && nCRLF == 0 {
		nCR++
	}
	if typ == "binary" {
		return 0
	}
	printed := len(st.out)
	if st.flags&Mime != 0 {
		if st.flags&MimeType != 0 {
			if printed != 0 {
				if st.flags&Continue == 0 {
					return 1
				}
				if needSep {
					st.printf(separator)
				}
			}
			st.printf("text/plain")
		}
		return 1
	}

	executable := false
	if printed != 0 {
		switch {
		case st.replaceSuffix(" text", ", "):
		case st.replaceSuffix(" text executable", ", "):
			executable = true
		default:
			st.printf(", ")
		}
	}
	st.printf("%s", code)
	st.printf(" %s", typ)
	if executable {
		st.printf(" executable")
	}
	if longLines != 0 {
		st.printf(", with very long lines (%d)", longLines)
	}
	if nCRLF == 0 && nCR == 0 && nNEL == 0 && nLF == 0 || nCRLF != 0 || nCR != 0 || nNEL != 0 {
		st.printf(", with")
		if nCRLF == 0 && nCR == 0 && nNEL == 0 && nLF == 0 {
			st.printf(" no")
		} else {
			if nCRLF != 0 {
				st.printf(" CRLF")
				if nCR != 0 || nLF != 0 || nNEL != 0 {
					st.printf(",")
				}
			}
			if nCR != 0 {
				st.printf(" CR")
				if nLF != 0 || nNEL != 0 {
					st.printf(",")
				}
			}
			if nLF != 0 {
				st.printf(" LF")
				if nNEL != 0 {
					st.printf(",")
				}
			}
			if nNEL != 0 {
				st.printf(" NEL")
			}
		}
		st.printf(" line terminators")
	}
	if escapes {
		st.printf(", with escape sequences")
	}
	if backspace {
		st.printf(", with overstriking")
	}
	return 1
}

// replaceSuffix is file_replace with a pattern that ends in $: it replaces
// suffix at the end of the output with rep.
func (st *state) replaceSuffix(suffix, rep string) bool {
	n := len(st.out) - len(suffix)
	if n < 0 || string(st.out[n:]) != suffix {
		return false
	}
	st.out = append(st.out[:n], rep...)
	return true
}

// encodeUTF8 is encode_utf8 of ascmagic.c, which writes forms of up to six
// bytes.
func encodeUTF8(ubuf []rune) ([]byte, bool) {
	out := make([]byte, 0, len(ubuf))
	for _, r := range ubuf {
		u := uint32(r)
		switch {
		case u <= 0x7f:
			out = append(out, byte(u))
		case u <= 0x7ff:
			out = append(out, byte(u>>6)+0xc0, byte(u&0x3f)+0x80)
		case u <= 0xffff:
			out = append(out, byte(u>>12)+0xe0, byte(u>>6&0x3f)+0x80, byte(u&0x3f)+0x80)
		case u <= 0x1fffff:
			out = append(out, byte(u>>18)+0xf0, byte(u>>12&0x3f)+0x80, byte(u>>6&0x3f)+0x80, byte(u&0x3f)+0x80)
		case u <= 0x3ffffff:
			out = append(out, byte(u>>24)+0xf8, byte(u>>18&0x3f)+0x80, byte(u>>12&0x3f)+0x80, byte(u>>6&0x3f)+0x80, byte(u&0x3f)+0x80)
		case u <= 0x7fffffff:
			out = append(out, byte(u>>30)+0xfc, byte(u>>24&0x3f)+0x80, byte(u>>18&0x3f)+0x80, byte(u>>12&0x3f)+0x80, byte(u>>6&0x3f)+0x80, byte(u&0x3f)+0x80)
		default:
			return nil, false
		}
	}
	return out, true
}
