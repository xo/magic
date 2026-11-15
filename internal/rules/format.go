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
// This file translates src/apprentice.c of file 5.48 to Go (D29).

package rules

import (
	"bytes"
	"errors"
	"fmt"
)

// The kinds of printf format that a type takes, as FILE_FMT_* of file.h.
const (
	fmtNone = iota
	fmtNum
	fmtStr
	fmtQuad
	fmtFloat
	fmtDouble
)

// formatOf returns the kind of format that t takes, as the format column of
// type_tbl in apprentice.c. name, use and clear take none.
func formatOf(t Type) int {
	switch t {
	case Invalid, Default, Name, Use, Clear:
		return fmtNone
	case Byte, Short, Long, BEShort, BELong, LEShort, LELong, MELong, LEID3, BEID3, Indirect:
		return fmtNum
	case Quad, LEQuad, BEQuad, Offset:
		return fmtQuad
	case Float, BEFloat, LEFloat:
		return fmtFloat
	case Double, BEDouble, LEDouble:
		return fmtDouble
	}
	return fmtStr
}

// checkFormat is check_format of apprentice.c: a description holds at most
// one printf format, and it fits the type of the line.
func checkFormat(e *Entry) error {
	desc := e.Desc[:]
	if i := bytes.IndexByte(desc, 0); i >= 0 {
		desc = desc[:i]
	}
	i := bytes.IndexByte(desc, '%')
	if i < 0 {
		return nil
	}
	if formatOf(e.Type) == fmtNone {
		return fmt.Errorf("no format string for %v with description %q", e.Type, desc)
	}
	rest, err := checkFormatType(desc[i+1:], e.Type)
	if err != nil {
		return fmt.Errorf("printf format is %v for type %v in description %q", err, e.Type, desc)
	}
	if bytes.IndexByte(rest, '%') >= 0 {
		return fmt.Errorf("too many format strings for %v with description %q", e.Type, desc)
	}
	return nil
}

var (
	errMissing = errors.New("missing format spec")
	errInvalid = errors.New("not valid")
	errTooLong = errors.New("too long")
)

// checkFormatType is check_format_type of apprentice.c, without
// STRICT_FORMAT, which the build does not set. It returns the text after the
// format.
func checkFormatType(p []byte, t Type) ([]byte, error) {
	if len(p) == 0 {
		return p, errMissing
	}
	i := 0
	// width reads a width or a precision of at most 5 digits and 1024.
	width := func() error {
		n, cnt := 0, 0
		for isDigit(at(p, i)) {
			n = n*10 + int(p[i]-'0')
			cnt++
			i++
		}
		if cnt > 5 || n > 1024 {
			return errTooLong
		}
		return nil
	}
	switch formatOf(t) {
	case fmtNum, fmtQuad:
		quad := formatOf(t) == fmtQuad
		h := 0
		if !quad && t == Byte {
			h = 2
		}
		for at(p, i) != 0 && bytes.IndexByte([]byte("+-.#"), p[i]) >= 0 {
			i++
		}
		if err := width(); err != nil {
			return p, err
		}
		if at(p, i) == '.' {
			i++
		}
		if err := width(); err != nil {
			return p, err
		}
		if quad {
			if at(p, i) != 'l' || at(p, i+1) != 'l' {
				return p, errInvalid
			}
			i += 2
		}
		c := at(p, i)
		i++
		switch c {
		case 'c':
			if h == 2 {
				return p[i:], nil
			}
			return p, errInvalid
		case 'i', 'd', 'u', 'o', 'x', 'X':
			return p[i:], nil
		}
		return p, errInvalid
	case fmtFloat, fmtDouble:
		if at(p, i) == '-' {
			i++
		}
		if at(p, i) == '.' {
			i++
		}
		if err := width(); err != nil {
			return p, err
		}
		if at(p, i) == '.' {
			i++
		}
		if err := width(); err != nil {
			return p, err
		}
		switch at(p, i) {
		case 'e', 'E', 'f', 'F', 'g', 'G':
			return p[i+1:], nil
		}
		return p, errInvalid
	case fmtStr:
		if at(p, i) == '-' {
			i++
		}
		for isDigit(at(p, i)) {
			i++
		}
		if at(p, i) == '.' {
			i++
			for isDigit(at(p, i)) {
				i++
			}
		}
		if at(p, i) == 's' {
			return p[i+1:], nil
		}
		return p, errInvalid
	}
	return p, errInvalid
}
