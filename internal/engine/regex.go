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
// This file translates src/funcs.c, src/softmagic.c of file 5.48 to Go (D29).

package engine

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// A regex is a rule's regular expression, compiled for Go.
//
// libmagic compiles each one with regcomp of the C library, with
// REG_EXTENDED and REG_NEWLINE, in the C locale. There a character is one
// byte. Go's regexp reads UTF-8, where a byte above 0x7f starts a character
// of several bytes. So the port reads each byte as the character with the
// same number, 0 to 255, in both the pattern and the text.
//
// The translation also keeps three rules of POSIX that Go does not follow:
// a backslash in a bracket expression is a literal, a negated bracket
// expression does not match a newline under REG_NEWLINE, and case folding
// in the C locale folds only the ASCII letters.
type regex struct {
	re *regexp.Regexp
}

// errRegex is the error of a pattern that libmagic does not compile.
var errRegex = errors.New("bad regex")

// compileRegex compiles pat, the C string of a rule, as file_regcomp does.
func compileRegex(pat []byte, icase bool) (*regex, error) {
	if err := checkRegex(pat); err != nil {
		return nil, err
	}
	s, err := translate(pat, icase)
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(s)
	if err != nil {
		return nil, fmt.Errorf("compiling %q as %q: %w", pat, s, err)
	}
	// POSIX takes the longest of the matches that start first.
	re.Longest()
	return &regex{re: re}, nil
}

// checkRegex is check_regex of funcs.c. libmagic refuses a pattern with two
// repetition operators in a row, with a bound above 1000, or with a byte
// that is not printable.
func checkRegex(pat []byte) error {
	const wild = "?*+{"
	// oc starts as NUL, and strchr finds the NUL that ends wild. So a
	// pattern that starts with a repetition operator is refused too.
	isWild := func(c byte) bool { return c == 0 || strings.IndexByte(wild, c) >= 0 }
	var oc byte
	for p := 0; p < len(pat); p++ {
		c := pat[p]
		if isWild(oc) && isWild(c) {
			return errRegex
		}
		if c == '{' {
			if oc == '}' {
				return errRegex
			}
			l, ep := strtoul10(pat, p+1)
			if ep != p+1 && l > 1000 {
				return errRegex
			}
			if ep < len(pat) && pat[ep] == ',' {
				l, eep := strtoul10(pat, ep+1)
				if eep != ep+1 && l > 1000 {
					return errRegex
				}
			}
		}
		oc = c
		if isPrint(c) || isSpace(c) || c == '\b' || c == 0x8a {
			continue
		}
		return errRegex
	}
	return nil
}

// strtoul10 reads a decimal number at s[i:] as strtoul with base 10, and
// returns it and the index after it, or i when there is no number. It
// saturates.
func strtoul10(s []byte, i int) (uint64, int) {
	j := i
	for j < len(s) && isSpace(s[j]) {
		j++
	}
	if j < len(s) && (s[j] == '+' || s[j] == '-') {
		j++
	}
	start := j
	var n uint64
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		if n > (1<<64-1)/10 {
			n = 1<<64 - 1
		} else {
			n = n*10 + uint64(s[j]-'0')
		}
		j++
	}
	if j == start {
		return 0, i
	}
	return n, j
}

