package huffcdic

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// entry is a phrase of a test book. A raw phrase holds text, and a phrase
// that is not raw holds the indexes of other phrases.
type entry struct {
	text  string
	codes []int
}

// book is the HUFF record and the CDIC records of a test book, and the code
// of each phrase.
type book struct {
	huff  []byte
	cdics [][]byte
	// bits and code are the length and the value of the code of each phrase.
	bits, code []int
}

// build makes the records of a book. bits[i] is the length of the code of
// phrase i. The lengths go up, and they make a complete code. perCDIC is the
// number of bits of an index of a CDIC record.
func build(t testing.TB, bits []int, phrases []entry, perCDIC int) *book {
	t.Helper()
	b := &book{bits: bits, code: make([]int, len(bits))}
	// The codes of each length take the highest values that are left, and
	// the stored highest code adds the index of the first phrase of the
	// length, as the format does.
	var minCode, maxCode [33]uint64
	upper, first := uint64(2), 0
	for n := 1; n <= 32; n++ {
		count := 0
		for _, l := range bits {
			if l == n {
				count++
			}
		}
		if upper < uint64(count) {
			t.Fatalf("the code has too many phrases of %d bits", n)
		}
		lo := upper - uint64(count)
		for j := range count {
			b.code[first+j] = int(upper - 1 - uint64(j))
		}
		minCode[n], maxCode[n] = lo, upper-1+uint64(first)
		first += count
		upper = lo * 2
	}
	huff := make([]byte, 24+256*4+64*4)
	copy(huff, "HUFF")
	binary.BigEndian.PutUint32(huff[4:], 24)
	binary.BigEndian.PutUint32(huff[8:], 24)
	binary.BigEndian.PutUint32(huff[12:], 24+256*4)
	for v := range 256 {
		e := uint32(9) // not final: the code is longer than 8 bits
		for n := 1; n <= 8; n++ {
			if uint64(v>>(8-n)) >= minCode[n] {
				e = uint32(maxCode[n])<<8 | 0x80 | uint32(n)
				break
			}
		}
		binary.BigEndian.PutUint32(huff[24+4*v:], e)
	}
	for n := 1; n <= 32; n++ {
		p := 24 + 256*4 + (n-1)*8
		binary.BigEndian.PutUint32(huff[p:], uint32(min(minCode[n], 0xFFFFFFFF)))
		binary.BigEndian.PutUint32(huff[p+4:], uint32(maxCode[n]))
	}
	b.huff = huff
	for start := 0; start < len(phrases); start += 1 << perCDIC {
		part := phrases[start:min(start+1<<perCDIC, len(phrases))]
		rec := make([]byte, 16+2*len(part))
		copy(rec, "CDIC")
		binary.BigEndian.PutUint32(rec[4:], 16)
		binary.BigEndian.PutUint32(rec[8:], uint32(len(phrases)))
		binary.BigEndian.PutUint32(rec[12:], uint32(perCDIC))
		for j, p := range part {
			binary.BigEndian.PutUint16(rec[16+2*j:], uint16(len(rec)-16))
			data, flag := []byte(p.text), uint16(0x8000)
			if p.codes != nil {
				data, flag = b.encode(p.codes), 0
			}
			rec = binary.BigEndian.AppendUint16(rec, uint16(len(data))|flag)
			rec = append(rec, data...)
		}
		b.cdics = append(b.cdics, rec)
	}
	return b
}

// encode writes the codes of the phrases idx, and pads the last byte with
// zeros.
func (b *book) encode(idx []int) []byte {
	var out []byte
	var acc uint64
	var n int
	for _, i := range idx {
		acc = acc<<b.bits[i] | uint64(b.code[i])
		n += b.bits[i]
		for n >= 8 {
			out = append(out, byte(acc>>(n-8)))
			n -= 8
		}
	}
	if n > 0 {
		out = append(out, byte(acc<<(8-n)))
	}
	return out
}

// chain gives the lengths of a complete code of n phrases: 1, 2 and so on to
// n-1, and n-1 again.
func chain(n int) []int {
	bits := make([]int, n)
	for i := range bits {
		bits[i] = min(i+1, n-1)
	}
	return bits
}

// fox is a book of 13 phrases in two CDIC records. Phrase 10 holds phrase 9,
// which holds three raw phrases. The codes have from 1 to 12 bits, so the
// cache table has final entries and entries that are not.
var foxPhrases = []entry{
	{text: "the "}, {text: "quick "}, {text: "brown "}, {text: "fox "},
	{text: "jumps "}, {text: "over "}, {text: "lazy "}, {text: "dog"},
	{text: "."}, {codes: []int{0, 1, 2}}, {codes: []int{9, 3}}, {text: ""},
	{text: "\n"},
}

const foxText = "the quick brown fox jumps over the lazy dog.\n"

var foxRecord = []int{10, 4, 5, 0, 6, 7, 8, 11, 12}

// TestDecode decodes a record of the fox book twice, so that the second
// time uses the phrases that the first decoded.
func TestDecode(t *testing.T) {
	t.Parallel()
	b := build(t, chain(13), foxPhrases, 3)
	if len(b.cdics) != 2 {
		t.Fatalf("expected 2 CDIC records, got: %d", len(b.cdics))
	}
	d, err := New(b.huff, b.cdics)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	rec := b.encode(foxRecord)
	for range 2 {
		got, err := d.Decode([]byte(">"), rec, len(foxText))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if want := ">" + foxText; string(got) != want {
			t.Errorf("expected %q, got: %q", want, got)
		}
	}
	got, err := d.Decode(nil, nil, 10)
	if err != nil || len(got) != 0 {
		t.Errorf("expected no bytes and no error, got: %q, %v", got, err)
	}
}

