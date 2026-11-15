// Package rules tests the compiler of the rules, internal/rules, against the
// magic.mgc that the oracle built (D30, D35).
package rules

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/xo/magic/internal/rules"
	"github.com/xo/magic/test/oracle"
)

// fragments returns the rule files that the build of file compiles, from
// MAGIC_FRAGMENTS of magic/Makefile.am. The folder Magdir can hold more: at
// FILE5_48 it holds etf, which the build leaves out.
func fragments(t *testing.T) []rules.File {
	t.Helper()
	src := filepath.Join(oracle.Dir(), "src", "magic")
	mk, err := os.ReadFile(filepath.Join(src, "Makefile.am"))
	if err != nil {
		t.Skipf("the oracle is not built. Run test/oracle/build.sh: %v", err)
	}
	start := bytes.Index(mk, []byte("MAGIC_FRAGMENTS ="))
	end := bytes.Index(mk[start:], []byte("\n\n"))
	if start < 0 || end < 0 {
		t.Fatal("no MAGIC_FRAGMENTS in magic/Makefile.am")
	}
	var names []string
	for _, m := range regexp.MustCompile(`\$\(MAGIC_FRAGMENT_DIR\)/(\S+)`).FindAllSubmatch(mk[start:start+end], -1) {
		names = append(names, string(m[1]))
	}
	// libmagic sorts the paths of a folder with strcmp. The build copies the
	// files into a folder named magic and compiles it.
	slices.Sort(names)
	var out []rules.File
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(src, "Magdir", n))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, rules.File{Name: "magic/" + n, Data: b})
	}
	return out
}

// fields are the fields of struct magic, by their place in an entry, for the
// report of a difference.
var fields = []struct {
	name     string
	from, to int
}{
	{"flag", 0, 2}, {"cont_level", 2, 3}, {"factor", 3, 4}, {"reln", 4, 5},
	{"vallen", 5, 6}, {"type", 6, 7}, {"in_type", 7, 8}, {"in_op", 8, 9},
	{"mask_op", 9, 10}, {"cond", 10, 11}, {"factor_op", 11, 12},
	{"offset", 12, 16}, {"in_offset", 16, 20}, {"lineno", 20, 24},
	{"mask", 24, 32}, {"value", 32, 160}, {"desc", 160, 224},
	{"mimetype", 224, 304}, {"apple", 304, 312}, {"ext", 312, 432},
}

// TestCompilerMatchesMagicMgc holds D35. The compiler of the port reads the
// same rule files as the build of file, and must give the same entries, in
// the same order, as the magic.mgc of the oracle, byte for byte.
//
// magic.mgc is the array of struct magic that libmagic compiled. So a match
// here means that each field of each rule, its strength and its place in the
// order are the ones of libmagic.
func TestCompilerMatchesMagicMgc(t *testing.T) {
	want, err := os.ReadFile(filepath.Join(oracle.Dir(), "root", "share", "misc", "magic.mgc"))
	if err != nil {
		t.Skipf("the oracle is not built. Run test/oracle/build.sh: %v", err)
	}
	files := fragments(t)
	db, err := rules.Compile(files)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	got := db.Encode()

	le := binary.LittleEndian
	for i, name := range []string{"set 0", "set 1"} {
		w, g := le.Uint32(want[8+4*i:]), le.Uint32(got[8+4*i:])
		if w != g {
			t.Errorf("%s: expected %d entries, got: %d", name, w, g)
		}
	}
	if !bytes.Equal(want[:16], got[:16]) {
		t.Errorf("the headers differ: expected % x, got: % x", want[:16], got[:16])
	}
	n := min(len(want), len(got)) / rules.EntrySize
	var diffs int
	for i := 1; i < n; i++ {
		w := want[i*rules.EntrySize : (i+1)*rules.EntrySize]
		g := got[i*rules.EntrySize : (i+1)*rules.EntrySize]
		if bytes.Equal(w, g) {
			continue
		}
		diffs++
		if diffs > 25 {
			continue
		}
		var which []string
		for _, f := range fields {
			if !bytes.Equal(w[f.from:f.to], g[f.from:f.to]) {
				which = append(which, fmt.Sprintf("%s: expected % x, got % x", f.name,
					trim(w[f.from:f.to]), trim(g[f.from:f.to])))
			}
		}
		t.Errorf("entry %d (line %d, %q): %v", i, le.Uint32(w[20:]), cstr(w[160:224]), which)
	}
	if diffs > 0 {
		t.Errorf("%d of %d entries differ", diffs, n-1)
	}
	if len(want) != len(got) {
		t.Errorf("expected %d bytes, got: %d", len(want), len(got))
	}
	t.Logf("%d files, %d entries", len(files), n-1)
}

func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

// trim cuts the zero bytes at the end of a field, so that a report shows
// what differs.
func trim(b []byte) []byte {
	n := len(b)
	for n > 0 && b[n-1] == 0 {
		n--
	}
	return b[:min(n+1, len(b))]
}
