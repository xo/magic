// Copyright (c) 2000 The NetBSD Foundation, Inc.
// All rights reserved.
//
// This code was contributed to The NetBSD Foundation by Allen Briggs.
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
// Copyright (c) Christos Zoulas 2003.
// All Rights Reserved.
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
// This file translates src/fmtcheck.c, src/softmagic.c, src/funcs.c of file 5.48 to Go (D29).

package engine

import (
	"math"
	"strconv"
	"strings"
)

// This file prints as the printf of glibc does, for the formats that the
// descriptions of the rules use: d, i, u, o, x, X, c, s, e, E, f, F, g, G
// and %%, with flags, a width, a precision and a length.

// carg is one argument of cprintf. A C integer is passed as bits, and the
// length in the format says how many of them printf reads, as a C vararg.
type carg struct {
	kind int // argInt, argFloat or argString
	i    uint64
	f    float64
	s    []byte
}

const (
	argInt = iota
	argFloat
	argString
)

func cint(v uint64) carg                  { return carg{kind: argInt, i: v} }
func cfloat(v float64) carg               { return carg{kind: argFloat, f: v} }
func cstring(s []byte) carg               { return carg{kind: argString, s: s} }
func cstringS(s string) carg              { return carg{kind: argString, s: []byte(s)} }
func cprintfS(f string, a ...carg) string { return string(cprintf([]byte(f), a...)) }

// cprintf formats args with format, as snprintf of glibc does.
func cprintf(format []byte, args ...carg) []byte {
	var out []byte
	ai := 0
	next := func() carg {
		if ai < len(args) {
			ai++
			return args[ai-1]
		}
		return carg{}
	}
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' {
			out = append(out, c)
			continue
		}
		i++
		if i >= len(format) {
			break
		}
		if format[i] == '%' {
			out = append(out, '%')
			continue
		}
		var sp spec
		for ; i < len(format) && strings.IndexByte("-+ #0'", format[i]) >= 0; i++ {
			switch format[i] {
			case '-':
				sp.minus = true
			case '+':
				sp.plus = true
			case ' ':
				sp.space = true
			case '#':
				sp.alt = true
			case '0':
				sp.zero = true
			}
		}
		for ; i < len(format) && isDigit(format[i]); i++ {
			sp.width = sp.width*10 + int(format[i]-'0')
		}
		sp.prec = -1
		if i < len(format) && format[i] == '.' {
			sp.prec = 0
			for i++; i < len(format) && isDigit(format[i]); i++ {
				sp.prec = sp.prec*10 + int(format[i]-'0')
			}
		}
		bits := 32
	length:
		for ; i < len(format); i++ {
			switch format[i] {
			case 'h':
				if bits == 16 {
					bits = 8
				} else {
					bits = 16
				}
			case 'l', 'q', 'j', 'z', 't', 'L':
				bits = 64
			default:
				break length
			}
		}
		if i >= len(format) {
			break
		}
		out = sp.format(out, format[i], bits, next())
	}
	return out
}

// spec is one conversion of a format.
type spec struct {
	minus, plus, space, alt, zero bool
	width, prec                   int
}

// pad writes body with the width of the spec. zero pads with zeros after
// the sign and the prefix.
func (sp *spec) pad(out []byte, prefix, body string, zero bool) []byte {
	n := len(prefix) + len(body)
	if n >= sp.width {
		return append(append(out, prefix...), body...)
	}
	fill := sp.width - n
	switch {
	case sp.minus:
		out = append(append(out, prefix...), body...)
		return append(out, strings.Repeat(" ", fill)...)
	case zero:
		out = append(out, prefix...)
		out = append(out, strings.Repeat("0", fill)...)
		return append(out, body...)
	}
	out = append(out, strings.Repeat(" ", fill)...)
	return append(append(out, prefix...), body...)
}

func (sp *spec) format(out []byte, conv byte, bits int, a carg) []byte {
	switch conv {
	case 'd', 'i':
		v := sext(true, bits, a.i)
		prefix := ""
		u := uint64(v)
		if v < 0 {
			prefix, u = "-", -uint64(v)
		} else if sp.plus {
			prefix = "+"
		} else if sp.space {
			prefix = " "
		}
		return sp.integer(out, prefix, u, 10, false)
	case 'u', 'o', 'x', 'X':
		u := a.i
		if bits < 64 {
			u &= 1<<bits - 1
		}
		switch conv {
		case 'u':
			return sp.integer(out, "", u, 10, false)
		case 'o':
			return sp.integer(out, "", u, 8, false)
		case 'x':
			prefix := ""
			if sp.alt && u != 0 {
				prefix = "0x"
			}
			return sp.integer(out, prefix, u, 16, false)
		}
		prefix := ""
		if sp.alt && u != 0 {
			prefix = "0X"
		}
		return sp.integer(out, prefix, u, 16, true)
	case 'c':
		return sp.pad(out, "", string([]byte{byte(a.i)}), false)
	case 's':
		s := cstr(a.s)
		if sp.prec >= 0 && sp.prec < len(s) {
			s = s[:sp.prec]
		}
		return sp.pad(out, "", string(s), false)
	case 'e', 'E', 'f', 'F', 'g', 'G':
		return sp.float(out, conv, a.f)
	}
	return out
}

