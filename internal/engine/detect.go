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
// Copyright (c) 2018 Christos Zoulas
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE NETBSD FOUNDATION, INC. AND CONTRIBUTORS
// ``AS IS'' AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED
// TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR
// PURPOSE ARE DISCLAIMED.  IN NO EVENT SHALL THE FOUNDATION OR CONTRIBUTORS
// BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
// CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
// SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
// INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
// CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
// ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
// POSSIBILITY OF SUCH DAMAGE.
//
// Copyright (c) 2019 Christos Zoulas
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE NETBSD FOUNDATION, INC. AND CONTRIBUTORS
// ``AS IS'' AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED
// TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR
// PURPOSE ARE DISCLAIMED.  IN NO EVENT SHALL THE FOUNDATION OR CONTRIBUTORS
// BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
// CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
// SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
// INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
// CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
// ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
// POSSIBILITY OF SUCH DAMAGE.
//
// Copyright (c) 2023 Christos Zoulas
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE NETBSD FOUNDATION, INC. AND CONTRIBUTORS
// ``AS IS'' AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED
// TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR
// PURPOSE ARE DISCLAIMED.  IN NO EVENT SHALL THE FOUNDATION OR CONTRIBUTORS
// BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
// CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
// SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
// INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
// CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
// ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
// POSSIBILITY OF SUCH DAMAGE.
//
// This file translates src/is_tar.c, src/is_json.c, src/is_csv.c,
// src/is_simh.c of file to Go, at the commit that docs/upstream/upstreams.tsv
// names (D29, D48).

package engine

import "bytes"

// This file holds the small detectors of libmagic: is_tar.c, is_json.c,
// is_csv.c and is_simh.c. Each one returns 1 when it printed a result, and 0
// when the data is not its format.

// tar is file_is_tar.
func (st *state) tar(buf []byte) int {
	if st.flags&(Apple|Extension) != 0 {
		return 0
	}
	t := isTar(buf)
	if t < 1 || t > 3 {
		return 0
	}
	mime := st.flags & Mime
	if mime == MimeEncoding {
		return 1
	}
	if mime != 0 {
		st.printf("application/x-tar")
		return 1
	}
	st.printf("%s", [...]string{"tar archive", "POSIX tar archive", "POSIX tar archive (GNU)"}[t-1])
	return 1
}

// isTar is is_tar: 0 when the checksum of the header is bad, 1 for an old
// tar file, 2 for a POSIX one and 3 for a GNU one.
func isTar(buf []byte) int {
	const recordSize = 512
	if len(buf) < recordSize {
		return 0
	}
	name := buf[:100]
	// A Gentoo GLEP 78 binary package is left to the rules.
	const gpkg = "/gpkg-1"
	if nul := bytes.IndexByte(name, 0); nul > len(gpkg) && string(name[nul-len(gpkg):nul]) == gpkg {
		return 0
	}
	chksum := buf[148:156]
	recsum := fromOct(chksum)
	sum := 0
	for _, c := range buf[:recordSize] {
		sum += int(c)
	}
	// The checksum counts its own field as blanks. The field is char,
	// which is signed in the reference.
	for _, c := range chksum {
		sum -= int(int8(c))
	}
	sum += ' ' * len(chksum)
	if sum != recsum {
		return 0
	}
	magic := buf[257:265]
	switch {
	case cstrEq(magic, "ustar  "):
		return 3
	case cstrEq(magic, "ustar"):
		return 2
	}
	return 1
}

// cstrEq is strncmp(b, s, len(b)) == 0, for a field b of fixed size.
func cstrEq(b []byte, s string) bool {
	for i := range b {
		var c byte
		if i < len(s) {
			c = s[i]
		}
		if b[i] != c {
			return false
		}
		if c == 0 {
			return true
		}
	}
	return true
}

// fromOct is from_oct of is_tar.c: the octal number of a field, or -1.
func fromOct(where []byte) int {
	i := 0
	for i < len(where) && isSpace(where[i]) {
		i++
	}
	if i == len(where) {
		return -1
	}
	v := 0
	for i < len(where) && where[i] >= '0' && where[i] <= '7' {
		v = v<<3 | int(where[i]-'0')
		i++
	}
	if i < len(where) && where[i] != 0 && !isSpace(where[i]) {
		return -1
	}
	return v
}

// The kinds of JSON value that is_json.c counts.
const (
	jsonArray = iota
	jsonConstant
	jsonNumber
	jsonObject
	jsonString
	jsonArrayN
	jsonMax
)

