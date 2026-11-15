package walk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestMatcherOrder holds the order of the matcher of binwalk: by the end of
// each match, then longest first, then in the order of the patterns.
func TestMatcherOrder(t *testing.T) {
	t.Parallel()
	m := newMatcher([][]byte{[]byte("abc"), []byte("bc"), []byte("c"), []byte("bc"), []byte("xab")})
	var got []string
	m.find([]byte("xabcbc"), 0, func(off, p int) bool {
		got = append(got, fmt.Sprintf("%d:%d", off, p))
		return true
	})
	exp := []string{"0:4", "1:0", "2:1", "2:3", "3:2", "4:1", "4:3", "5:2"}
	if !slices.Equal(got, exp) {
		t.Errorf("expected %q, got: %q", exp, got)
	}
}

// TestMatcherStart holds that a search from an offset finds no match that
// starts before it.
func TestMatcherStart(t *testing.T) {
	t.Parallel()
	m := newMatcher([][]byte{[]byte("aa")})
	var got []int
	m.find([]byte("aaaa"), 1, func(off, _ int) bool {
		got = append(got, off)
		return true
	})
	if exp := []int{1, 2}; !slices.Equal(got, exp) {
		t.Errorf("expected %v, got: %v", exp, got)
	}
}

// sig returns a signature for the tests: a match at the magic, of the size
// and the confidence that the byte after the magic names.
func sig(name, magic string) *Signature {
	return &Signature{
		Name:  name,
		Magic: [][]byte{[]byte(magic)},
		Parse: func(data []byte, off int) (Match, error) {
			end := off + len(magic)
			if end+2 > len(data) {
				return Match{}, ErrNoMatch
			}
			c := map[byte]Confidence{'l': Low, 'm': Medium, 'h': High}[data[end+1]]
			return Match{
				Offset:      off,
				Size:        int(data[end] - '0'),
				Confidence:  c,
				Description: fmt.Sprintf("%s at %d", name, off),
			}, nil
		},
	}
}

// TestScan holds the rules of Binwalk::scan: the jump past a match of known
// size, the overlaps, and the size of a match that has none.
func TestScan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		exp  []string
	}{
		// A match of medium confidence and known size hides what is in it.
		{"skip", "A8m.C4h.B4h....", []string{"0+8 a", "8+4 b"}},
		// A match of low confidence does not.
		{"low", "A8l.B4h.....", []string{"0+8 a", "4+4 b"}},
		// A match of unknown size ends at the next match of medium or high
		// confidence.
		{"unknown", "A0h..B0l.C4m...", []string{"0+9 a", "5+4 b", "9+4 c"}},
		// At one offset, the higher confidence wins.
		{"same offset", "D4l.", []string{"0+4 e"}},
		// A size past the end is dropped.
		{"past end", "A9h.", nil},
	}
	sc := newScanner([]*Signature{
		sig("a", "A"), sig("b", "B"), sig("c", "C"), sig("d", "D"), sigAt("e", "D", High),
	}, false)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ms, sigs, _, err := sc.scan(context.Background(), []byte(tt.data))
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			var got []string
			for i, m := range ms {
				got = append(got, fmt.Sprintf("%d+%d %s", m.Offset, m.Size, sigs[i].Name))
			}
			if !slices.Equal(got, tt.exp) {
				t.Errorf("expected %q, got: %q", tt.exp, got)
			}
		})
	}
}

// sigAt returns a signature that matches at its magic with a size of 4 and
// the confidence c.
func sigAt(name, magic string, c Confidence) *Signature {
	return &Signature{
		Name:  name,
		Magic: [][]byte{[]byte(magic)},
		Parse: func(data []byte, off int) (Match, error) {
			if off+4 > len(data) {
				return Match{}, ErrNoMatch
			}
			return Match{Offset: off, Size: 4, Confidence: c}, nil
		},
	}
}

