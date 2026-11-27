package engine

import (
	"math/rand/v2"
	"regexp"
	"testing"

	"github.com/xo/magic/internal/rules"
)

// The expected values in this file are what glibc and libmagic 5.48 give
// in the C locale (D33). The differential test in the test module compares
// the whole engine with the oracle (D30).

func TestRegexMatchesGlibc(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		pat, text string
		icase     bool
		so, eo    int
		ok        bool
	}{
		{"a.c", "xabcx", false, 1, 4, true},
		{"a.c", "a\nc", false, 0, 0, false}, // . does not match a newline
		{"[^x]+", "ab\ncd", false, 0, 2, true},
		{"^cd", "ab\ncd", false, 3, 5, true},
		{"ab$", "ab\ncd", false, 0, 2, true},
		{"[\\.]+", "a\\.b", false, 1, 3, true}, // a backslash in brackets is a literal
		{"[]a]+", "x]a]y", false, 1, 4, true},
		{"[a-]+", "x-a-y", false, 1, 4, true},
		{"HeLLo", "say hello", true, 4, 9, true},
		{"[[:space:]]+", "a \t b", false, 1, 4, true},
		{"(a|ab)(c|bcd)", "abcd", false, 0, 4, true}, // the longest match
		{"x{2,3}", "xxxxx", false, 0, 3, true},
		{"\xe9", "caf\xe9", false, 3, 4, true}, // one byte is one character
		{"caf.", "caf\xe9!", false, 0, 4, true},
		{"\xe9", "CAF\xc9", true, 0, 0, false}, // only ASCII folds
		{"\\(a\\)", "(a)", false, 0, 3, true},
	} {
		s, err := translate([]byte(test.pat), test.icase)
		if err != nil {
			t.Errorf("%q: expected no error, got: %v", test.pat, err)
			continue
		}
		re, err := regexp.Compile(s)
		if err != nil {
			t.Errorf("%q: expected no error, got: %v", test.pat, err)
			continue
		}
		re.Longest()
		so, eo, ok := (&regex{re: re}).find([]byte(test.text))
		if so != test.so || eo != test.eo || ok != test.ok {
			t.Errorf("%q on %q: expected (%d, %d, %v), got: (%d, %d, %v)", test.pat, test.text,
				test.so, test.eo, test.ok, so, eo, ok)
		}
	}
}

func TestCheckRegexRefusesWhatLibmagicRefuses(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		pat string
		ok  bool
	}{
		{"a+b", true},
		{"a+*", false},     // two repetition operators
		{"*a", false},      // a repetition operator first
		{"a{2}{3}", false}, // cascading intervals
		{"a{1001}", false}, // a bound above 1000
		{"a{1000}", true},
		{"a\x01", false}, // not printable
		{"a\x8a", true},
	} {
		if err := checkRegex([]byte(test.pat)); (err == nil) != test.ok {
			t.Errorf("%q: expected ok %v, got: %v", test.pat, test.ok, err)
		}
	}
}

func TestStrncmp(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		a, b  string
		flags uint32
		want  uint64
	}{
		{"abc", "abc", 0, 0},
		{"abc", "abd", 0, 1},
		{"abd", "abc", 0, 1<<64 - 1}, // the difference of the bytes, as unsigned
		{"abc", "ABC", rules.StrIgnoreLowercase, 0},
		{"ABC", "abc", rules.StrIgnoreLowercase, 32}, // only a lowercase letter of the rule folds
		{"a b", "a    b", rules.StrCompactWhitespace, 0},
		{"a b", "ab", rules.StrCompactWhitespace, 1},
		{"a b", "ab", rules.StrCompactOptionalWhitespace, 0},
		{"abc", "abcd", rules.StrFullWord, 1},
		{"abc", "abc d", rules.StrFullWord, 0},
	} {
		got := strncmp([]byte(test.a), []byte(test.b), len(test.a), len(test.b), test.flags)
		if got != test.want {
			t.Errorf("%q, %q, %#x: expected %d, got: %d", test.a, test.b, test.flags, test.want, got)
		}
	}
	// The byte after a full word is past maxlen, so strncmp does not read it.
	if got := strncmp([]byte("abc"), []byte("abcd"), 3, 3, rules.StrFullWord); got != 0 {
		t.Errorf("expected 0, got: %d", got)
	}
}

func TestFmtDateTime(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		v    int64
		want string
	}{
		{0, "Thu Jan  1 00:00:00 1970"},
		{253402300799, "Fri Dec 31 23:59:59 9999"},
		{-100000000000, "*Invalid datetime*"}, // the year -1199 does not fit in asctime_r
	} {
		if got := fmtDateTime(uint64(test.v), 0); got != test.want {
			t.Errorf("expected %q, got: %q", test.want, got)
		}
	}
}

