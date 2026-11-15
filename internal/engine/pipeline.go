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

	enc := encoding(b.data)
	looksText := enc.text
	codeMime = enc.codeMime

	// Compressed data. Its result skips the separator and the encoding at
	// the end, as the goto done_encoding of libmagic does.
	if m = st.zmagic(b); m != 0 {
		if st.err != nil {
			return -1
		}
		return m
	}

	// The detectors of the formats that the rules cannot describe. Each
	// one that answers ends the search, unless the flags ask for every
	// result.
	for _, try := range []func() int{
		func() int { return st.tar(b.data) },
		func() int { return st.json(b.data) },
		func() int { return st.csv(b.data, looksText, enc.code) },
		func() int { return st.simh(b.data) },
		func() int { return st.cdf(b) },
	} {
		if m = try(); m != 0 {
			if st.checkDone(&rv) {
				return st.done(m, rv, codeMime)
			}
		}
	}

	// The ELF reader runs only on a file. Its text goes after the result of
	// the rules, and its walk of the program headers sets the mode that the
	// rules read.
	var rbuf []byte
	if nb > 5 && b.file != nil && b.file.r != nil {
		pb := st.push()
		rv = st.tryELF(b)
		var ok bool
		if rbuf, ok = st.pop(pb); !ok || rv == -1 {
			rbuf = nil
		}
	}

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

	m = st.ascmagic(b, looksText)
	if m != 0 {
		return st.done(m, rv, codeMime)
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
func (st *state) checkDone(rv *int) bool {
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
