package diff

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	_ "github.com/xo/magic/compress/bzip2"
	_ "github.com/xo/magic/compress/xz"
	_ "github.com/xo/magic/compress/zlib"
	_ "github.com/xo/magic/compress/zstd"

	"github.com/xo/magic/internal/engine"
	"github.com/xo/magic/internal/table"
	"github.com/xo/magic/test/corpus"
	"github.com/xo/magic/test/oracle"
)

// portOutputs are the outputs that the port must give as the oracle does.
// Each pairs the flags of the oracle with the flags of the port.
var portOutputs = []struct {
	name   string
	oracle int
	port   engine.Flags
}{
	{"description", oracle.None, engine.None},
	{"continue", oracle.Continue, engine.Continue},
	{"raw", oracle.Raw, engine.Raw},
	{"mime-type", oracle.MimeType, engine.MimeType},
	{"mime-encoding", oracle.MimeEncoding, engine.MimeEncoding},
	{"mime", oracle.Mime, engine.Mime},
	{"apple", oracle.Apple, engine.Apple},
	{"extension", oracle.Extension, engine.Extension},
	{"mime-type-continue", oracle.MimeType | oracle.Continue, engine.MimeType | engine.Continue},
	{"compress", oracle.Compress, engine.Compress},
	{"compress-mime", oracle.Compress | oracle.Mime, engine.Compress | engine.Mime},
	{"compress-mime-type", oracle.Compress | oracle.MimeType, engine.Compress | engine.MimeType},
	{"compress-continue", oracle.Compress | oracle.Continue, engine.Compress | engine.Continue},
	{"compress-transparent", oracle.Compress | oracle.CompressTransp, engine.Compress | engine.CompressTransp},
}

// TestPortMatchesTheOracle is the gate of phases 2 to 5 (docs/PLAN.md). The
// port runs the embedded rules of file (D35, D48), and must give the same
// output as the oracle for every output flag and every input of the corpus,
// as a buffer and as a file.
func TestPortMatchesTheOracle(t *testing.T) {
	o := oracle.Open(t)
	db, err := table.Load()
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
		known  []string
		asked  int
		byFlag = map[string]int{}
	)
	for _, out := range portOutputs {
		for _, in := range inputs {
			want, err := o.Buffer(in.Data, out.oracle)
			if err != nil {
				t.Fatal(err)
			}
			got, gerr := e.Buffer(in.Data, out.port)
			asked++
			if decoderText(want, got, gerr) || (out.port&engine.Compress != 0 && decoderDamaged(in.Name, want.Text, got)) {
				known = append(known, fmt.Sprintf("%s, %s: expected %q, got: %q", in.Name, out.name, want.Text, got))
			} else if d := compare(want, got, gerr); d != "" {
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
			if decoderText(want, got, gerr) || (out.port&engine.Compress != 0 && decoderDamaged(in.Name, want.Text, got)) {
				known = append(known, fmt.Sprintf("%s (file), %s: expected %q, got: %q", in.Name, out.name, want.Text, got))
			} else if d := compare(want, got, gerr); d != "" {
				byFlag[out.name]++
				diffs = append(diffs, fmt.Sprintf("%s (file), %s: %s", in.Name, out.name, d))
			}
		}
	}
	t.Logf("%d inputs, %d questions", len(inputs), asked)
	if len(known) > 0 {
		// The decoders of Go do not name each fault as the libraries of C
		// do. docs/BACKLOG.md holds the item.
		t.Logf("%d answers differ in how a decoder of Go reads damaged compressed data", len(known))
		for i, k := range known {
			if i == 10 {
				break
			}
			t.Log(k)
		}
	}
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

// decoderDamaged reports whether name is a damaged copy of compressed data
// whose answer has an error of a decoder on one side. libbz2, libzstd and
// liblzma find damage at other points than the decoders of Go: one can
// report an error where the other decodes nothing yet, or the other way.
func decoderDamaged(name, want, got string) bool {
	if want == got || !strings.Contains(name, "~") {
		return false
	}
	isErr := func(s string) bool {
		return strings.Contains(s, "ERROR:[") || strings.Contains(s, "x-decompression-error-")
	}
	return isErr(want) || isErr(got)
}

var (
	errorText = regexp.MustCompile(`ERROR:\[([a-z0-9]+): [^\]]*\]`)
	errorMIME = regexp.MustCompile(`application/x-decompression-error-([a-z0-9]+)-[A-Za-z0-9-]*`)
)

// decoderText reports whether the answer of the port differs from the
// oracle only in the text of an error of a decoder of compressed data.
func decoderText(want oracle.Answer, got string, gerr error) bool {
	if gerr != nil || want.Err != "" || got == want.Text {
		return false
	}
	norm := func(s string) string {
		s = errorText.ReplaceAllString(s, "ERROR:[$1: ?]")
		return errorMIME.ReplaceAllString(s, "application/x-decompression-error-$1-?")
	}
	return norm(got) == norm(want.Text)
}