// json is file_is_json.
func (st *state) json(buf []byte) int {
	if st.flags&(Apple|Extension) != 0 {
		return 0
	}
	var counts [jsonMax]int
	p := 0
	jt := jsonParse(buf, &p, &counts, 0)
	if jt == 0 {
		return 0
	}
	mime := st.flags & Mime
	if mime == MimeEncoding {
		return 1
	}
	if mime != 0 {
		if jt == 1 {
			st.printf("application/json")
		} else {
			st.printf("application/x-ndjson")
		}
		return 1
	}
	if jt == 1 {
		st.printf("JSON text data")
	} else {
		st.printf("New Line Delimited JSON text data")
	}
	return 1
}

func jsonIsSpace(c byte) bool { return c == ' ' || c == '\n' || c == '\r' || c == '\t' }

func jsonSkipSpace(b []byte, p int) int {
	for p < len(b) && jsonIsSpace(b[p]) {
		p++
	}
	return p
}

func jsonParseString(b []byte, pp *int) bool {
	p := *pp
	ue := len(b)
	for p < ue {
		c := b[p]
		p++
		switch c {
		case 0:
			*pp = p
			return false
		case '\\':
			if p == ue {
				*pp = p
				return false
			}
			c = b[p]
			p++
			switch c {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				continue
			case 'u':
				if ue-p < 4 {
					*pp = ue
					return false
				}
				for range 4 {
					x := b[p]
					p++
					if !isXDigit(x) {
						*pp = p
						return false
					}
				}
				continue
			default:
				*pp = p
				return false
			}
		case '"':
			*pp = p
			return true
		}
	}
	*pp = p
	return false
}

func jsonParseArray(b []byte, pp *int, st *[jsonMax]int, lvl int) bool {
	p := *pp
	for p < len(b) {
		p = jsonSkipSpace(b, p)
		if p == len(b) {
			break
		}
		if b[p] == ']' {
			st[jsonArrayN]++
			*pp = p + 1
			return true
		}
		if jsonParse(b, &p, st, lvl+1) == 0 {
			break
		}
		if p == len(b) {
			break
		}
		switch b[p] {
		case ',':
			p++
			continue
		case ']':
			st[jsonArrayN]++
			*pp = p + 1
			return true
		}
		break
	}
	*pp = p
	return false
}

func jsonParseObject(b []byte, pp *int, st *[jsonMax]int, lvl int) bool {
	p := *pp
	for p < len(b) {
		p = jsonSkipSpace(b, p)
		if p == len(b) {
			break
		}
		if b[p] == '}' {
			*pp = p + 1
			return true
		}
		c := b[p]
		p++
		if c != '"' {
			break
		}
		if !jsonParseString(b, &p) {
			break
		}
		p = jsonSkipSpace(b, p)
		if p == len(b) {
			break
		}
		c = b[p]
		p++
		if c != ':' {
			break
		}
		if jsonParse(b, &p, st, lvl+1) == 0 {
			break
		}
		if p == len(b) {
			break
		}
		c = b[p]
		p++
		switch c {
		case ',':
			continue
		case '}':
			*pp = p
			return true
		}
		// libmagic sets the position back by one here, then overwrites it.
		break
	}
	*pp = p
	return false
}

func jsonParseNumber(b []byte, pp *int) bool {
	p := *pp
	ue := len(b)
	got := false
	if p == ue {
		return false
	}
	if b[p] == '-' {
		p++
	}
	for ; p < ue && isDigit(b[p]); p++ {
		got = true
	}
	if p == ue {
		goto out
	}
	if b[p] == '.' {
		p++
	}
	for ; p < ue && isDigit(b[p]); p++ {
		got = true
	}
	if p == ue {
		goto out
	}
	if got && (b[p] == 'e' || b[p] == 'E') {
		p++
		got = false
		if p == ue {
			goto out
		}
		if b[p] == '+' || b[p] == '-' {
			p++
		}
		for ; p < ue && isDigit(b[p]); p++ {
			got = true
		}
	}
out:
	*pp = p
	return got
}

// jsonParseConst is json_parse_const: it compares the rest of str from the
// letter after the first, and accepts a constant that the end of the data
// cuts.
func jsonParseConst(b []byte, pp *int, str string) bool {
	p := *pp
	*pp += len(str) - 1
	if *pp > len(b) {
		*pp = len(b)
	}
	for i := 1; p < len(b) && i < len(str); i++ {
		if b[p] != str[i] {
			return false
		}
		p++
	}
	return true
}