// translate writes the POSIX extended regular expression pat, as glibc reads
// it in the C locale, in the syntax of Go's regexp.
func translate(pat []byte, icase bool) (string, error) {
	var sb strings.Builder
	// ^ and $ match at each line under REG_NEWLINE.
	sb.WriteString("(?m)")
	for i := 0; i < len(pat); i++ {
		c := pat[i]
		switch c {
		case '\\':
			i++
			if i == len(pat) {
				return "", errRegex
			}
			c = pat[i]
			switch c {
			case 'w':
				sb.WriteString(`[0-9A-Za-z_]`)
			case 'W':
				sb.WriteString(`[^0-9A-Za-z_]`)
			case 's':
				sb.WriteString(`[\t\n\v\f\r ]`)
			case 'S':
				sb.WriteString(`[^\t\n\v\f\r ]`)
			case 'b', '<', '>':
				sb.WriteString(`\b`)
			case 'B':
				sb.WriteString(`\B`)
			case '`':
				sb.WriteString(`\A`)
			case '\'':
				sb.WriteString(`\z`)
			case '1', '2', '3', '4', '5', '6', '7', '8', '9':
				// Go has no backreferences, and no rule uses one.
				return "", errRegex
			default:
				literal(&sb, c, icase)
			}
		case '[':
			set, next, err := bracket(pat, i+1)
			if err != nil {
				return "", err
			}
			if icase {
				set.foldASCII()
			}
			set.write(&sb)
			i = next - 1
		case '.':
			sb.WriteString(`[^\n]`)
		case '^', '$', '(', ')', '|', '*', '+', '?':
			sb.WriteByte(c)
		case '{':
			if n := interval(pat, i); n > 0 {
				iv := string(pat[i : i+n])
				if strings.HasPrefix(iv, "{,") {
					// glibc reads {,n} as {0,n}.
					iv = "{0" + iv[1:]
				}
				sb.WriteString(iv)
				i += n - 1
				break
			}
			literal(&sb, c, icase)
		default:
			literal(&sb, c, icase)
		}
	}
	return sb.String(), nil
}

// interval returns the length of the interval, such as {2} or {1,3}, at
// pat[i], or 0 if there is none there.
func interval(pat []byte, i int) int {
	j := i + 1
	digits := 0
	for j < len(pat) && pat[j] >= '0' && pat[j] <= '9' {
		j++
		digits++
	}
	if j < len(pat) && pat[j] == ',' {
		j++
		for j < len(pat) && pat[j] >= '0' && pat[j] <= '9' {
			j++
			digits++
		}
	}
	if digits == 0 || j >= len(pat) || pat[j] != '}' {
		return 0
	}
	return j - i + 1
}

// literal writes the byte c as a literal.
func literal(sb *strings.Builder, c byte, icase bool) {
	if icase && isAlpha(c) {
		var set byteSet
		set.add(c)
		set.foldASCII()
		set.write(sb)
		return
	}
	if c < utf8.RuneSelf {
		sb.WriteString(regexp.QuoteMeta(string(rune(c))))
		return
	}
	fmt.Fprintf(sb, `\x{%x}`, c)
}

// byteSet is a set of bytes, from a bracket expression.
type byteSet [4]uint64

func (s *byteSet) add(c byte)      { s[c>>6] |= 1 << (c & 63) }
func (s *byteSet) has(c byte) bool { return s[c>>6]&(1<<(c&63)) != 0 }
func (s *byteSet) addRange(lo, hi byte) {
	for c := int(lo); c <= int(hi); c++ {
		s.add(byte(c))
	}
}

// foldASCII adds the other case of each ASCII letter in s, as REG_ICASE does
// in the C locale.
func (s *byteSet) foldASCII() {
	for c := byte('A'); c <= 'Z'; c++ {
		if s.has(c) || s.has(c+32) {
			s.add(c)
			s.add(c + 32)
		}
	}
}

// write writes s as a Go character class.
func (s *byteSet) write(sb *strings.Builder) {
	sb.WriteByte('[')
	empty := true
	for c := 0; c < 256; c++ {
		if !s.has(byte(c)) {
			continue
		}
		hi := c
		for hi+1 < 256 && s.has(byte(hi+1)) {
			hi++
		}
		fmt.Fprintf(sb, `\x{%x}`, c)
		if hi > c {
			fmt.Fprintf(sb, `-\x{%x}`, hi)
		}
		empty = false
		c = hi
	}
	if empty {
		// A set with nothing in it matches nothing.
		sb.WriteString(`^\x00-\x{10ffff}`)
	}
	sb.WriteByte(']')
}

