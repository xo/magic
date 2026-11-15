package oracle

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Case is one test of file, from tests/ of the tree that build.sh exported at
// the base commit. The test program of file runs magic_file on TestFile with
// Flags and compares the text with Want.
type Case struct {
	Name     string
	TestFile string
	Want     string
	Flags    int
	// Magic names the rule files of the test, joined by ':', or is empty for
	// the database of file.
	Magic string
}

// UpstreamCases returns the tests of file at the commit of the oracle, in the
// order of their names.
// It copies what tests/Makefile.am does: the flag -e always, the letters of a
// file <name>.flags, and the rule files <name>*.magic in place of the
// database.
func UpstreamCases() ([]Case, error) {
	dir := filepath.Join(Dir(), "src", "tests")
	files, err := filepath.Glob(filepath.Join(dir, "*.testfile"))
	if err != nil {
		return nil, fmt.Errorf("listing the tests of file: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("listing the tests of file: no test in %s. Run test/oracle/build.sh", dir)
	}
	slices.Sort(files)
	var out []Case
	for _, f := range files {
		base := strings.TrimSuffix(f, ".testfile")
		c := Case{Name: filepath.Base(base), TestFile: f, Flags: Error}
		want, err := os.ReadFile(base + ".result")
		if err != nil {
			return nil, fmt.Errorf("reading the result of %s: %w", c.Name, err)
		}
		// slurp in tests/test.c drops one newline at the end.
		c.Want = strings.TrimSuffix(string(want), "\n")
		if flags, err := os.ReadFile(base + ".flags"); err == nil {
			for _, r := range strings.TrimSpace(string(flags)) {
				switch r {
				case 'e':
					c.Flags |= Error
				case 'k':
					c.Flags |= Continue
				default:
					return nil, fmt.Errorf("reading the flags of %s: unknown flag %q", c.Name, r)
				}
			}
		}
		magic, err := filepath.Glob(base + "*.magic")
		if err != nil {
			return nil, fmt.Errorf("listing the rule files of %s: %w", c.Name, err)
		}
		slices.Sort(magic)
		c.Magic = strings.Join(magic, ":")
		out = append(out, c)
	}
	return out, nil
}
