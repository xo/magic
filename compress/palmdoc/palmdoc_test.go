package palmdoc

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// compress is a small PalmDOC compressor for the tests. It uses each of the
// four codes: a copy of 3 to 10 bytes, a space and a letter in one byte, a
// byte as itself, and a run of 1 to 8 bytes that need a count.
func compress(src []byte) []byte {
	var out []byte
	for i := 0; i < len(src); {
		var n, dist int
		for d := 1; d <= min(i, 2047); d++ {
			l := 0
			for l < 10 && i+l < len(src) && src[i+l] == src[i+l-d] {
				l++
			}
			if l > n {
				n, dist = l, d
			}
		}
		switch c := src[i]; {
		case n >= 3:
			v := 0x8000 | dist<<3 | (n - 3)
			out = append(out, byte(v>>8), byte(v))
			i += n
		case c == ' ' && i+1 < len(src) && src[i+1] >= 0x40 && src[i+1] <= 0x7F:
			out = append(out, src[i+1]^0x80)
			i += 2
		case plain(c):
			out = append(out, c)
			i++
		default:
			j := i
			for j < len(src) && j-i < 8 && !plain(src[j]) {
				j++
			}
			out = append(out, byte(j-i))
			out = append(out, src[i:j]...)
			i = j
		}
	}
	return out
}

// plain reports whether c stands for itself in PalmDOC.
func plain(c byte) bool {
	return c == 0 || c >= 0x09 && c <= 0x7F
}

// TestRoundTrip compresses texts with compress, and makes sure that Decode
// gives them back.
func TestRoundTrip(t *testing.T) {
	t.Parallel()
	binary := make([]byte, 600)
	for i := range binary {
		binary[i] = byte(i * 37)
	}
	for _, s := range []string{
		"",
		"a",
		"The quick brown fox jumps over the lazy dog. The quick brown fox jumps again.",
		strings.Repeat("abc", 1500),
		strings.Repeat("x", 4096),
		"tab\there, NUL\x00, and high bytes \x80\x81\xC0\xFF\x01\x08 at the end\xFE",
		string(binary),
		strings.Repeat("Mobipocket text record. ", 200)[:4096],
	} {
		enc := compress([]byte(s))
		got, err := Decode([]byte("prefix"), enc, len(s))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if want := "prefix" + s; string(got) != want {
			t.Errorf("expected %q, got: %q", want, got)
		}
	}
}

// TestCodes decodes each code by hand.
func TestCodes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src  []byte
		want string
	}{
		{[]byte{0x00, 0x09, 'A', 0x7F}, "\x00\tA\x7F"},
		{[]byte{0x03, 0x80, 0xFF, 0x01}, "\x80\xFF\x01"},
		{[]byte{0xE8, 0xE5}, " h e"},
		// "ab", then a copy of 2 back for 5 bytes, which overlaps.
		{[]byte{'a', 'b', 0x80, 2<<3 | 2}, "abababa"},
		// The longest copy, 10 bytes, of 1 byte back.
		{[]byte{'z', 0x80, 1<<3 | 7}, "zzzzzzzzzzz"},
	} {
		got, err := Decode(nil, tt.src, 100)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if string(got) != tt.want {
			t.Errorf("expected %q, got: %q", tt.want, got)
		}
	}
}

// TestErrors decodes damaged records and records over the limit. Decode
// returns the bytes before the fault.
func TestErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		src   []byte
		limit int
		want  string
		err   error
	}{
		{"copy before the start", []byte{'a', 0x80, 2 << 3}, 100, "a", ErrCorrupt},
		{"copy of distance 0", []byte{'a', 0x80, 0}, 100, "a", ErrCorrupt},
		{"copy cut short", []byte{'a', 0x80}, 100, "a", ErrCorrupt},
		{"run cut short", []byte{'a', 0x04, 'b', 'c'}, 100, "a", ErrCorrupt},
		{"byte over the limit", []byte{'a', 'b', 'c'}, 2, "ab", ErrLimit},
		{"run over the limit", []byte{0x04, 'a', 'b', 'c', 'd'}, 2, "ab", ErrLimit},
		{"pair over the limit", []byte{'a', 0xE8}, 2, "a ", ErrLimit},
		{"pair at the limit", []byte{'a', 0xE8}, 1, "a", ErrLimit},
		{"copy over the limit", []byte{'a', 0x80, 1<<3 | 7}, 4, "aaaa", ErrLimit},
		{"negative limit", []byte{'a'}, -1, "", ErrLimit},
	} {
		got, err := Decode(nil, tt.src, tt.limit)
		if !errors.Is(err, tt.err) {
			t.Errorf("%s: expected %v, got: %v", tt.name, tt.err, err)
		}
		if string(got) != tt.want {
			t.Errorf("%s: expected %q, got: %q", tt.name, tt.want, got)
		}
	}
}

// TestPrefixIsNotHistory makes sure that a copy cannot reach the bytes of
// dst before the record.
func TestPrefixIsNotHistory(t *testing.T) {
	t.Parallel()
	got, err := Decode([]byte("abc"), []byte{0x80, 3 << 3}, 100)
	if !errors.Is(err, ErrCorrupt) {
		t.Errorf("expected %v, got: %v", ErrCorrupt, err)
	}
	if string(got) != "abc" {
		t.Errorf("expected %q, got: %q", "abc", got)
	}
}

// FuzzDecode decodes any record, which must not panic, and must stay in the
// limit. It also compresses the input, and makes sure that the record
// decodes to it again.
func FuzzDecode(f *testing.F) {
	f.Add([]byte("The quick brown fox jumps over the lazy dog."))
	f.Add(compress([]byte(strings.Repeat("abc ", 100))))
	f.Add([]byte{0x80, 0x08, 0x08, 0xFF})
	f.Fuzz(func(t *testing.T, b []byte) {
		const limit = 4096
		got, err := Decode(nil, b, limit)
		if len(got) > limit {
			t.Fatalf("expected at most %d bytes, got: %d", limit, len(got))
		}
		if err != nil && !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrLimit) {
			t.Fatalf("expected %v or %v, got: %v", ErrCorrupt, ErrLimit, err)
		}
		got, err = Decode(nil, compress(b), len(b))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !bytes.Equal(got, b) {
			t.Fatalf("expected %q, got: %q", b, got)
		}
	})
}
