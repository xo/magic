package diff

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/xo/magic"
	"github.com/xo/magic/test/corpus"
	"github.com/xo/magic/test/oracle"
)

// TestAPIMatchesTheOracle is the gate of phase 4 (docs/PLAN.md). Each form
// of a Result of the public API must be the output of the oracle for its
// flag, for every input of the corpus, as bytes and as a file (D46).
func TestAPIMatchesTheOracle(t *testing.T) {
	o := oracle.Open(t)
	inputs, err := corpus.All()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var diffs []string
	asked := 0
	check := func(name string, r magic.Result, ask func(int) (oracle.Answer, error)) {
		forms := []struct {
			form  string
			flags int
			got   string
		}{
			{"description", oracle.None, r.Description},
			{"mime-type", oracle.MimeType, r.MIME},
			{"mime-encoding", oracle.MimeEncoding, r.Charset},
			{"extension", oracle.Extension, orDefault(strings.Join(r.Extensions, "/"), "???")},
			{"apple", oracle.Apple, orDefault(r.Apple, "UNKNUNKN")},
		}
		for _, f := range forms {
			want, err := ask(f.flags)
			if err != nil {
				t.Fatal(err)
			}
			asked++
			if want.Err != "" || want.Text != f.got {
				diffs = append(diffs, fmt.Sprintf("%s, %s: expected %q%s, got: %q", name, f.form, want.Text, want.Err, f.got))
			}
		}
	}
	for _, in := range inputs {
		r, err := magic.Detect(ctx, in.Data)
		if err != nil {
			t.Fatalf("%s: expected no error, got: %v", in.Name, err)
		}
		check(in.Name, r, func(f int) (oracle.Answer, error) { return o.Buffer(in.Data, f) })
		if in.Path == "" {
			continue
		}
		r, err = magic.DetectFile(ctx, in.Path)
		if err != nil {
			t.Fatalf("%s: expected no error, got: %v", in.Name, err)
		}
		check(in.Name+" (file)", r, func(f int) (oracle.Answer, error) { return o.File(in.Path, f) })
	}
	t.Logf("%d inputs, %d questions", len(inputs), asked)
	for i, d := range diffs {
		if i == 40 {
			t.Errorf("and %d more", len(diffs)-i)
			break
		}
		t.Error(d)
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