// TestLimit decodes the fox record with each limit, and makes sure that
// Decode stops at it, in a phrase that holds others too.
func TestLimit(t *testing.T) {
	t.Parallel()
	b := build(t, chain(13), foxPhrases, 3)
	for limit := range len(foxText) {
		d, err := New(b.huff, b.cdics)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		got, err := d.Decode(nil, b.encode(foxRecord), limit)
		if !errors.Is(err, ErrLimit) {
			t.Errorf("limit %d: expected %v, got: %v", limit, ErrLimit, err)
		}
		if want := foxText[:limit]; string(got) != want {
			t.Errorf("limit %d: expected %q, got: %q", limit, want, got)
		}
		// The phrases that went over the limit decode again.
		got, err = d.Decode(nil, b.encode(foxRecord), len(foxText))
		if err != nil || string(got) != foxText {
			t.Errorf("limit %d: expected %q, got: %q, %v", limit, foxText, got, err)
		}
	}
}

// TestNesting decodes phrases that hold one another: a phrase that holds
// itself, a loop of two phrases, and a chain over the depth limit.
func TestNesting(t *testing.T) {
	t.Parallel()
	// 128 phrases of 7 bits each. Phrase i holds phrase i+1, and phrase 100
	// is raw.
	deep := make([]entry, 128)
	for i := range deep {
		deep[i] = entry{codes: []int{i + 1}}
	}
	deep[100] = entry{text: "x"}
	deep[101] = entry{codes: []int{101}}
	deep[102] = entry{codes: []int{103}}
	deep[103] = entry{codes: []int{102}}
	for i := 104; i < 128; i++ {
		deep[i] = entry{text: "y"}
	}
	bits := make([]int, 128)
	for i := range bits {
		bits[i] = 7
	}
	b := build(t, bits, deep, 7)
	for _, tt := range []struct {
		name   string
		phrase int
		want   string
		err    error
	}{
		{"depth 40", 60, "x", nil},
		{"depth 100", 0, "", ErrCorrupt},
		{"itself", 101, "", ErrCorrupt},
		{"loop", 102, "", ErrCorrupt},
	} {
		// A new decoder for each case, so that no case decodes a phrase
		// for the next.
		d, err := New(b.huff, b.cdics)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		got, err := d.Decode(nil, b.encode([]int{tt.phrase}), 100)
		if !errors.Is(err, tt.err) {
			t.Errorf("%s: expected %v, got: %v", tt.name, tt.err, err)
		}
		if string(got) != tt.want {
			t.Errorf("%s: expected %q, got: %q", tt.name, tt.want, got)
		}
	}
}

// TestCorrupt gives New and Decode damaged records.
func TestCorrupt(t *testing.T) {
	t.Parallel()
	b := build(t, chain(13), foxPhrases, 3)
	clone := func(p []byte, at int, v ...byte) []byte {
		p = append([]byte(nil), p...)
		copy(p[at:], v)
		return p
	}
	for _, tt := range []struct {
		name  string
		huff  []byte
		cdics [][]byte
	}{
		{"short HUFF", b.huff[:12], b.cdics},
		{"no HUFF", clone(b.huff, 0, 'X'), b.cdics},
		{"cache past the end", clone(b.huff, 8, 0, 0, 0x10, 0), b.cdics},
		{"base past the end", b.huff[:len(b.huff)-4], b.cdics},
		{"code of 0 bits", clone(b.huff, 24+3, 0x80), b.cdics},
		{"short code not final", clone(b.huff, 24+3, 0x01), b.cdics},
		{"no CDIC", b.huff, [][]byte{clone(b.cdics[0], 0, 'X'), b.cdics[1]}},
		{"short CDIC", b.huff, [][]byte{b.cdics[0][:10]}},
		{"32 bits", b.huff, [][]byte{clone(b.cdics[0], 12, 0, 0, 0, 32)}},
		{"offsets past the end", b.huff, [][]byte{b.cdics[0][:20]}},
		{"phrase past the end", b.huff, [][]byte{clone(b.cdics[0], 16, 0x7F, 0)}},
		{"phrase length past the end", b.huff, [][]byte{clone(b.cdics[0], 16+16, 0x7F, 0xFF)}},
	} {
		if _, err := New(tt.huff, tt.cdics); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: expected %v, got: %v", tt.name, ErrCorrupt, err)
		}
	}
	// With only the first CDIC record, codes 8 to 12 name no phrase.
	d, err := New(b.huff, b.cdics[:1])
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	got, err := d.Decode(nil, b.encode([]int{0, 12}), 100)
	if !errors.Is(err, ErrCorrupt) {
		t.Errorf("expected %v, got: %v", ErrCorrupt, err)
	}
	if string(got) != "the " {
		t.Errorf("expected %q, got: %q", "the ", got)
	}
}

// FuzzDecode reads any HUFF and CDIC records, and decodes any record with
// them. It must not panic, and must stay in the limit.
func FuzzDecode(f *testing.F) {
	b := build(f, chain(13), foxPhrases, 3)
	f.Add(b.huff, b.cdics[0], b.cdics[1], b.encode(foxRecord))
	f.Add(b.huff, b.cdics[0], b.cdics[1], []byte(strings.Repeat("\xFF\x00", 20)))
	f.Fuzz(func(t *testing.T, huff, cdic0, cdic1, rec []byte) {
		d, err := New(huff, [][]byte{cdic0, cdic1})
		if err != nil {
			return
		}
		const limit = 1 << 12
		got, err := d.Decode(nil, rec, limit)
		if len(got) > limit {
			t.Fatalf("expected at most %d bytes, got: %d", limit, len(got))
		}
		if err != nil && !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrLimit) {
			t.Fatalf("expected %v or %v, got: %v", ErrCorrupt, ErrLimit, err)
		}
	})
}
