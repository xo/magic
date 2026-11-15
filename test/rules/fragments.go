// Package rules holds the rule files that the build of file compiles, and
// tests the compiler of the rules, internal/rules, against the magic.mgc
// that the oracle built (D30, D35).
package rules

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/xo/magic/internal/rules"
	"github.com/xo/magic/test/oracle"
)

// Fragments returns the rule files that the build of file compiles, from
// MAGIC_FRAGMENTS of magic/Makefile.am. The folder Magdir can hold more: at
// FILE5_48 it holds etf, which the build leaves out.
func Fragments(t testing.TB) []rules.File {
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
