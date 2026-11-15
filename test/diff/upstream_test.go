package diff

import (
	"os"
	"strings"
	"testing"

	"github.com/xo/magic/internal/engine"
	"github.com/xo/magic/internal/rules"
	"github.com/xo/magic/test/oracle"
	rulestest "github.com/xo/magic/test/rules"
)

// TestPortPassesTheTestsOfFile is the gate of phase 3 (docs/PLAN.md, D37).
// The port runs each test of file 5.48 as the test program of file does,
// and must give the result that file expects.
func TestPortPassesTheTestsOfFile(t *testing.T) {
	cases, err := oracle.UpstreamCases()
	if err != nil {
		t.Skip(err)
	}
	db, err := rules.Compile(rulestest.Fragments(t))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	def := engine.New(db)
	failed := 0
	for _, c := range cases {
		e := def
		if c.Magic != "" {
			var files []rules.File
			for _, p := range strings.Split(c.Magic, ":") {
				b, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				files = append(files, rules.File{Name: p, Data: b})
			}
			db, err := rules.Compile(files)
			if err != nil {
				t.Errorf("%s: expected no error, got: %v", c.Name, err)
				failed++
				continue
			}
			e = engine.New(db)
		}
		got, err := e.File(c.TestFile, engine.Flags(c.Flags))
		if err != nil {
			t.Errorf("%s: expected no error, got: %v", c.Name, err)
			failed++
			continue
		}
		if got != c.Want {
			t.Errorf("%s: expected %q, got: %q", c.Name, c.Want, got)
			failed++
		}
	}
	t.Logf("%d of %d tests of file pass", len(cases)-failed, len(cases))
}
