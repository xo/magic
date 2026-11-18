package lzfse

import (
	"bytes"
	"errors"
	"flag"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The files of testdata were written by the lzfse program of the reference
// code, built from the commit that docs/upstream/upstreams.tsv names (D63).
// With -update and -lzfse, the tests write them again with that program.
var (
	update   = flag.Bool("update", false, "write the files of testdata again, with the program that -lzfse names")
	lzfseCmd = flag.String("lzfse", "", "the lzfse program of the reference code, for -update")
)

// vectors are the inputs of the files of testdata, and the kinds of block
// that the reference encoder writes for each.
var vectors = []struct {
	name  string
	input func() []byte
	kinds []uint32
}{
	// Fewer than 8 bytes are not compressed.
	{"tiny", func() []byte { return []byte("LZFSE!\n") }, []uint32{magicUncompressed}},
	// Fewer than 4096 bytes that LZVN cannot make smaller.
	{"random", func() []byte { return random(3000, 1) }, []uint32{magicUncompressed}},
	// Fewer than 4096 bytes go to LZVN.
	{"short", func() []byte { return text(3000, 2) }, []uint32{magicLZVN}},
	// More go to LZFSE, in blocks of up to 10000 matches.
	{"text", func() []byte { return text(150000, 3) }, []uint32{magicV2}},
	// Long matches, and matches from over 65536 bytes back.
	{"mixed", mixed, []uint32{magicV2}},
}

// text returns n bytes of words, in an order that seed gives.
func text(n int, seed uint64) []byte {
	words := strings.Fields(`the a of and to in is it that for on with as was at by
		decoder stream block literal match distance state table frequency entropy
		Apple disk image compressed bytes header payload FSE LZVN LZFSE window`)
	rng := rand.New(rand.NewPCG(seed, 0x6c7a6673))
	var b bytes.Buffer
	for b.Len() < n {
		b.WriteString(words[rng.IntN(len(words))])
		if rng.IntN(12) == 0 {
			b.WriteString(".\n")
		} else {
			b.WriteByte(' ')
		}
	}
	return b.Bytes()[:n]
}

// random returns n random bytes, from seed.
func random(n int, seed uint64) []byte {
	rng := rand.New(rand.NewPCG(seed, 0x72616e64))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Uint32())
	}
	return b
}

// mixed returns text, random bytes, a long run of zeros, and copies of the
// text from far back.
func mixed() []byte {
	a := text(40000, 4)
	b := make([]byte, 0, 151000)
	b = append(b, a...)
	b = append(b, random(8000, 5)...)
	b = append(b, make([]byte, 30000)...)
	b = append(b, a[:30000]...)
	b = append(b, random(8000, 6)...)
	// From 111000 bytes back.
	b = append(b, a[5000:25000]...)
	b = append(b, bytes.Repeat([]byte("abc"), 5000)...)
	return b
}