func TestEncoding(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		in       string
		text     bool
		codeMime string
	}{
		{"hello\n", true, "us-ascii"},
		{"caf\xc3\xa9\n", true, "utf-8"},
		{"\xef\xbb\xbfhi", true, "utf-8"},
		{"\xff\xfeh\x00i\x00", true, "utf-16le"},
		{"\xfe\xff\x00h\x00i", true, "utf-16be"},
		{"caf\xe9\n", true, "iso-8859-1"},
		{"\x81\x82abc", true, "unknown-8bit"},
		{"\x00\x01\x02\x03", false, "binary"},
		{"+/v8abc", true, "utf-7"},
	} {
		r := encoding([]byte(test.in))
		if r.text != test.text || r.codeMime != test.codeMime {
			t.Errorf("%q: expected (%v, %q), got: (%v, %q)", test.in, test.text, test.codeMime, r.text, r.codeMime)
		}
	}
}

func TestJSON(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		in   string
		want int
	}{
		{`{"a": [1, 2.5e3, true, null]}`, 1},
		{"{\"a\":1}\n{\"b\":2}\n", 2},
		{`[1, 2]`, 1},
		{`"just a string"`, 0}, // no object or array
		{`{"a": tr`, 0},
		{`{"a": "\u00zz"}`, 0},
		{`[1, 2`, 0},
	} {
		var counts [jsonMax]int
		p := 0
		if got := jsonParse([]byte(test.in), &p, &counts, 0); got != test.want {
			t.Errorf("%q: expected %d, got: %d", test.in, test.want, got)
		}
	}
}

func TestCSV(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		in   string
		want bool
	}{
		{"a,b,c\n1,2,3\n", true},
		{"a,b\n1,2,3\n", false},
		{"\"a,b\",c\n1,2\n", true},
		{"no commas\nat all\n", false},
		{"a,b\n", false}, // one line
	} {
		if got := csvParse([]byte(test.in)); got != test.want {
			t.Errorf("%q: expected %v, got: %v", test.in, test.want, got)
		}
	}
}

func TestTar(t *testing.T) {
	t.Parallel()
	h := make([]byte, 512)
	copy(h, "file.txt")
	copy(h[257:], "ustar\x0000")
	copy(h[148:], "        ")
	sum := 0
	for _, c := range h {
		sum += int(c)
	}
	copy(h[148:], []byte(octal(sum)+"\x00 "))
	if got := isTar(h); got != 2 {
		t.Errorf("expected 2, got: %d", got)
	}
	copy(h[257:], "ustar  \x00")
	if got := isTar(h); got != 0 {
		t.Errorf("the checksum covers the magic: expected 0, got: %d", got)
	}
	if got := isTar(h[:511]); got != 0 {
		t.Errorf("a short header: expected 0, got: %d", got)
	}
}

func octal(n int) string {
	s := ""
	for i := 0; i < 6; i++ {
		s = string(rune('0'+n&7)) + s
		n >>= 3
	}
	return s
}

func TestSIMH(t *testing.T) {
	t.Parallel()
	rec := func(n int) []byte {
		b := []byte{byte(n), 0, 0, 0}
		b = append(b, make([]byte, n+n&1)...)
		return append(b, byte(n), 0, 0, 0)
	}
	tape := append(rec(4), rec(3)...)
	tape = append(tape, 0, 0, 0, 0)
	if !simhParse(tape) {
		t.Errorf("expected a tape, got none")
	}
	if simhParse([]byte{0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Errorf("only tapemarks: expected no tape, got one")
	}
	if simhParse([]byte{4, 0, 0, 0, 1, 2, 3, 4, 5, 0, 0, 0}) {
		t.Errorf("lengths that differ: expected no tape, got one")
	}
}

func TestVarexpand(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		in   string
		mode uint32
		want string
		ok   bool
	}{
		{"application/x-${x?pie-executable:sharedlib}", 0o755, "application/x-pie-executable", true},
		{"application/x-${x?pie-executable:sharedlib}", 0o644, "application/x-sharedlib", true},
		{"plain", 0, "plain", true},
		{"${y?a:b}", 0, "", false},
		{"${x?a}", 0, "", false},
	} {
		st := &state{mode: test.mode}
		got, ok := st.varexpand([]byte(test.in), 1024)
		if string(got) != test.want || ok != test.ok {
			t.Errorf("%q: expected (%q, %v), got: (%q, %v)", test.in, test.want, test.ok, got, ok)
		}
	}
}

func TestPrintable(t *testing.T) {
	t.Parallel()
	if got, want := printable([]byte("a\n- b\x80\x00c")), `a\012- b\200`; got != want {
		t.Errorf("expected %q, got: %q", want, got)
	}
}

