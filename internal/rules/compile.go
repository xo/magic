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
// This file translates src/apprentice.c, src/encoding.c of file 5.48 to Go (D29).

package rules

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
)

// File is one file of rules, with the name that libmagic gives it.
type File struct {
	// Name is the name that libmagic tucks after an empty description, such
	// as "magic/elf".
	Name string
	Data []byte
}

// Database is the compiled rules: the rules in the order that libmagic tries
// them. Sets[0] holds the rules, and Sets[1] the rules whose line at level 0
// is a name, which use calls as subroutines.
type Database struct {
	Sets [2][]Group
}

// Compile compiles files, in the order given, as apprentice_load of libmagic
// compiles a folder: it reads each file, marks each rule as one for binary
// data or for text, and sorts the rules of each set by their strength. The
// caller sorts the files by name, as libmagic does for a folder.
func Compile(files []File) (*Database, error) {
	var db Database
	var errs []error
	for _, f := range files {
		groups, err := ParseFile(f.Name, f.Data)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, g := range groups {
			i := 0
			if g[0].Type == Name {
				i = 1
			}
			db.Sets[i] = append(db.Sets[i], g)
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	for i := range db.Sets {
		for _, g := range db.Sets[i] {
			// set_text_binary of apprentice.c means to pass each line of
			// a rule, but its loop walks the array of rules and stops at the
			// next line at level 0, which is the next rule. So only the line
			// at level 0 counts.
			setTestType(&g[0], &g[0])
		}
		sortGroups(db.Sets[i])
	}
	return &db, nil
}

// sortGroups is the qsort of apprentice_load with apprentice_sort: by
// strength, higher first, then by the bytes of the line at level 0 with its
// line number cleared, higher first. The glibc qsort keeps the order of
// equal rules, and so does a stable sort.
func sortGroups(gs []Group) {
	type key struct {
		strength int
		bytes    [EntrySize]byte
	}
	keys := make(map[*Entry]key, len(gs))
	for _, g := range gs {
		e := g[0]
		e.Lineno = 0
		keys[&g[0]] = key{strength(&g[0]), e.Encode()}
	}
	slices.SortStableFunc(gs, func(a, b Group) int {
		ka, kb := keys[&a[0]], keys[&b[0]]
		if ka.strength != kb.strength {
			if ka.strength > kb.strength {
				return -1
			}
			return 1
		}
		return -bytes.Compare(ka.bytes[:], kb.bytes[:])
	})
}

// strength is file_magic_strength of apprentice.c: how strongly a rule
// matches, from its line at level 0.
func strength(e *Entry) int {
	const mult = 10
	val := 2 * mult
	switch e.Type {
	case Default:
		return 0
	case PString, String, Octal:
		val += int(e.Vallen) * mult
	case BEString16, LEString16:
		val += int(e.Vallen) * mult / 2
	case Search:
		if e.Vallen != 0 {
			val += int(e.Vallen) * max(mult/int(e.Vallen), 1)
		}
	case Regex:
		v := nonMagic(e.Value[:])
		val += v * max(mult/v, 1)
	case Indirect, Name, Use, Clear:
	case DER:
		val += mult
	default:
		val += e.Type.size() * mult
	}
	switch e.Reln {
	case 'x', '!':
		val = 0
	case '=':
		val += mult
	case '>', '<':
		val -= 2 * mult
	case '^', '&':
		val -= mult
	}
	switch e.FactorOp {
	case '+':
		val += int(e.Factor)
	case '-':
		val -= int(e.Factor)
	case '*':
		val *= int(e.Factor)
	case '/':
		val /= int(e.Factor)
	}
	if val <= 0 {
		val = 1
	}
	if e.Desc[0] == 0 {
		// A rule with no description depends on its continuations to print
		// something, and gets a bonus.
		val++
	}
	return val
}

// nonMagic is nonmagic of apprentice.c: how many characters of a regular
// expression match only themselves. It reads the C string at the start of v.
func nonMagic(v []byte) int {
	if i := bytes.IndexByte(v, 0); i >= 0 {
		v = v[:i]
	}
	rv := 0
	for p := 0; p < len(v); p++ {
		switch v[p] {
		case '\\':
			// An escaped character counts 1.
			if p+1 < len(v) {
				p++
			}
			rv++
		case '?', '*', '.', '+', '^', '$':
		case '[':
			// A bracket expression counts the ']' that ends it, which the
			// loop reads next.
			for p < len(v) && v[p] != ']' {
				p++
			}
			p--
		case '{':
			// A brace expression counts 0.
			for p < len(v) && v[p] != '}' {
				p++
			}
			if p == len(v) {
				p--
			}
		default:
			rv++
		}
	}
	if rv == 0 {
		return 1
	}
	return rv
}

// setTestType is set_test_type of apprentice.c. It marks the line at level 0,
// start, as a rule for binary data or for text, from each of its lines, e.
// For a string, it reads the string flags of start, not of e, as libmagic
// does.
func setTestType(start, e *Entry) {
	switch e.Type {
	case String, PString, BEString16, LEString16, Regex, Search:
		f := start.StrFlags()
		if f&StrBinTest != 0 {
			start.Flag |= FlagBinTest
		}
		if f&StrTextTest != 0 {
			start.Flag |= FlagTextTest
		}
		if start.Flag&(FlagTextTest|FlagBinTest) != 0 {
			return
		}
		if e.Type != Regex && e.Type != Search {
			start.Flag |= FlagBinTest
			return
		}
		if looksUTF8(e.Value[:e.Vallen]) <= 0 {
			start.Flag |= FlagBinTest
		} else {
			start.Flag |= FlagTextTest
		}
	case Default, Invalid, Indirect, Name, Use, Clear:
	default:
		start.Flag |= FlagBinTest
	}
}

// textChars marks the bytes that appear in plain ASCII text, as T in
// text_chars of encoding.c.
var textChars = func() [256]bool {
	var t [256]bool
	for _, c := range []byte{7, 8, 9, 10, 11, 12, 13, 26, 27, 0x85} {
		t[c] = true
	}
	for c := 0x20; c < 0x7f; c++ {
		t[c] = true
	}
	return t
}()

// looksUTF8 is file_looks_utf8 of encoding.c: -1 for bytes that are not
// UTF-8, 0 for UTF-8 with control characters, 1 for ASCII and 2 for UTF-8 with
// a character above ASCII. A sequence cut by the end of b counts as good, as
// in libmagic.
func looksUTF8(b []byte) int {
	gotone, ctrl := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case c&0x80 == 0:
			if !textChars[c] {
				ctrl = true
			}
			continue
		case c&0x40 == 0:
			return -1
		}
		var lo, hi byte = 0x80, 0xbf
		following := 0
		switch {
		case c < 0xc2:
			return -1
		case c < 0xe0:
			following = 1
		case c < 0xf0:
			following = 2
			switch c {
			case 0xe0:
				lo = 0xa0
			case 0xed:
				hi = 0x9f
			}
		case c < 0xf5:
			following = 3
			switch c {
			case 0xf0:
				lo = 0x90
			case 0xf4:
				hi = 0x8f
			}
		default:
			return -1
		}
		for n := range following {
			i++
			if i >= len(b) {
				return result(ctrl, gotone)
			}
			if n == 0 && (b[i] < lo || b[i] > hi) {
				return -1
			}
			if b[i]&0x80 == 0 || b[i]&0x40 != 0 {
				return -1
			}
		}
		gotone = true
	}
	return result(ctrl, gotone)
}

func result(ctrl, gotone bool) int {
	switch {
	case ctrl:
		return 0
	case gotone:
		return 2
	}
	return 1
}

// Encode writes db in the format of magic.mgc of file 5.48 on a little-endian
// machine: a header the size of one entry, then the entries of each set.
func (db *Database) Encode() []byte {
	n := [2]int{}
	for i, set := range db.Sets {
		for _, g := range set {
			n[i] += len(g)
		}
	}
	out := make([]byte, EntrySize, EntrySize*(1+n[0]+n[1]))
	binary.LittleEndian.PutUint32(out[0:], 0xf11e041c)
	binary.LittleEndian.PutUint32(out[4:], 21)
	binary.LittleEndian.PutUint32(out[8:], uint32(n[0]))
	binary.LittleEndian.PutUint32(out[12:], uint32(n[1]))
	for _, set := range db.Sets {
		for _, g := range set {
			for i := range g {
				b := g[i].Encode()
				out = append(out, b[:]...)
			}
		}
	}
	return out
}
