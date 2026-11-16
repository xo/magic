package cfb_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/xo/magic/cfb"
)

func TestParsePropertySet(t *testing.T) {
	t.Parallel()
	ps, err := cfb.ParsePropertySet(summary(cfb.CFMetafilePict, []byte("metafile")))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(ps.Sections) != 1 || ps.SystemID != 0x00020006 {
		t.Fatalf("expected 1 section from Windows, got: %d, %#x", len(ps.Sections), ps.SystemID)
	}
	s := ps.Section(cfb.FMTIDSummaryInformation)
	if s == nil {
		t.Fatalf("expected the section %s, got: none", cfb.FMTIDSummaryInformation)
	}
	if s.CodePage != 1252 {
		t.Errorf("expected the code page 1252, got: %d", s.CodePage)
	}
	text := func(id uint32) string {
		t.Helper()
		p, ok := s.Property(id)
		if !ok {
			t.Fatalf("expected the property %#x, got: none", id)
		}
		switch v := p.Value.(type) {
		case cfb.CodePageString:
			got, ok := v.Text()
			if !ok {
				t.Fatalf("expected the text of %q, got: none", v.Bytes)
			}
			return got
		case string:
			return v
		}
		t.Fatalf("expected a string, got: %T", p.Value)
		return ""
	}
	if exp, got := "Café €", text(cfb.PIDSITitle); got != exp {
		t.Errorf("expected %q, got: %q", exp, got)
	}
	if exp, got := "Microsoft Office Word", text(cfb.PIDSIAppName); got != exp {
		t.Errorf("expected %q, got: %q", exp, got)
	}
	values := map[uint32]any{
		cfb.PIDCodePage:    int16(1252),
		cfb.PIDSIEditTime:  cfb.Filetime(90 * 60 * 1e7),
		cfb.PIDSIPageCount: int32(3),
		0x40:               []byte("rawbytes"),
	}
	for id, exp := range values {
		p, ok := s.Property(id)
		if !ok || !reflect.DeepEqual(p.Value, exp) {
			t.Errorf("expected %#v for %#x, got: %#v", exp, id, p.Value)
		}
	}
	p, _ := s.Property(cfb.PIDSIEditTime)
	if d := p.Value.(cfb.Filetime).Duration(); d != 90*time.Minute {
		t.Errorf("expected %v, got: %v", 90*time.Minute, d)
	}
	p, _ = s.Property(cfb.PIDSICreateTime)
	if exp, got := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC), p.Value.(cfb.Filetime).Time(); !got.Equal(exp) {
		t.Errorf("expected %v, got: %v", exp, got)
	}
	thumb, ok := ps.Thumbnail()
	if !ok {
		t.Fatalf("expected a thumbnail, got: none")
	}
	if exp := (cfb.ClipboardData{Tag: -1, Format: cfb.CFMetafilePict, Data: []byte("metafile")}); !reflect.DeepEqual(thumb, exp) {
		t.Errorf("expected %+v, got: %+v", exp, thumb)
	}
}

func TestParsePropertySetVectors(t *testing.T) {
	t.Parallel()
	ps, err := cfb.ParsePropertySet(docSummary())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(ps.Sections) != 2 {
		t.Fatalf("expected 2 sections, got: %d", len(ps.Sections))
	}
	s := ps.Section(cfb.FMTIDDocSummaryInformation)
	str := func(s string) cfb.CodePageString {
		return cfb.CodePageString{CodePage: 1252, Bytes: append([]byte(s), 0)}
	}
	values := map[uint32]any{
		0x0C: []any{cfb.Variant{Type: cfb.VTLPSTR, Value: str("Title")}, cfb.Variant{Type: cfb.VTI4, Value: int32(1)}},
		0x0D: []any{str("Intro"), str("Body")},
		0x0F: []any{int16(1), int16(2), int16(-1)},
		0x10: true,
	}
	for id, exp := range values {
		p, ok := s.Property(id)
		if !ok || !reflect.DeepEqual(p.Value, exp) {
			t.Errorf("expected %#v for %#x, got: %#v", exp, id, p.Value)
		}
	}
	u := ps.Section(cfb.FMTIDUserDefinedProperties)
	if exp := map[uint32]string{2: "Client", 3: "Due"}; !reflect.DeepEqual(u.Names, exp) {
		t.Errorf("expected %v, got: %v", exp, u.Names)
	}
	if len(u.Properties) != 3 {
		t.Errorf("expected 3 properties without the dictionary, got: %d", len(u.Properties))
	}
	p, _ := u.Property(3)
	if !p.Value.(cfb.Filetime).Time().IsZero() {
		t.Errorf("expected the zero time, got: %v", p.Value.(cfb.Filetime).Time())
	}
}

