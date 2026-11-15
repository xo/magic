package diff

import (
	"fmt"
	"testing"

	"github.com/xo/magic/internal/engine"
	"github.com/xo/magic/internal/rules"
	"github.com/xo/magic/test/corpus"
	"github.com/xo/magic/test/oracle"
	rulestest "github.com/xo/magic/test/rules"
)

// annotations are the outputs that print no description, which phase 2
// covers (docs/PLAN.md). Each pairs the flags of the oracle with the flags
// of the port.
var annotations = []struct {
	name   string
	oracle int
	port   engine.Flags
}{
	{"mime-type", oracle.MimeType, engine.MimeType},
	{"mime-encoding", oracle.MimeEncoding, engine.MimeEncoding},
	{"mime", oracle.Mime, engine.Mime},
	{"apple", oracle.Apple, engine.Apple},
	{"extension", oracle.Extension, engine.Extension},
	{"mime-type-continue", oracle.MimeType | oracle.Continue, engine.MimeType | engine.Continue},
}

// TestPortMatchesTheOracle is the gate of phase 2 (docs/PLAN.md). The port
// compiles the rules of file 5.48 and must give the same MIME type, MIME
// encoding, Apple type and extension as the oracle, for every input of the
// corpus, as a buffer and as a file.
func TestPortMatchesTheOracle(t *testing.T) {
	o := oracle.Open(t)
	db, err := rules.Compile(rulestest.Fragments(t))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	e := engine.New(db)
	inputs, err := corpus.All()
	if err != nil {
		t.Fatal(err)
	}
	var (
		diffs  []string
		asked  int
		byFlag = map[string]int{}
	)
	for _, out := range annotations {
		for _, in := range inputs {
			want, err := o.Buffer(in.Data, out.oracle)
			if err != nil {
				t.Fatal(err)
			}
			got, gerr := e.Buffer(in.Data, out.port)
			asked++
			if d := compare(want, got, gerr); d != "" {
				byFlag[out.name]++
				diffs = append(diffs, fmt.Sprintf("%s, %s: %s", in.Name, out.name, d))
			}
			if in.Path == "" {
				continue
			}
			want, err = o.File(in.Path, out.oracle)
			if err != nil {
				t.Fatal(err)
			}
			got, gerr = e.File(in.Path, out.port)
			asked++
			if d := compare(want, got, gerr); d != "" {
				byFlag[out.name]++
				diffs = append(diffs, fmt.Sprintf("%s (file), %s: %s", in.Name, out.name, d))
			}
		}
	}
	t.Logf("%d inputs, %d questions", len(inputs), asked)
	if len(diffs) == 0 {
		return
	}
	t.Errorf("%d of %d answers differ from the oracle, by output: %v", len(diffs), asked, byFlag)
	for i, d := range diffs {
		if i == 40 {
			t.Errorf("and %d more", len(diffs)-i)
			break
		}
		t.Error(d)
	}
}