func TestBufferOfOneByte(t *testing.T) {
	t.Parallel()
	e := New(&rules.Database{})
	for _, test := range []struct {
		in    string
		flags Flags
		want  string
	}{
		{"", None, "empty"},
		{"", MimeType, "application/x-empty"},
		{"x", None, "very short file (no magic)"},
		{"x", Mime, "application/octet-stream; charset=binary"},
		{"hello world\n", MimeType, "text/plain"},
		{"hello world\n", Mime, "text/plain; charset=us-ascii"},
		{"{\"a\":1}", MimeType, "application/json"},
	} {
		got, err := e.Buffer([]byte(test.in), test.flags)
		if err != nil || got != test.want {
			t.Errorf("%q, %#x: expected %q, got: %q, %v", test.in, test.flags, test.want, got, err)
		}
	}
}

func TestCprintfMatchesGlibc(t *testing.T) {
	t.Parallel()
	neg := func(v int64) carg { return cint(uint64(v)) }
	for _, test := range []struct {
		format string
		args   []carg
		want   string
	}{
		{"[%5d][%-5d][%05d][%+d][% d]", []carg{cint(42), cint(42), cint(42), cint(42), cint(42)}, "[   42][42   ][00042][+42][ 42]"},
		{"[%u][%x][%#x][%#o][%X][%#X]", []carg{neg(-1), cint(255), cint(255), cint(8), cint(255), cint(0)}, "[4294967295][ff][0xff][010][FF][0]"},
		{"[%.3s][%10s][%-10s|]", []carg{cstringS("abcdef"), cstringS("abc"), cstringS("abc")}, "[abc][       abc][abc       |]"},
		{"[%g][%g][%g][%g][%g][%g]", []carg{cfloat(0.0001), cfloat(123456), cfloat(1234567), cfloat(1e-5), cfloat(0.1), cfloat(100)}, "[0.0001][123456][1.23457e+06][1e-05][0.1][100]"},
		{"[%e][%.2f][%#g][%G]", []carg{cfloat(1234.5), cfloat(3.14159), cfloat(1), cfloat(1e20)}, "[1.234500e+03][3.14][1.00000][1E+20]"},
		{"[%c][%hhd][%hd][%lld][%llx]", []carg{cint(65), cint(300), cint(70000), neg(-5), cint(255)}, "[A][44][4464][-5][ff]"},
		{"[%.0d][%#.0o][%.4x]", []carg{cint(0), cint(0), cint(10)}, "[][0][000a]"},
	} {
		if got := cprintfS(test.format, test.args...); got != test.want {
			t.Errorf("%q: expected %q, got: %q", test.format, test.want, got)
		}
	}
}

func TestFmtcheck(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		desc, def string
		ok        bool
	}{
		{"version %d", "%d", true},
		{"version %u", "%d", true},
		{"%c", "%d", true},
		{"%lld", "%d", false},
		{"%s", "%d", false},
		{"%.3s", "%s", true},
		{"100%% sure, %d", "%u", true},
		{"%", "%d", false},
	} {
		if _, ok := fmtcheck(test.desc, test.def); ok != test.ok {
			t.Errorf("%q against %q: expected %v, got: %v", test.desc, test.def, test.ok, ok)
		}
	}
}

// TestCompressWithoutDecoder holds D45: with no decoder registered, the port
// answers as libmagic does when it finds no program for the format.
func TestCompressWithoutDecoder(t *testing.T) {
	t.Parallel()
	e := New(&rules.Database{})
	got, err := e.Buffer([]byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\x03hello"), Compress)
	want := "ERROR:[gzip: Cannot posix_spawn `gzip', No such file or directory] (data)"
	if err != nil || got != want {
		t.Errorf("expected %q, got: %q, %v", want, got, err)
	}
}

// TestWindowMatchesFind checks that a cached window gives the match that a
// conversion of the same bytes gives, on random bytes with and without
// characters above 0x7f.
func TestWindowMatchesFind(t *testing.T) {
	t.Parallel()
	rx, err := compileRegex([]byte("[a-z\x8a]+[0-9]*"), false)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	rng := rand.New(rand.NewPCG(1, 2))
	alphabet := []byte("abc019 \n\x8a\xe9\xff")
	for range 500 {
		buf := make([]byte, 1+rng.IntN(300))
		for i := range buf {
			buf[i] = alphabet[rng.IntN(len(alphabet))]
		}
		st := &state{}
		w := st.window(buf)
		for range 5 {
			end := rng.IntN(len(buf) + 1)
			so, eo, ok := w.find(rx, end)
			wso, weo, wok := rx.find(buf[:end])
			if so != wso || eo != weo || ok != wok {
				t.Fatalf("%q[:%d]: expected (%d, %d, %v), got: (%d, %d, %v)", buf, end, wso, weo, wok, so, eo, ok)
			}
		}
	}
}