// readVector reads the file of testdata of a vector. With -update, it first
// writes it with the reference program.
func readVector(t *testing.T, name string, input []byte) []byte {
	t.Helper()
	path := filepath.Join("testdata", name+".lzfse")
	if *update {
		if *lzfseCmd == "" {
			t.Fatal("expected -lzfse with -update")
		}
		in := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(in, input, 0o644); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if out, err := exec.Command(*lzfseCmd, "-encode", "-i", in, "-o", path).CombinedOutput(); err != nil {
			t.Fatalf("expected no error, got: %v: %s", err, out)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	return b
}

// blocks returns the magic number of each block of a valid stream, and its
// offset.
func blocks(t *testing.T, b []byte) (magics []uint32, offsets []int) {
	t.Helper()
	for p := 0; ; {
		magic := le.Uint32(b[p:])
		magics, offsets = append(magics, magic), append(offsets, p)
		switch magic {
		case magicEnd:
			return magics, offsets
		case magicUncompressed:
			p += 8 + int(le.Uint32(b[p+4:]))
		case magicLZVN:
			p += lzvnHeaderSize + int(le.Uint32(b[p+8:]))
		case magicV2:
			var d decoder
			n, err := d.readV2(b[p:])
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			p += n + int(d.header.literalPayload) + int(d.header.lmdPayload)
		default:
			t.Fatalf("expected a block at byte %d, got: %#x", p, magic)
		}
	}
}

// TestVectors decodes the files of testdata, which the reference encoder
// wrote, and makes sure that each holds the kinds of block that it is for.
func TestVectors(t *testing.T) {
	t.Parallel()
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			want := v.input()
			b := readVector(t, v.name, want)
			magics, _ := blocks(t, b)
			for _, k := range v.kinds {
				if !slices.Contains(magics, k) {
					t.Errorf("expected a block %#x, got: %#x", k, magics)
				}
			}
			got, err := Decode([]byte("prefix"), b, len(want))
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if !bytes.Equal(got, append([]byte("prefix"), want...)) {
				t.Errorf("the output differs from the input at byte %d", diff(got[6:], want))
			}
			// Bytes after the block of the end do not count.
			got, err = Decode(nil, append(b, "trailing"...), len(want))
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("expected the input and no error, got: %d bytes and %v", len(got), err)
			}
		})
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

// toV1 returns b with each block of version 2 written with the header of
// version 1, which the reference encoder does not write.
func toV1(t *testing.T, b []byte) []byte {
	t.Helper()
	magics, offsets := blocks(t, b)
	var out []byte
	last := 0
	for i, m := range magics {
		if m != magicV2 {
			continue
		}
		p := offsets[i]
		var d decoder
		n, err := d.readV2(b[p:])
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		h := &d.header
		out = append(out, b[last:p]...)
		v1 := make([]byte, v1HeaderSize)
		le.PutUint32(v1[0:], magicV1)
		copy(v1[4:8], b[p+4:p+8])
		le.PutUint32(v1[8:], h.literalPayload+h.lmdPayload)
		le.PutUint32(v1[12:], h.nLiterals)
		le.PutUint32(v1[16:], h.nMatches)
		le.PutUint32(v1[20:], h.literalPayload)
		le.PutUint32(v1[24:], h.lmdPayload)
		le.PutUint32(v1[28:], uint32(h.literalBits))
		for j, s := range h.literalState {
			le.PutUint16(v1[32+2*j:], s)
		}
		le.PutUint32(v1[40:], uint32(h.lmdBits))
		le.PutUint16(v1[44:], h.lState)
		le.PutUint16(v1[46:], h.mState)
		le.PutUint16(v1[48:], h.dState)
		for j, f := range h.freq {
			le.PutUint16(v1[50+2*j:], f)
		}
		out = append(out, v1...)
		last = p + n
	}
	return append(out, b[last:]...)
}

// TestV1 decodes the files of testdata with headers of version 1.
func TestV1(t *testing.T) {
	t.Parallel()
	for _, v := range vectors {
		want := v.input()
		b := toV1(t, readVector(t, v.name, want))
		got, err := Decode(nil, b, len(want))
		if err != nil {
			t.Fatalf("%s: expected no error, got: %v", v.name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: the output differs from the input at byte %d", v.name, diff(got, want))
		}
	}
}

// TestLimit decodes each file of testdata up to a limit, which cuts it.
func TestLimit(t *testing.T) {
	t.Parallel()
	for _, v := range vectors {
		want := v.input()
		b := readVector(t, v.name, want)
		for _, limit := range []int{0, 1, len(want) / 3, len(want) - 1} {
			got, err := Decode(nil, b, limit)
			if !errors.Is(err, ErrLimit) {
				t.Errorf("%s, %d: expected %v, got: %v", v.name, limit, ErrLimit, err)
			}
			if !bytes.Equal(got, want[:limit]) {
				t.Errorf("%s, %d: expected the first %d bytes, got: %d bytes", v.name, limit, limit, len(got))
			}
		}
	}
}

// TestCut decodes the files of testdata cut short. Each cut gives
// ErrCorrupt, and the bytes before the fault.
func TestCut(t *testing.T) {
	t.Parallel()
	for _, v := range vectors {
		want := v.input()
		b := readVector(t, v.name, want)
		step := max(1, len(b)/300)
		for n := 0; n < len(b); n += step {
			got, err := Decode(nil, b[:n], len(want))
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("%s[:%d]: expected %v, got: %v", v.name, n, ErrCorrupt, err)
			}
			if !bytes.HasPrefix(want, got) {
				t.Fatalf("%s[:%d]: expected a part of the input, got: %d bytes that differ at byte %d", v.name, n, len(got), diff(got, want))
			}
		}
	}
}

