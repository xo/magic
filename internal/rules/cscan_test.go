package rules

import (
	"math"
	"testing"
)

// The scanners copy the strto* functions of the C library. The cases are the
// values that a C library gives, so each one is a fact about C.

func TestStrtoull(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		in   string
		want uint64
		end  int
		ok   bool
	}{
		{"0", 0, 1, true},
		{"10 x", 10, 2, true},
		{"0x1f,", 31, 4, true},
		{"0X1F", 31, 4, true},
		{"017", 15, 3, true},
		{"08", 0, 1, true}, // octal stops at 8
		{"0x", 0, 1, true}, // "0x" with no hex digit is 0
		{"-1", math.MaxUint64, 2, true},
		{"  +5", 5, 4, true},
		{"x", 0, 0, true}, // no number
		{"18446744073709551616", math.MaxUint64, 20, false},
	} {
		got, end, ok := strtoull([]byte(test.in), 0)
		if got != test.want || end != test.end || ok != test.ok {
			t.Errorf("%q: expected (%d, %d, %v), got: (%d, %d, %v)", test.in, test.want, test.end, test.ok, got, end, ok)
		}
	}
}

func TestStrtol(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		in   string
		want int64
		end  int
		ok   bool
	}{
		{"-12", -12, 3, true},
		{"0x7fffffffffffffff", math.MaxInt64, 18, true},
		{"0x8000000000000000", math.MaxInt64, 18, false},
		{"-0x8000000000000000", math.MinInt64, 19, true},
		{"-0x8000000000000001", math.MinInt64, 19, false},
	} {
		got, end, ok := strtol([]byte(test.in), 0)
		if got != test.want || end != test.end || ok != test.ok {
			t.Errorf("%q: expected (%d, %d, %v), got: (%d, %d, %v)", test.in, test.want, test.end, test.ok, got, end, ok)
		}
	}
}

func TestStrtod(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		in   string
		bits int
		want float64
		end  int
	}{
		{"1.5 x", 64, 1.5, 3},
		{"-2e3", 64, -2000, 4},
		{"1e", 64, 1, 1}, // an exponent with no digit is not read
		{"0x10", 64, 16, 4},
		{"0x1p4", 64, 16, 5},
		{"inf", 64, math.Inf(1), 3},
		{"-Infinity", 64, math.Inf(-1), 9},
		{"0.1", 32, float64(float32(0.1)), 3},
		{"abc", 64, 0, 0},
	} {
		got, end, _ := strtod([]byte(test.in), 0, test.bits)
		if got != test.want || end != test.end {
			t.Errorf("%q: expected (%v, %d), got: (%v, %d)", test.in, test.want, test.end, got, end)
		}
	}
}

func TestLooksUTF8(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		in   string
		want int
	}{
		{"plain text", 1},
		{"caf\xc3\xa9", 2},
		{"\x01", 0},      // a control character
		{"\x80", -1},     // a continuation byte first
		{"\xc0\x80", -1}, // an overlong form
		{"\xe0\x80\x80", -1},
		{"\xed\xa0\x80", -1}, // a surrogate
		{"\xc3", 1},          // cut by the end: good, and not counted
		{"\xf4\x90\x80\x80", -1},
	} {
		if got := looksUTF8([]byte(test.in)); got != test.want {
			t.Errorf("%q: expected %d, got: %d", test.in, test.want, got)
		}
	}
}

func TestNonMagic(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		in   string
		want int
	}{
		{"abc", 3},
		{"a.*b", 2},
		{"\\.x", 2},
		{"[abc]d", 2}, // the bracket counts 1, by its ']'
		{"a{2,3}", 1},
		{"^$", 1}, // at least 1
	} {
		if got := nonMagic([]byte(test.in)); got != test.want {
			t.Errorf("%q: expected %d, got: %d", test.in, test.want, got)
		}
	}
}