// integer writes u in base, after prefix.
func (sp *spec) integer(out []byte, prefix string, u uint64, base int, upper bool) []byte {
	digits := strconv.FormatUint(u, base)
	if upper {
		digits = strings.ToUpper(digits)
	}
	if sp.prec == 0 && u == 0 {
		digits = ""
	}
	if base == 8 && sp.alt && (digits == "" || digits[0] != '0') {
		digits = "0" + digits
	}
	if sp.prec > len(digits) {
		digits = strings.Repeat("0", sp.prec-len(digits)) + digits
	}
	return sp.pad(out, prefix, digits, sp.zero && sp.prec < 0)
}

// float writes v as %e, %f or %g of glibc.
func (sp *spec) float(out []byte, conv byte, v float64) []byte {
	upper := conv == 'E' || conv == 'F' || conv == 'G'
	prefix := ""
	if math.Signbit(v) {
		prefix = "-"
		v = -v
	} else if sp.plus {
		prefix = "+"
	} else if sp.space {
		prefix = " "
	}
	if math.IsInf(v, 0) || math.IsNaN(v) {
		body := "inf"
		if math.IsNaN(v) {
			body = "nan"
		}
		if upper {
			body = strings.ToUpper(body)
		}
		return sp.pad(out, prefix, body, false)
	}
	prec := sp.prec
	if prec < 0 {
		prec = 6
	}
	var body string
	switch conv {
	case 'e', 'E':
		body = strconv.FormatFloat(v, 'e', prec, 64)
		if sp.alt && prec == 0 {
			body = strings.Replace(body, "e", ".e", 1)
		}
	case 'f', 'F':
		body = strconv.FormatFloat(v, 'f', prec, 64)
		if sp.alt && prec == 0 {
			body += "."
		}
	default:
		if prec == 0 {
			prec = 1
		}
		// The exponent that %e would print decides the style.
		e := strconv.FormatFloat(v, 'e', prec-1, 64)
		x, _ := strconv.Atoi(e[strings.IndexByte(e, 'e')+1:])
		if x < -4 || x >= prec {
			body = e
			if !sp.alt {
				mant, exp, _ := strings.Cut(body, "e")
				body = trimZeros(mant) + "e" + exp
			}
		} else {
			body = strconv.FormatFloat(v, 'f', prec-1-x, 64)
			if !sp.alt {
				body = trimZeros(body)
			}
		}
	}
	if upper {
		body = strings.ToUpper(body)
	}
	return sp.pad(out, prefix, body, sp.zero)
}