// TestDamaged changes bytes of the files of testdata. A change must give an
// error of the package, or bytes within the limit.
func TestDamaged(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(7, 8))
	for _, v := range vectors {
		want := v.input()
		b := readVector(t, v.name, want)
		for range 200 {
			c := bytes.Clone(b)
			for range 1 + rng.IntN(4) {
				c[rng.IntN(len(c))] = byte(rng.Uint32())
			}
			got, err := Decode(nil, c, len(want))
			if err != nil && !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrLimit) {
				t.Fatalf("%s: expected %v or %v, got: %v", v.name, ErrCorrupt, ErrLimit, err)
			}
			if len(got) > len(want) {
				t.Fatalf("%s: expected at most %d bytes, got: %d", v.name, len(want), len(got))
			}
		}
	}
}

// lzvnBlock returns a stream of one LZVN block with payload, which decodes
// to raw bytes.
func lzvnBlock(raw int, payload ...byte) []byte {
	b := le.AppendUint32(nil, magicLZVN)
	b = le.AppendUint32(b, uint32(raw))
	b = le.AppendUint32(b, uint32(len(payload)))
	b = append(b, payload...)
	return le.AppendUint32(b, magicEnd)
}

// end is the opcode of the end of LZVN, with its padding.
var end = []byte{6, 0, 0, 0, 0, 0, 0, 0}

// TestLZVN decodes LZVN blocks that the tests write, with each kind of
// opcode, and the faults that the decoder must find.
func TestLZVN(t *testing.T) {
	t.Parallel()
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	for _, test := range []struct {
		name    string
		raw     int
		payload []byte
		want    string
		err     error
	}{
		// A small literal, then a small distance: LL=0, MMM=0 (3 bytes),
		// D=3.
		{"small", 6, cat([]byte{0xE3}, []byte("abc"), []byte{0x00, 0x03}, end), "abcabc", nil},
		// A small distance with a literal of 2 bytes in the opcode.
		{"small literal", 5, cat([]byte{0x80, 0x01}, []byte("xy"), end), "xyyyy", nil},
		// A large distance: LLMMM111 and 2 bytes of distance.
		{"large", 5, cat([]byte{0x47 | 1<<3, 0x01, 0x00}, []byte("q"), end), "qqqqq", nil},
		// A medium distance: 101LLMMM DDDDDDMM DDDDDDDD, L=1, M=4, D=1.
		{"medium", 5, cat([]byte{0xA8, 0x05, 0x00}, []byte("m"), end), "mmmmm", nil},
		// The distance before it: LLMMM110 with L=1, M=3.
		{"previous", 10, cat([]byte{0xE3}, []byte("abc"), []byte{0x00, 0x03}, []byte{0x46}, []byte("z"), end), "abcabczbcz", nil},
		// Only a match, with the distance before it: 1111MMMM and
		// 11110000 MMMMMMMM.
		{"match", 25, cat([]byte{0xE3}, []byte("abc"), []byte{0x00, 0x03, 0xF2, 0xF0, 0x01}, end), "abcabcabcabcabcabcabcabca", nil},
		// A large literal, and no operation.
		{"large literal", 16, cat([]byte{0x0E, 0xE0, 0x00}, []byte("0123456789abcdef"), end), "0123456789abcdef", nil},
		{"no distance", 3, cat([]byte{0xF3}, end), "", ErrCorrupt},
		{"before the start", 4, cat([]byte{0xE1}, []byte("a"), []byte{0x00, 0x02}, end), "a", ErrCorrupt},
		{"undefined", 1, cat([]byte{0x70}, end), "", ErrCorrupt},
		{"too long", 2, cat([]byte{0xE3}, []byte("abc"), end), "", ErrCorrupt},
		{"too short", 4, cat([]byte{0xE3}, []byte("abc"), end), "abc", ErrCorrupt},
		{"no bytes", 0, end, "", ErrCorrupt},
		{"no end", 3, cat([]byte{0xE3}, []byte("abc"), []byte{0x0E}), "abc", ErrCorrupt},
		{"after the end", 3, cat([]byte{0xE3}, []byte("abc"), end, []byte{0}), "abc", ErrCorrupt},
	} {
		got, err := Decode(nil, lzvnBlock(test.raw, test.payload...), 1000)
		if !errors.Is(err, test.err) || (err == nil) != (test.err == nil) {
			t.Errorf("%s: expected %v, got: %v", test.name, test.err, err)
		}
		if string(got) != test.want {
			t.Errorf("%s: expected %q, got: %q", test.name, test.want, got)
		}
	}
}

