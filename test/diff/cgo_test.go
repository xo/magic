package diff

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xo/magic"
	"github.com/xo/magic/test/corpus"
	"github.com/xo/magic/test/oracle"
)

// outputs are the output flags that the differential test compares. Each is
// one call of libmagic. Compress is left out, because libmagic can run an
// outside program for it, which the oracle stops (D36) and the cgo package
// does not.
var outputs = []struct {
	name  string
	flags int
}{
	{"description", oracle.None},
	{"mime-type", oracle.MimeType},
	{"mime-encoding", oracle.MimeEncoding},
	{"mime", oracle.Mime},
	{"extension", oracle.Extension},
	{"apple", oracle.Apple},
	{"continue", oracle.Continue},
}

// TestCgoPackageMatchesTheOracle is the gate of phase 1 (docs/PLAN.md). The cgo
// package of today and the oracle, built for glibc, must give the same output
// for every input of the corpus and every output flag. It proves that the
// harness works before the port exists, and it is the comparison that the
// port takes over.
//
// On Linux it passes with gcc, which links glibc, and with zig, which links
// musl, because the archives carry their own regex engine (D43). Before D43, a
// glibc link crashed on the test file cmd1 of file.
func TestCgoPackageMatchesTheOracle(t *testing.T) {
	o := oracle.Open(t)
	inputs, err := corpus.All()
	if err != nil {
		t.Fatal(err)
	}
	var (
		diffs  []string
		asked  int
		skips  int
		byFlag = map[string]int{}
	)
	for _, out := range outputs {
		m, err := magic.New(out.flags)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		for _, in := range inputs {
			// The cgo package refuses an empty buffer, where libmagic says
			// "empty". That is the wrapper, not libmagic.
			if len(in.Data) == 0 {
				skips++
				continue
			}
			want, err := o.Buffer(in.Data, out.flags)
			if err != nil {
				t.Fatal(err)
			}
			got, gerr := m.Buffer(in.Data)
			asked++
			if d := compare(want, got, gerr); d != "" {
				byFlag[out.name]++
				diffs = append(diffs, fmt.Sprintf("%s, %s: %s", in.Name, out.name, d))
			}
			if in.Path == "" {
				continue
			}
			want, err = o.File(in.Path, out.flags)
			if err != nil {
				t.Fatal(err)
			}
			got, gerr = m.File(in.Path)
			asked++
			if d := compare(want, got, gerr); d != "" {
				byFlag[out.name]++
				diffs = append(diffs, fmt.Sprintf("%s (file), %s: %s", in.Name, out.name, d))
			}
		}
		if err := m.Close(); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	}
	t.Logf("%d inputs, %d questions, %d empty inputs skipped", len(inputs), asked, skips)
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

// compare returns how the answer of the cgo package differs from the oracle,
// or "" when they agree. The cgo package returns the error of libmagic as an
// error whose text ends with the text of magic_error.
func compare(want oracle.Answer, got string, gerr error) string {
	switch {
	case want.Err != "" && gerr == nil:
		return fmt.Sprintf("expected the error %q, got: %q", want.Err, got)
	case want.Err == "" && gerr != nil:
		return fmt.Sprintf("expected %q, got the error: %v", want.Text, gerr)
	case want.Err != "":
		if !strings.HasSuffix(gerr.Error(), want.Err) {
			return fmt.Sprintf("expected the error %q, got: %v", want.Err, gerr)
		}
		return ""
	case got != want.Text:
		return fmt.Sprintf("expected %q, got: %q", want.Text, got)
	}
	return ""
}