// trimZeros removes the zeros at the end of the fraction, and the point
// when no fraction is left, as %g does.
func trimZeros(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// The kinds of argument that fmtcheck compares, as enum __e_fmtcheck_types
// of fmtcheck.c.
const (
	fcStart = iota
	fcShort
	fcInt
	fcLong
	fcQuad
	fcShortPointer
	fcIntPointer
	fcLongPointer
	fcQuadPointer
	fcDouble
	fcLongDouble
	fcString
	fcWidth
	fcPrecision
	fcDone
	fcUnknown
)

// fmtNextFromPrecision is get_next_format_from_precision.
func fmtNextFromPrecision(f string, p int) (int, int) {
	sh, lg, quad, ld := false, false, false, false
	switch at([]byte(f), p) {
	case 'h':
		p++
		sh = true
	case 'l':
		p++
		if p >= len(f) {
			return p, fcUnknown
		}
		if f[p] == 'l' {
			p++
			quad = true
		} else {
			lg = true
		}
	case 'q':
		p++
		quad = true
	case 'L':
		p++
		ld = true
	}
	if p >= len(f) {
		return p, fcUnknown
	}
	c := f[p]
	any := sh || lg || quad || ld
	switch {
	case strings.IndexByte("diouxX", c) >= 0:
		switch {
		case ld:
			return p, fcUnknown
		case lg:
			return p, fcLong
		case quad:
			return p, fcQuad
		}
		return p, fcInt
	case c == 'n':
		switch {
		case ld:
			return p, fcUnknown
		case sh:
			return p, fcShortPointer
		case lg:
			return p, fcLongPointer
		case quad:
			return p, fcQuadPointer
		}
		return p, fcIntPointer
	case strings.IndexByte("DOU", c) >= 0:
		if any {
			return p, fcUnknown
		}
		return p, fcLong
	case strings.IndexByte("eEfg", c) >= 0:
		if ld {
			return p, fcLongDouble
		}
		if sh || lg || quad {
			return p, fcUnknown
		}
		return p, fcDouble
	case c == 'c':
		if any {
			return p, fcUnknown
		}
		return p, fcInt
	case c == 's':
		if any {
			return p, fcUnknown
		}
		return p, fcString
	case c == 'p':
		if any {
			return p, fcUnknown
		}
		return p, fcLong
	}
	return p, fcUnknown
}

// fmtNextFromWidth is get_next_format_from_width.
func fmtNextFromWidth(f string, p int) (int, int) {
	if at([]byte(f), p) == '.' {
		p++
		if at([]byte(f), p) == '*' {
			return p, fcPrecision
		}
		for p < len(f) && isDigit(f[p]) {
			p++
		}
		if p >= len(f) {
			return p, fcUnknown
		}
	}
	return fmtNextFromPrecision(f, p)
}

// fmtNext is get_next_format.
func fmtNext(f string, p, eft int) (int, int) {
	switch eft {
	case fcWidth:
		return fmtNextFromWidth(f, p+1)
	case fcPrecision:
		return fmtNextFromPrecision(f, p+1)
	}
	for {
		i := strings.IndexByte(f[min(p, len(f)):], '%')
		if i < 0 {
			return len(f), fcDone
		}
		p += i + 1
		if p >= len(f) {
			return p, fcUnknown
		}
		if f[p] != '%' {
			break
		}
		p++
	}
	for p < len(f) && strings.IndexByte("#0- +", f[p]) >= 0 {
		p++
	}
	if at([]byte(f), p) == '*' {
		return p, fcWidth
	}
	for p < len(f) && isDigit(f[p]) {
		p++
	}
	if p >= len(f) {
		return p, fcUnknown
	}
	return fmtNextFromWidth(f, p)
}

// fmtcheck is fmtcheck of libmagic: f1 when its conversions take the same
// arguments as the ones of f2, and f2 otherwise.
func fmtcheck(f1, f2 string) (string, bool) {
	p1, p2 := 0, 0
	t1, t2 := fcStart, fcStart
	for {
		p1, t1 = fmtNext(f1, p1, t1)
		if t1 == fcDone {
			return f1, true
		}
		if t1 == fcUnknown {
			return f2, false
		}
		p2, t2 = fmtNext(f2, p2, t2)
		if t1 != t2 {
			return f2, false
		}
	}
}

// checkFmtString is check_fmt of softmagic.c: whether desc formats its
// value as a string, with %s and an optional width.
func checkFmtString(desc []byte) bool {
	for i := 0; i < len(desc); i++ {
		if desc[i] != '%' {
			continue
		}
		j := i + 1
		for j < len(desc) && (desc[j] == '-' || desc[j] == '.' || isDigit(desc[j])) {
			j++
		}
		if j < len(desc) && desc[j] == 's' {
			return true
		}
	}
	return false
}

// checkPrintfFormat is file_checkfmt of funcs.c: it refuses a format that
// file_printf must not run. It returns the reason, or "".
func checkPrintfFormat(f []byte) string {
	for p := 0; p < len(f); p++ {
		if f[p] != '%' {
			continue
		}
		p++
		if p >= len(f) {
			return "incomplete % format"
		}
		if f[p] == '%' {
			continue
		}
		for p < len(f) && strings.IndexByte("#0.'+- ", f[p]) >= 0 {
			p++
		}
		if at(f, p) == '*' {
			return "* not allowed in format"
		}
		if r := checkField(f, &p, "width"); r != "" {
			return r
		}
		if at(f, p) == '.' {
			p++
			if r := checkField(f, &p, "precision"); r != "" {
				return r
			}
		}
		if !isAlpha(at(f, p)) {
			return "bad format char: " + string([]byte{at(f, p)})
		}
	}
	return ""
}

// checkField is file_checkfield: a width or a precision below 1024.
func checkField(f []byte, p *int, what string) string {
	n := 0
	for isDigit(at(f, *p)) {
		n = n*10 + int(f[*p]-'0')
		*p++
		if n > 1024 {
			break
		}
	}
	if n < 1024 {
		return ""
	}
	return "field " + what + " too large: " + strconv.Itoa(n)
}