// TestStreams decodes streams that the tests write, with the faults of the
// blocks around the LZVN blocks.
func TestStreams(t *testing.T) {
	t.Parallel()
	raw := func(s string) []byte {
		b := le.AppendUint32(nil, magicUncompressed)
		return append(le.AppendUint32(b, uint32(len(s))), s...)
	}
	endBlock := le.AppendUint32(nil, magicEnd)
	for _, test := range []struct {
		name string
		b    []byte
		want string
		err  error
	}{
		{"empty", endBlock, "", nil},
		{"two blocks", bytes.Join([][]byte{raw("ab"), raw("cd"), endBlock}, nil), "abcd", nil},
		// A match of an LZVN block can reach into the block before it.
		{"across blocks", append(raw("abc"), lzvnBlock(3, append([]byte{0x00, 0x03}, end...)...)...), "abcabc", nil},
		{"nothing", nil, "", ErrCorrupt},
		{"no end", raw("ab"), "ab", ErrCorrupt},
		{"unknown block", []byte("bvx?"), "", ErrCorrupt},
		{"cut block", raw("abc")[:9], "", ErrCorrupt},
		{"cut header", []byte("bvx2\x00\x00\x00"), "", ErrCorrupt},
	} {
		got, err := Decode(nil, test.b, 1000)
		if !errors.Is(err, test.err) || (err == nil) != (test.err == nil) {
			t.Errorf("%s: expected %v, got: %v", test.name, test.err, err)
		}
		if string(got) != test.want {
			t.Errorf("%s: expected %q, got: %q", test.name, test.want, got)
		}
	}
}

// FuzzDecode decodes any bytes. It must not panic or hang, and it must keep
// to the limit.
func FuzzDecode(f *testing.F) {
	for _, v := range vectors {
		b, err := os.ReadFile(filepath.Join("testdata", v.name+".lzfse"))
		if err != nil {
			f.Fatalf("expected no error, got: %v", err)
		}
		f.Add(b)
	}
	f.Add(lzvnBlock(6, append([]byte{0xE3, 'a', 'b', 'c', 0x00, 0x03}, end...)...))
	f.Fuzz(func(t *testing.T, b []byte) {
		const limit = 1 << 20
		got, err := Decode(nil, b, limit)
		if len(got) > limit {
			t.Fatalf("expected at most %d bytes, got: %d", limit, len(got))
		}
		if err != nil && !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrLimit) {
			t.Fatalf("expected %v or %v, got: %v", ErrCorrupt, ErrLimit, err)
		}
	})
}
