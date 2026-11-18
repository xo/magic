package adc

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"
)

// compress is a small ADC compressor for the tests. It uses each of the
// three codes: a literal, a short match and a long match.
func compress(src []byte) []byte {
	var out []byte
	// last holds the last positions of each 3 bytes.
	last := make(map[[3]byte][]int)
	lit := 0 // the start of the literal that waits
	flush := func(end int) {
		for lit < end {
			n := min(end-lit, 128)
			out = append(out, byte(0x80|(n-1)))
			out = append(out, src[lit:lit+n]...)
			lit += n
		}
	}
	for i := 0; i < len(src); {
		var n, dist int
		if i+3 <= len(src) {
			key := [3]byte(src[i : i+3])
			for _, j := range last[key] {
				d := i - j
				if d > 65536 {
					continue
				}
				l := 0
				for l < 67 && i+l < len(src) && src[i+l] == src[i+l-d] {
					l++
				}
				if l > n || l == n && d < dist {
					n, dist = l, d
				}
			}
			// Distances of 1 and 2 overlap the match, and no earlier
			// position of the same 3 bytes finds them.
			for d := 1; d <= min(i, 2); d++ {
				l := 0
				for l < 67 && i+l < len(src) && src[i+l] == src[i+l-d] {
					l++
				}
				if l > n {
					n, dist = l, d
				}
			}
		}
		if n >= 3 && dist <= 1024 || n >= 4 {
			flush(i)
			if n <= 18 && dist <= 1024 {
				out = append(out, byte((n-3)<<2|(dist-1)>>8), byte(dist-1))
			} else {
				out = append(out, byte(0x40|(n-4)), byte((dist-1)>>8), byte(dist-1))
			}
			lit = i + n
		} else {
			n = 1
		}
		for k := i; k < i+n && k+3 <= len(src); k++ {
			key := [3]byte(src[k : k+3])
			last[key] = append(last[key], k)
			if len(last[key]) > 8 {
				last[key] = last[key][1:]
			}
		}
		i += n
	}
	flush(len(src))
	return out
}

// sample returns n bytes of text, zeros, random bytes and copies from far
// back, from seed.
func sample(n int, seed uint64) []byte {
	words := strings.Fields(`Apple disk image chunk sector block the a of and to in
		literal match distance volume partition header table resource fork`)
	rng := rand.New(rand.NewPCG(seed, 0x616463))
	var b []byte
	for len(b) < n {
		switch rng.IntN(5) {
		case 0:
			b = append(b, make([]byte, rng.IntN(3000))...)
		case 1:
			for range rng.IntN(500) {
				b = append(b, byte(rng.Uint32()))
			}
		case 2:
			if len(b) > 0 {
				from := max(0, len(b)-1-rng.IntN(70000))
				b = append(b, b[from:min(len(b), from+rng.IntN(400))]...)
			}
		default:
			for range rng.IntN(300) {
				b = append(b, words[rng.IntN(len(words))]...)
				b = append(b, ' ')
			}
		}
	}
	return b[:n]
}

// TestCodes decodes each code by itself.
func TestCodes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		src  []byte
		want string
	}{
		{"literal", []byte{0x82, 'a', 'b', 'c'}, "abc"},
		{"short match", []byte{0x82, 'a', 'b', 'c', 0x00, 0x02}, "abcabc"},
		{"short match of 18", []byte{0x80, 'x', 0x3C, 0x00}, strings.Repeat("x", 19)},
		{"long match", []byte{0x81, 'a', 'b', 0x40, 0x00, 0x01}, "ababab"},
		{"long match of 67", []byte{0x80, 'z', 0x7F, 0x00, 0x00}, strings.Repeat("z", 68)},
		{"empty", nil, ""},
	} {
		got, err := Decode(nil, test.src, 1000)
		if err != nil {
			t.Fatalf("%s: expected no error, got: %v", test.name, err)
		}
		if string(got) != test.want {
			t.Errorf("%s: expected %q, got: %q", test.name, test.want, got)
		}
	}
}

// TestRoundTrip compresses samples with compress, and makes sure that Decode
// gives them back.
func TestRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range [][]byte{
		nil,
		[]byte("a"),
		[]byte(strings.Repeat("abc", 1500)),
		make([]byte, 100000),
		sample(200000, 1),
		sample(5000, 2),
	} {
		enc := compress(s)
		got, err := Decode([]byte("prefix"), enc, len(s))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !bytes.Equal(got, append([]byte("prefix"), s...)) {
			t.Errorf("the output differs from the input at byte %d", diff(got[6:], s))
		}
	}
}

// TestCodesOfTheSample makes sure that the compressed sample of the tests has
// each code.
func TestCodesOfTheSample(t *testing.T) {
	t.Parallel()
	enc := compress(sample(200000, 1))
	var seen [3]bool
	for i := 0; i < len(enc); {
		switch c := enc[i]; {
		case c&0x80 != 0:
			seen[0] = true
			i += int(c&0x7F) + 2
		case c&0x40 != 0:
			seen[2] = true
			i += 3
		default:
			seen[1] = true
			i += 2
		}
	}
	if seen != [3]bool{true, true, true} {
		t.Errorf("expected each code, got: %v", seen)
	}
}

// diff returns the first byte at which a and b differ.
func diff(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// TestLimit decodes a chunk up to a limit, which cuts it in a literal and in
// a match.
func TestLimit(t *testing.T) {
	t.Parallel()
	s := sample(50000, 3)
	enc := compress(s)
	for _, limit := range []int{0, 1, 2, 1000, 25000, len(s) - 1} {
		got, err := Decode(nil, enc, limit)
		if !errors.Is(err, ErrLimit) {
			t.Errorf("%d: expected %v, got: %v", limit, ErrLimit, err)
		}
		if !bytes.Equal(got, s[:limit]) {
			t.Errorf("%d: expected the first %d bytes, got: %d bytes", limit, limit, len(got))
		}
	}
}

// TestCorrupt decodes chunks that ADC does not allow.
func TestCorrupt(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		src  []byte
		want string
	}{
		{"match first", []byte{0x00, 0x00}, ""},
		{"match too far", []byte{0x81, 'a', 'b', 0x00, 0x02}, "ab"},
		{"long match too far", []byte{0x80, 'a', 0x40, 0x00, 0x01}, "a"},
		{"cut literal", []byte{0x83, 'a', 'b'}, ""},
		{"cut short match", []byte{0x80, 'a', 0x00}, "a"},
		{"cut long match", []byte{0x80, 'a', 0x40, 0x00}, "a"},
	} {
		got, err := Decode([]byte("prefix"), test.src, 1000)
		if !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: expected %v, got: %v", test.name, ErrCorrupt, err)
		}
		if want := "prefix" + test.want; string(got) != want {
			t.Errorf("%s: expected %q, got: %q", test.name, want, got)
		}
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(compress(sample(3000, 4)))
	f.Add([]byte{0x82, 'a', 'b', 'c', 0x00, 0x02, 0x7F, 0x00, 0x00})
	f.Fuzz(func(t *testing.T, b []byte) {
		const limit = 1 << 20
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
			t.Fatalf("expected the input back, got: %d bytes that differ at byte %d", len(got), diff(got, b))
		}
	})
}