// TestThumbnail reads the thumbnail of a document from its Compound File.
func TestThumbnail(t *testing.T) {
	t.Parallel()
	emf := append([]byte{1, 0, 0, 0}, pattern(5000, 9)...)
	l := build(t, 9, stream("WordDocument", pattern(600, 1)), stream("\x05SummaryInformation", summary(cfb.CFEnhMetafile, emf)))
	f := open(t, l.b, cfb.Limits{})
	data, err := f.ReadFile("\x05summaryinformation")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	ps, err := cfb.ParsePropertySet(data)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	thumb, ok := ps.Thumbnail()
	if !ok || thumb.Format != cfb.CFEnhMetafile || !bytes.Equal(thumb.Data, emf) {
		t.Errorf("expected the %d bytes of the EMF, got: %v, format %d, %d bytes", len(emf), ok, thumb.Format, len(thumb.Data))
	}
}

func TestParsePropertySetDamaged(t *testing.T) {
	t.Parallel()
	good := summary(cfb.CFDIB, []byte("dib"))
	change := func(fn func(b []byte) []byte) []byte {
		return fn(bytes.Clone(good))
	}
	tests := []struct {
		name string
		b    []byte
	}{
		{"short", good[:20]},
		{"byte order", change(func(b []byte) []byte { b[0] = 0; return b })},
		{"sections", change(func(b []byte) []byte { le.PutUint32(b[24:], 1000); return b })},
		{"section offset", change(func(b []byte) []byte { le.PutUint32(b[44:], 1<<30); return b })},
		{"section size", change(func(b []byte) []byte { le.PutUint32(b[48:], 4); return b })},
		{"properties", change(func(b []byte) []byte { le.PutUint32(b[52:], 1<<20); return b })},
	}
	for _, test := range tests {
		if _, err := cfb.ParsePropertySet(test.b); !errors.Is(err, cfb.ErrCorrupt) {
			t.Errorf("%s: expected %v, got: %v", test.name, cfb.ErrCorrupt, err)
		}
	}
	// A section that the stream cuts keeps the properties before the cut.
	ps, err := cfb.ParsePropertySet(good[:len(good)-10])
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if _, ok := ps.Thumbnail(); !ok {
		t.Errorf("expected the thumbnail, got: none")
	}
	if p, ok := ps.Sections[0].Property(0x40); ok {
		t.Errorf("expected no property 0x40, got: %#v", p.Value)
	}
}

func TestCodePageStringText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cp   uint16
		b    []byte
		exp  string
		isOK bool
	}{
		{1252, []byte("na\xefve \x93q\x94\x00junk"), "naïve “q”", true},
		{28591, []byte("\x93\xe9\x00"), "\u0093é", true},
		{65001, []byte("日本\x00"), "日本", true},
		{65001, []byte("\xff\x00"), "\xff", false},
		{1200, []byte{0x2c, 0x67, 0x41, 0, 0, 0, 0x42, 0}, "本A", true},
		{932, []byte("plain\x00"), "plain", true},
		{932, []byte("\x93\xfa\x96\x7b\x00"), "", false},
	}
	for _, test := range tests {
		got, ok := cfb.CodePageString{CodePage: test.cp, Bytes: test.b}.Text()
		if got != test.exp || ok != test.isOK {
			t.Errorf("code page %d: expected %q, %v, got: %q, %v", test.cp, test.exp, test.isOK, got, ok)
		}
	}
}

func FuzzParsePropertySet(f *testing.F) {
	f.Add(summary(cfb.CFMetafilePict, pattern(40, 1)))
	f.Add(docSummary())
	f.Fuzz(func(t *testing.T, b []byte) {
		ps, err := cfb.ParsePropertySet(b)
		if err != nil {
			if !errors.Is(err, cfb.ErrCorrupt) {
				t.Fatalf("expected %v, got: %v", cfb.ErrCorrupt, err)
			}
			return
		}
		for _, s := range ps.Sections {
			for _, p := range s.Properties {
				if v, ok := p.Value.(cfb.CodePageString); ok {
					_, _ = v.Text()
				}
			}
		}
		_, _ = ps.Thumbnail()
	})
}
