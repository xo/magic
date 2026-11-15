package rules

import (
	"math"
	"strconv"
)

// These functions copy the functions of the C library that apprentice.c uses
// to read a rule, in the C locale. A rule means what libmagic reads in it,
// so their quirks matter: base 0, a sign before an unsigned number, and how
// far each reads.

// at returns the byte at i of s, or 0 past its end, as a C string has a NUL
// there.
func at(s []byte, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return 0
}

func isSpace(c byte) bool { return c == ' ' || c >= '\t' && c <= '\r' }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isAlpha(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

func isAlnum(c byte) bool { return isDigit(c) || isAlpha(c) }

func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }

func isPrint(c byte) bool { return c >= 0x20 && c <= 0x7e }

func lower(c byte) byte {
	if isUpper(c) {
		return c + 'a' - 'A'
	}
	return c
}

// digit returns the value of c as a digit of base, or -1.
func digit(c byte, base int) int {
	var v int
	switch {
	case isDigit(c):
		v = int(c - '0')
	case c >= 'a' && c <= 'z':
		v = int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		v = int(c-'A') + 10
	default:
		return -1
	}
	if v >= base {
		return -1
	}
	return v
}

// scanned is a number as strtoull and strtol read it, before either
// applies its own sign rule.
type scanned struct {
	mag      uint64
	neg      bool
	overflow bool
	end      int // where the number ends, or the start when there is none
}

// scanInt reads an integer from s at i with base 0, as the strto* family
// does: spaces, a sign, then 0x for hexadecimal, 0 for octal, or decimal.
func scanInt(s []byte, i int) scanned {
	start := i
	for isSpace(at(s, i)) {
		i++
	}
	var r scanned
	switch at(s, i) {
	case '-':
		r.neg = true
		i++
	case '+':
		i++
	}
	base := 10
	if at(s, i) == '0' {
		if lower(at(s, i+1)) == 'x' && digit(at(s, i+2), 16) >= 0 {
			base = 16
			i += 2
		} else {
			base = 8
		}
	}
	digits := 0
	for {
		d := digit(at(s, i), base)
		if d < 0 {
			break
		}
		if r.mag > (math.MaxUint64-uint64(d))/uint64(base) {
			r.overflow = true
		} else {
			r.mag = r.mag*uint64(base) + uint64(d)
		}
		digits++
		i++
	}
	if digits == 0 {
		r.end = start
		return r
	}
	r.end = i
	return r
}

// strtoull is strtoull(s+i, &end, 0). ok is false where C sets ERANGE.
func strtoull(s []byte, i int) (v uint64, end int, ok bool) {
	r := scanInt(s, i)
	if r.overflow {
		return math.MaxUint64, r.end, false
	}
	if r.neg {
		return -r.mag, r.end, true
	}
	return r.mag, r.end, true
}

// strtol is strtol(s+i, &end, 0) with a 64-bit long. ok is false where C sets
// ERANGE.
func strtol(s []byte, i int) (v int64, end int, ok bool) {
	r := scanInt(s, i)
	switch {
	case r.neg && (r.overflow || r.mag > 1<<63):
		return math.MinInt64, r.end, false
	case !r.neg && (r.overflow || r.mag > math.MaxInt64):
		return math.MaxInt64, r.end, false
	case r.neg:
		return -int64(r.mag), r.end, true
	}
	return int64(r.mag), r.end, true
}

// strtod is strtod(s+i, &end), or strtof when bits is 32. It reads a decimal
// or hexadecimal number, inf, infinity or nan. ok is false where C sets
// ERANGE.
func strtod(s []byte, i int, bits int) (v float64, end int, ok bool) {
	start := i
	for isSpace(at(s, i)) {
		i++
	}
	j := i
	if c := at(s, j); c == '+' || c == '-' {
		j++
	}
	word := func(w string) bool {
		for k := range len(w) {
			if lower(at(s, j+k)) != w[k] {
				return false
			}
		}
		return true
	}
	switch {
	case word("infinity"):
		j += len("infinity")
		return parseFloat(s[i:j], bits, j)
	case word("inf"):
		j += len("inf")
		return parseFloat(s[i:j], bits, j)
	case word("nan"):
		j += len("nan")
		// nan(chars) is part of the number.
		if at(s, j) == '(' {
			k := j + 1
			for isAlnum(at(s, k)) || at(s, k) == '_' {
				k++
			}
			if at(s, k) == ')' {
				j = k + 1
			}
		}
		return math.NaN(), j, true
	}
	hex := at(s, j) == '0' && lower(at(s, j+1)) == 'x'
	base := 10
	k := j
	if hex {
		base = 16
		k += 2
	}
	mantissa := 0
	for digit(at(s, k), base) >= 0 {
		k++
		mantissa++
	}
	if at(s, k) == '.' {
		k++
		for digit(at(s, k), base) >= 0 {
			k++
			mantissa++
		}
	}
	if mantissa == 0 {
		if hex {
			// "0x" with no hex digit is the number 0, and ends after the 0.
			return 0, j + 1, true
		}
		return 0, start, true
	}
	exp := byte('e')
	if hex {
		exp = 'p'
	}
	if lower(at(s, k)) == exp {
		e := k + 1
		if c := at(s, e); c == '+' || c == '-' {
			e++
		}
		if isDigit(at(s, e)) {
			for isDigit(at(s, e)) {
				e++
			}
			k = e
		}
	}
	text := s[i:k]
	if hex && lower(at(s, k-1)) != 'p' && !hasExponent(text) {
		// Go wants an exponent on a hexadecimal float, and C does not.
		text = append(append([]byte(nil), text...), 'p', '0')
	}
	return parseFloat(text, bits, k)
}

func hasExponent(b []byte) bool {
	for _, c := range b {
		if lower(c) == 'p' {
			return true
		}
	}
	return false
}

func parseFloat(text []byte, bits, end int) (float64, int, bool) {
	v, err := strconv.ParseFloat(string(text), bits)
	if err != nil {
		// A value out of range is ±Inf, as in C, with ERANGE.
		return v, end, false
	}
	return v, end, true
}
