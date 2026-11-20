package magic

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// extraSamples are the samples of the rules of WithExtraRules (D67). lib is
// what libmagic reports at the commit of the ledger (D11, D48), which the
// gates of the test module compare with the oracle, because the corpus holds
// the files of testdata. extra is what WithExtraRules reports.
var extraSamples = []struct {
	file  string
	make  func() []byte
	lib   Result
	extra Result
}{
	{
		file: "fb2.fb2",
		make: fb2Sample,
		lib:  Result{Description: "XML 1.0 document, ASCII text", MIME: "text/xml", Charset: "us-ascii"},
		extra: Result{
			Description: "FictionBook document", MIME: "application/x-fictionbook+xml",
			Charset: "us-ascii", Extensions: []string{"fb2"},
		},
	},
	{
		file: "emf.emf",
		make: emfSample,
		lib: Result{
			Description: "Windows Enhanced Metafile (EMF) image data version 0x10000",
			MIME:        "application/octet-stream", Charset: "binary",
		},
		extra: Result{
			Description: "Windows Enhanced Metafile (EMF) image data version 0x10000",
			MIME:        "image/emf", Charset: "binary", Extensions: []string{"emf"},
		},
	},
}

// fb2Sample makes a FictionBook 2.0 book of one paragraph.
func fb2Sample() []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
 <description>
  <title-info>
   <genre>prose</genre>
   <author><first-name>Sample</first-name><last-name>Author</last-name></author>
   <book-title>Sample Book</book-title>
   <lang>en</lang>
  </title-info>
 </description>
 <body>
  <section><p>A sample book for the tests of magic.</p></section>
 </body>
