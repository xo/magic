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
// This file translates src/funcs.c, src/softmagic.c of file to Go, at the
// commit that docs/upstream/upstreams.tsv names (D29, D48).

package engine

import (
	"github.com/xo/magic/internal/rules"
)

// buffer is file_buffer of funcs.c: it runs the detectors on b, in the
// order of libmagic, until one of them gives a result. nb is the length of
// the data. It returns -1 on an error.
func (st *state) buffer(b *buffer, nb int) int {
	m, rv := 0, 0
	// The mode is the one of the file, and 0 for a buffer.
	st.mode = b.mode
	codeMime := "binary"
	def := "data"

	switch nb {
	case 0:
		def = "empty"
		return st.simple(m, nb, def, codeMime)
	case 1:
		def = "very short file (no magic)"
		return st.simple(m, nb, def, codeMime)
	}

	looksText := false
	code := ""
	if st.flags&NoCheckEncoding == 0 {
		enc := encoding(b.data)
		looksText, code, codeMime = enc.text, enc.code, enc.codeMime
	}

	// Compressed data. Its result skips the separator and the encoding at
	// the end, as the goto done_encoding of libmagic does.
	if st.flags&NoCheckCompress == 0 {
		if m = st.zmagic(b); m != 0 {
			if st.err != nil {
				return -1
			}
			return m
		}
	}

	// The detectors of the formats that the rules cannot describe. Each
	// one that answers ends the search, unless the flags ask for every
	// result.
	for _, d := range []struct {
		off Flags
		try func() int
	}{
		{NoCheckTar, func() int { return st.tar(b.data) }},
		{NoCheckJSON, func() int { return st.json(b.data) }},
		{NoCheckCSV, func() int { return st.csv(b.data, looksText, code) }},
		{NoCheckSIMH, func() int { return st.simh(b.data) }},
		{NoCheckCDF, func() int { return st.cdf(b) }},
	} {
		if st.flags&d.off != 0 {
			continue
		}
		if m = d.try(); m != 0 {
			if st.checkDone(&rv) {
				return st.done(m, rv, codeMime)
			}
		}
	}

	// The ELF reader runs only on a file. Its text goes after the result of
	// the rules, and its walk of the program headers sets the mode that the
	// rules read.
	var rbuf []byte
	if st.flags&NoCheckELF == 0 && nb > 5 && b.file != nil && b.file.r != nil {
		pb := st.push()
		rv = st.tryELF(b)
		var ok bool
		if rbuf, ok = st.pop(pb); !ok || rv == -1 {
			rbuf = nil
		}
	}

	if st.flags&NoCheckSoft == 0 {
		m = st.softmagic(b, rules.FlagBinTest, looksText)
		if m == 1 && rbuf != nil {
			if st.printf("%s", rbuf) == -1 {
				return st.done(m, rv, codeMime)
			}
		}
		if m != 0 {
			if st.checkDone(&rv) {
				return st.done(m, rv, codeMime)
			}
		}
	}

	if st.flags&NoCheckText == 0 {
		m = st.ascmagic(b, looksText)
		if m != 0 {
			return st.done(m, rv, codeMime)
		}
	}
	return st.simple(m, nb, def, codeMime)
}

// simple is the end of file_buffer when no detector answered.
func (st *state) simple(m, nb int, def, codeMime string) int {
	rv := 0
	if m == 0 {
		m = 1
		rv = st.fileDefault(nb)
		if rv == 0 {
			st.printf("%s", def)
		}
	}
	return st.done(m, rv, codeMime)
}

// done trims the last separator and adds the encoding.
func (st *state) done(m, rv int, codeMime string) int {
	if len(st.out) >= len(separator) && string(st.out[len(st.out)-len(separator):]) == separator {
		st.out = st.out[:len(st.out)-len(separator)]
	}
	if st.flags&MimeEncoding != 0 {
		if st.flags&MimeType != 0 {
			st.printf("; charset=")
		}
		st.printf("%s", codeMime)
	}
	if st.err != nil {
		return -1
	}
	if rv != 0 {
		return rv
	}
	return m
}

// checkDone is checkdone of funcs.c: a result ends the search, unless the
// flags ask for every result.
func (st *state) checkDone(_ *int) bool {
	if st.flags&Continue == 0 {
		return true
	}
	st.printf(separator)
	return false
}

// fileDefault is file_default: what a MIME, Apple or extension output says
// when nothing matched.
func (st *state) fileDefault(nb int) int {
	switch {
	case st.flags&Mime != 0:
		if st.flags&MimeType != 0 {
			if nb != 0 {
				st.printf("application/octet-stream")
			} else {
				st.printf("application/x-empty")
			}
		}
		return 1
	case st.flags&Apple != 0:
		// This is not a typo: Type: UNKN Creator: UNKN.
		st.printf("UNKNUNKN")
		return 1
	case st.flags&Extension != 0:
		st.printf("???")
		return 1
	}
	return 0
}

// softmagic is file_softmagic: it tries the rules of set 0 on b.
func (st *state) softmagic(b *buffer, mode rules.Flag, text bool) int {
	var ic, nc uint16
	mv := matchVars{firstline: true}
	rv := 0
	t := st.e.sets[0]
	ret := st.match(t, 0, len(t.entries), b, 0, mode, text, false, &ic, &nc, &mv, nil, nil)
	switch ret {
	case -1:
		return -1
	case 0:
	default:
		rv = ret
	}
	return rv
}