// TestShort holds that a short signature is tried at offset 0 only, unless
// the search is for all offsets.
func TestShort(t *testing.T) {
	t.Parallel()
	s := sigAt("s", "S", High)
	s.Short = true
	data := []byte("S...S....")
	for _, all := range []bool{false, true} {
		ms, _, _, err := newScanner([]*Signature{s}, all).scan(context.Background(), data)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		var got []int
		for _, m := range ms {
			got = append(got, m.Offset)
		}
		exp := []int{0}
		if all {
			exp = []int{0, 4}
		}
		if !slices.Equal(got, exp) {
			t.Errorf("all=%t: expected %v, got: %v", all, exp, got)
		}
	}
}

// TestScanStops holds that a scan stops when its context is done.
func TestScanStops(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sc := newScanner([]*Signature{sigAt("z", "z", Low)}, false)
	_, _, _, err := sc.scan(ctx, bytes.Repeat([]byte("z"), 4096))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected %v, got: %v", context.Canceled, err)
	}
}

// TestWalkFile walks a file through WalkFile, Walk and WalkBytes, which give
// the same entries.
func TestWalkFile(t *testing.T) {
	t.Parallel()
	Register(sigAt("walk_test", "WALKTEST", High))
	data := []byte("....WALKTEST....")
	name := filepath.Join(t.TempDir(), "image.bin")
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	ctx := context.Background()
	opt := WithFormats("WALK_TEST")
	for label, seq := range map[string]func(func(*Entry, error) bool){
		"WalkFile":  WalkFile(ctx, name, opt),
		"Walk":      Walk(ctx, bytes.NewReader(data), int64(len(data)), opt),
		"WalkBytes": WalkBytes(ctx, data, opt),
	} {
		var got []string
		for e, err := range seq {
			if err != nil {
				t.Fatalf("%s: expected no error, got: %v", label, err)
			}
			b := make([]byte, e.Size)
			if _, err := e.Section().ReadAt(b, 0); err != nil {
				t.Fatalf("%s: expected no error, got: %v", label, err)
			}
			got = append(got, fmt.Sprintf("%d+%d %s %s %t", e.Offset, e.Size, e.Format, b, e.SizeKnown))
		}
		if exp := []string{"4+4 walk_test WALK true"}; !slices.Equal(got, exp) {
			t.Errorf("%s: expected %q, got: %q", label, exp, got)
		}
	}
}

// TestNoFormat holds the error of a walk with no format.
func TestNoFormat(t *testing.T) {
	t.Parallel()
	for _, err := range WalkBytes(context.Background(), nil, WithFormats("no such format")) {
		if err == nil {
			t.Errorf("expected an error, got: nil")
		}
	}
}

// TestEntropy holds the blocks and the values of Entropy.
func TestEntropy(t *testing.T) {
	t.Parallel()
	if got := Entropy(nil); got != nil {
		t.Errorf("expected no block, got: %v", got)
	}
	data := make([]byte, 4096+3)
	for i := range 2048 {
		data[i] = byte(i)
	}
	blocks := Entropy(data)
	// 4099 bytes in blocks of 2 bytes: 2049 blocks, and a last one of 1 byte.
	if len(blocks) != 2050 || blocks[0].End != 2 || blocks[2049].Start != 4098 || blocks[2049].End != 4099 {
		t.Fatalf("expected 2049 blocks of 2 bytes and one of 1, got: %d, %v, %v", len(blocks), blocks[0], blocks[len(blocks)-1])
	}
	if e := blocks[0].Entropy; math.Abs(e-1) > 1e-6 {
		t.Errorf("expected an entropy of 1, got: %v", e)
	}
	if e := blocks[1500].Entropy; e != 0 {
		t.Errorf("expected an entropy of 0, got: %v", e)
	}
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	if e := Entropy(all)[0].Entropy; math.Abs(e-8) > 1e-5 {
		t.Errorf("expected an entropy of 8, got: %v", e)
	}
}