// jsonParse is json_parse. At level 0 it returns 1 for one JSON value, 2
// for several in a row, and 0 for none.
func jsonParse(b []byte, pp *int, st *[jsonMax]int, lvl int) int {
	p := jsonSkipSpace(b, *pp)
	ouc := p
	rv := false
	if p == len(b) {
		goto out
	}
	if lvl > 500 {
		return 0
	}
	{
		c := b[p]
		p++
		var t int
		switch c {
		case '"':
			rv = jsonParseString(b, &p)
			t = jsonString
		case '[':
			rv = jsonParseArray(b, &p, st, lvl+1)
			t = jsonArray
		case '{':
			rv = jsonParseObject(b, &p, st, lvl+1)
			t = jsonObject
		case 't':
			rv = jsonParseConst(b, &p, "true")
			t = jsonConstant
		case 'f':
			rv = jsonParseConst(b, &p, "false")
			t = jsonConstant
		case 'n':
			rv = jsonParseConst(b, &p, "null")
			t = jsonConstant
		default:
			p--
			rv = jsonParseNumber(b, &p)
			t = jsonNumber
		}
		if rv {
			st[t]++
		}
		p = jsonSkipSpace(b, p)
	}
out:
	*pp = p
	if lvl == 0 {
		if !rv {
			return 0
		}
		if p == len(b) {
			if st[jsonArrayN] != 0 || st[jsonObject] != 0 {
				return 1
			}
			return 0
		}
		if b[ouc] == b[p] && jsonParse(b, &p, st, 1) != 0 {
			if st[jsonArrayN] != 0 || st[jsonObject] != 0 {
				return 2
			}
		}
		return 0
	}
	if rv {
		return 1
	}
	return 0
}

// csv is file_is_csv.
func (st *state) csv(buf []byte, looksText bool, code string) int {
	if !looksText || st.flags&(Apple|Extension) != 0 {
		return 0
	}
	if !csvParse(buf) {
		return 0
	}
	mime := st.flags & Mime
	if mime == MimeEncoding {
		return 1
	}
	if mime != 0 {
		st.printf("text/csv")
		return 1
	}
	if code == "" {
		// libmagic has no encoding when its test is off.
		st.printf("CSV text")
		return 1
	}
	st.printf("CSV %s text", code)
	return 1
}

// csvLines is CSV_LINES: how many lines csvParse reads.
const csvLines = 10

func csvParse(b []byte) bool {
	nf, tf, nl := 0, 0, 0
	for p := 0; p < len(b); {
		c := b[p]
		p++
		switch c {
		case '"':
			// Eat to the matching quote.
			quote := false
			for p < len(b) {
				c := b[p]
				p++
				if c != '"' {
					if quote {
						p--
						break
					}
					continue
				}
				quote = !quote
			}
		case ',':
			nf++
		case '\n':
			nl++
			if nl == csvLines {
				return tf > 1 && tf == nf
			}
			if tf == 0 {
				if nf == 0 {
					return false
				}
				tf = nf
			} else if tf != nf {
				return false
			}
			nf = 0
		}
	}
	return tf >= 1 && nl >= 2
}

// simh is file_is_simh.
func (st *state) simh(buf []byte) int {
	if st.flags&(Apple|Extension) != 0 {
		return 0
	}
	if !simhParse(buf) {
		return 0
	}
	mime := st.flags & Mime
	if mime == MimeEncoding {
		return 1
	}
	if mime != 0 {
		st.printf("application/SIMH-tape-data")
		return 1
	}
	st.printf("SIMH tape data")
	return 1
}

// simhTapemarks is SIMH_TAPEMARKS.
const simhTapemarks = 10

func simhParse(b []byte) bool {
	p, nt, nr := 0, 0, 0
	getlen := func() (uint32, bool) {
		n := le.Uint32(b[p:])
		p += 4
		if n == 0xffffffff {
			return n, true
		}
		bad := n&0x00ffffff != n&0x0fffffff
		n &= 0x00ffffff
		if n&1 != 0 {
			n++
		}
		return n, !bad
	}
	for len(b)-p >= 4 {
		n, ok := getlen()
		if !ok {
			return false
		}
		if (nt > 0 || nr > 0) && n == 0xffffffff {
			break
		}
		if n == 0 {
			nt++
			if nt == simhTapemarks {
				break
			}
			continue
		}
		p += int(n)
		if len(b)-p < 4 {
			break
		}
		c, ok := getlen()
		if !ok || n != c {
			return false
		}
		nr++
	}
	if nt*4 == p {
		return false
	}
	return nr != 0
}