// classes are the character classes of the C locale.
var classes = map[string]func(c byte) bool{
	"alpha":  isAlpha,
	"digit":  func(c byte) bool { return c >= '0' && c <= '9' },
	"alnum":  isAlnum,
	"upper":  func(c byte) bool { return c >= 'A' && c <= 'Z' },
	"lower":  func(c byte) bool { return c >= 'a' && c <= 'z' },
	"space":  isSpace,
	"blank":  func(c byte) bool { return c == ' ' || c == '\t' },
	"punct":  func(c byte) bool { return isPrint(c) && c != ' ' && !isAlnum(c) },
	"print":  isPrint,
	"graph":  func(c byte) bool { return isPrint(c) && c != ' ' },
	"cntrl":  func(c byte) bool { return c < 0x20 || c == 0x7f },
	"xdigit": isXDigit,
}

// bracket reads the bracket expression that starts at pat[i], after its
// '['. It returns the set and the index after the closing ']'.
func bracket(pat []byte, i int) (byteSet, int, error) {
	var set byteSet
	negate := false
	if i < len(pat) && pat[i] == '^' {
		negate = true
		i++
	}
	first := true
	for {
		if i >= len(pat) {
			return set, 0, errRegex
		}
		c := pat[i]
		if c == ']' && !first {
			i++
			break
		}
		first = false
		if c == '[' && i+1 < len(pat) && (pat[i+1] == ':' || pat[i+1] == '=' || pat[i+1] == '.') {
			kind := pat[i+1]
			end := strings.Index(string(pat[i+2:]), string([]byte{kind, ']'}))
			if end < 0 {
				return set, 0, errRegex
			}
			name := string(pat[i+2 : i+2+end])
			i += 2 + end + 2
			switch kind {
			case ':':
				f, ok := classes[name]
				if !ok {
					return set, 0, errRegex
				}
				for b := 0; b < 256; b++ {
					if f(byte(b)) {
						set.add(byte(b))
					}
				}
				continue
			default:
				// An equivalence class or a collating symbol of one
				// character is that character in the C locale.
				if len(name) != 1 {
					return set, 0, errRegex
				}
				c = name[0]
			}
		} else {
			i++
		}
		// c starts a range if a '-' and an end that is not ']' follow.
		if i+1 < len(pat) && pat[i] == '-' && pat[i+1] != ']' {
			hi := pat[i+1]
			i += 2
			if hi == '[' && i < len(pat) && pat[i] == '.' {
				end := strings.Index(string(pat[i+1:]), ".]")
				if end != 1 {
					return set, 0, errRegex
				}
				hi = pat[i+1]
				i += 3
			}
			if hi < c {
				return set, 0, errRegex
			}
			set.addRange(c, hi)
			continue
		}
		set.add(c)
	}
	if negate {
		for k := range set {
			set[k] = ^set[k]
		}
		// Under REG_NEWLINE a negated list does not match a newline.
		set[0] &^= 1 << '\n'
	}
	return set, i, nil
}

// find runs re on text as file_regexec does, and returns the start and the
// end of the first match.
func (r *regex) find(text []byte) (int, int, bool) {
	ascii := true
	for _, c := range text {
		if c >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		loc := r.re.FindIndex(text)
		if loc == nil {
			return 0, 0, false
		}
		return loc[0], loc[1], true
	}
	// Read each byte as the character with the same number, then map the
	// offsets of the match back to bytes.
	u := make([]byte, 0, 2*len(text))
	for _, c := range text {
		u = utf8.AppendRune(u, rune(c))
	}
	loc := r.re.FindIndex(u)
	if loc == nil {
		return 0, 0, false
	}
	return utf8.RuneCount(u[:loc[0]]), utf8.RuneCount(u[:loc[1]]), true
}