</FictionBook>
`)
}

// emfSample makes an EMF image with two records: the header, and the end of
// the file ([MS-EMF] 2.3.4.2 and 2.3.4.1).
func emfSample() []byte {
	le := binary.LittleEndian
	var b []byte
	b = le.AppendUint32(b, 1)  // Type: EMR_HEADER
	b = le.AppendUint32(b, 88) // Size
	for _, v := range []uint32{0, 0, 15, 15, 0, 0, 400, 400} {
		b = le.AppendUint32(b, v) // Bounds in pixels, Frame in 0.01 mm
	}
	b = le.AppendUint32(b, 0x464d4520) // Signature: " EMF"
	b = le.AppendUint32(b, 0x10000)    // Version
	b = le.AppendUint32(b, 108)        // Bytes of the file
	b = le.AppendUint32(b, 2)          // Records
	b = le.AppendUint16(b, 1)          // Handles
	b = le.AppendUint16(b, 0)          // Reserved
	for _, v := range []uint32{0, 0, 0, 1024, 768, 320, 240} {
		b = le.AppendUint32(b, v) // no description and no palette, Device, Millimeters
	}
	for _, v := range []uint32{14, 20, 0, 16, 20} {
		b = le.AppendUint32(b, v) // EMR_EOF
	}
	return b
}

// TestExtraRules checks each sample with and without WithExtraRules, with
// each entry. With -update, it writes the samples again.
func TestExtraRules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, s := range extraSamples {
		path := filepath.Join("testdata", s.file)
		if *update {
			if err := os.WriteFile(path, s.make(), 0o644); err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !bytes.Equal(b, s.make()) {
			t.Errorf("%s: expected the sample that the test makes. Run the test with -update", s.file)
		}
		entries := map[string]func(...Option) (Result, error){
			"bytes":  func(opts ...Option) (Result, error) { return Detect(ctx, b, opts...) },
			"string": func(opts ...Option) (Result, error) { return Detect(ctx, string(b), opts...) },
			"reader": func(opts ...Option) (Result, error) { return DetectReader(ctx, bytes.NewReader(b), opts...) },
			"readerat": func(opts ...Option) (Result, error) {
				return DetectReaderAt(ctx, bytes.NewReader(b), int64(len(b)), opts...)
			},
			"file": func(opts ...Option) (Result, error) { return DetectFile(ctx, path, opts...) },
		}
		for name, detect := range entries {
			r, err := detect()
			if err != nil {
				t.Fatalf("%s %s: expected no error, got: %v", s.file, name, err)
			}
			checkResult(t, s.file+" "+name, s.lib, r)
			r, err = detect(WithExtraRules())
			if err != nil {
				t.Fatalf("%s %s: expected no error, got: %v", s.file, name, err)
			}
			checkResult(t, s.file+" "+name+" extra", s.extra, r)
		}
	}
}

// checkResult compares each form of got with exp, and the first match with
// the forms of got.
func checkResult(t *testing.T, name string, exp, got Result) {
	t.Helper()
	if got.Description != exp.Description {
		t.Errorf("%s: expected %q, got: %q", name, exp.Description, got.Description)
	}
	if got.MIME != exp.MIME {
		t.Errorf("%s: expected %q, got: %q", name, exp.MIME, got.MIME)
	}
	if got.Charset != exp.Charset {
		t.Errorf("%s: expected %q, got: %q", name, exp.Charset, got.Charset)
	}
	if !slices.Equal(got.Extensions, exp.Extensions) {
		t.Errorf("%s: expected %q, got: %q", name, exp.Extensions, got.Extensions)
	}
	if got.Apple != exp.Apple {
		t.Errorf("%s: expected %q, got: %q", name, exp.Apple, got.Apple)
	}
	if len(got.Matches) != 1 {
		t.Fatalf("%s: expected 1 match, got: %d", name, len(got.Matches))
	}
	m := got.Matches[0]
	if m.Description != got.Description || m.MIME != got.MIME || !slices.Equal(m.Extensions, got.Extensions) || m.Apple != got.Apple {
		t.Errorf("%s: expected the match %+v, got: %+v", name, Match{got.Description, got.MIME, got.Extensions, got.Apple}, m)
	}
}

// TestExtraRulesAll checks that a rule of magic changes only the first
// match of WithAll.
func TestExtraRulesAll(t *testing.T) {
	t.Parallel()
	r, err := Detect(context.Background(), fb2Sample(), WithAll(), WithExtraRules())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	exp := []string{"FictionBook document", "XML document, ASCII text"}
	got := make([]string, 0, len(r.Matches))
	for _, m := range r.Matches {
		got = append(got, m.Description)
	}
	if !slices.Equal(got, exp) || r.Description != strings.Join(exp, "\n- ") {
		t.Errorf("expected %q, got: %q", exp, got)
	}
	if exp := "application/x-fictionbook+xml"; r.MIME != exp || r.Matches[0].MIME != exp {
		t.Errorf("expected %q, got: %q", exp, r.MIME)
	}
}

// TestExtraRulesKeep checks that WithExtraRules keeps the answer of
// libmagic where no rule of magic matches.
func TestExtraRulesKeep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, test := range []struct {
		name string
		s    string
	}{
		{"xml", `<?xml version="1.0"?><root xmlns="http://www.gribuser.ru/xml/fictionbook/2.0"/>`},
		{"other namespace", `<?xml version="1.0"?><FictionBook xmlns="http://example.com/"/>`},
		{"no namespace", `<?xml version="1.0"?><FictionBook/>`},
		{"text", "FictionBook http://www.gribuser.ru/xml/fictionbook/2.0\n"},
		{"gif", "GIF89a\x01\x00\x01\x00\x00\x00\x00;"},
	} {
		lib, err := Detect(ctx, test.s)
		if err != nil {
			t.Fatalf("%s: expected no error, got: %v", test.name, err)
		}
		r, err := Detect(ctx, test.s, WithExtraRules())
		if err != nil {
			t.Fatalf("%s: expected no error, got: %v", test.name, err)
		}
		if r.Description != lib.Description || r.MIME != lib.MIME || !slices.Equal(r.Extensions, lib.Extensions) {
			t.Errorf("%s: expected %q, got: %q", test.name, lib.Description+" "+lib.MIME, r.Description+" "+r.MIME)
		}
	}
	r, err := DetectFile(ctx, "testdata", WithExtraRules())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if exp := "inode/directory"; r.MIME != exp {
		t.Errorf("expected %q, got: %q", exp, r.MIME)
	}
}

// TestRootElement checks the reader of the root element of FictionBook.
func TestRootElement(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		s    string
		name string
		ns   string
		ok   bool
	}{
		{`<FictionBook xmlns="ns">`, "FictionBook", "ns", true},
		{"\ufeff<?xml version='1.0'?>\n<FictionBook xmlns='ns'/>", "FictionBook", "ns", true},
		{`<!-- a > b --><?pi x?><FictionBook  xmlns = "ns" >`, "FictionBook", "ns", true},
		{`<!DOCTYPE FictionBook [<!ENTITY a "]>">]><FictionBook xmlns="ns">`, "FictionBook", "ns", true},
		{`<fb:FictionBook xmlns:fb="ns" a="1 > 2">`, "fb:FictionBook", "", true},
		{`<FictionBook xmlns="ns"`, "", "", false},
		{`<!-- no end`, "", "", false},
		{`text <FictionBook>`, "", "", false},
		{`</FictionBook>`, "", "", false},
		{`<FictionBook xmlns=ns>`, "", "", false},
	} {
		name, attrs, ok := rootElement(test.s)
		if ok != test.ok || name != test.name || attrs["xmlns"] != test.ns {
			t.Errorf("%q: expected %q %q %t, got: %q %q %t", test.s, test.name, test.ns, test.ok, name, attrs["xmlns"], ok)
		}
	}
	ok, err := matchFictionBook(Match{MIME: "text/xml"}, func() ([]byte, error) {
		return []byte(`<fb:FictionBook xmlns:fb="` + fictionBookNS + `">`), nil
	})
	if err != nil || !ok {
		t.Errorf("expected %t, got: %t, %v", true, ok, err)
	}
}
