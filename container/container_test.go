package container_test

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/xo/magic/container"
)

// samples are the files of the testdata of the root package. TestContainers
// of the root package writes the samples of D64, and the oracle checks what
// libmagic reports for each. err is the error that Detect wraps, or nil.
var samples = []struct {
	file string
	want container.Format
	err  error
}{
	{"xps.xps", container.XPS, nil},
	{"xps-fdseq.xps", container.XPS, nil},
	{"oxps.oxps", container.OpenXPS, nil},
	{"encrypted.docx", container.EncryptedOOXML, nil},
	{"mobi.mobi", container.Unknown, nil},
	{"azw3.azw3", container.AZW3, nil},
	{"cbz.cbz", container.CBZ, nil},
	{"apk.apk", container.APK, nil},
	{"apk-res.apk", container.APK, nil},
	{"ipa.ipa", container.IPA, nil},
	{"kra.kra", container.Krita, nil},
	{"ora.ora", container.OpenRaster, nil},
	// Other containers, which hold none of the formats.
	{"docx.docx", container.Unknown, nil},
	{"xlsx.xlsx", container.Unknown, nil},
	{"jar.jar", container.Unknown, nil},
	// These two samples are the first 4096 bytes of a zip file, and have no
	// central directory.
	{"pptx.pptx", container.Unknown, container.ErrCorrupt},
	{"jar_in_zip.zip", container.Unknown, container.ErrCorrupt},
	{"msi.msi", container.Unknown, nil},
	{"gif.gif", container.Unknown, nil},
	{"utf8.txt", container.Unknown, nil},
	// The Compound Files of testdata are the first 4096 bytes of a file.
	// cfb reads doc.doc, and finds the directory of the others past the
	// cut.
	{"doc.doc", container.Unknown, nil},
	{"xls.xls", container.Unknown, container.ErrCorrupt},
	{"ppt.ppt", container.Unknown, container.ErrCorrupt},
}

// readSample reads the file of the testdata of the root package.
func readSample(t testing.TB, file string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "testdata", file))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	return b
}

// detect calls Detect on b with the default limits.
func detect(b []byte) (container.Format, error) {
	return container.Detect(bytes.NewReader(b), int64(len(b)), container.Limits{})
}

// TestSamples checks the format of each sample.
func TestSamples(t *testing.T) {
	t.Parallel()
	for _, s := range samples {
		t.Run(s.file, func(t *testing.T) {
			t.Parallel()
			got, err := detect(readSample(t, s.file))
			switch {
			case s.err == nil && err != nil:
				t.Fatalf("expected no error, got: %v", err)
			case s.err != nil && !errors.Is(err, s.err):
				t.Fatalf("expected %v, got: %v", s.err, err)
			}
			if got != s.want {
				t.Errorf("expected %q, got: %q", s.want, got)
			}
		})
	}
}

// TestFormats checks that each format has a name, a MIME type and a
// description.
func TestFormats(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for f := container.XPS; f <= container.OpenRaster; f++ {
		if f.String() == "" || strings.HasPrefix(f.String(), "Format(") || f.MIME() == "" || f.Description() == "" {
			t.Errorf("%d: expected a name, a MIME type and a description, got: %q, %q, %q", int(f), f.String(), f.MIME(), f.Description())
		}
		if seen[f.String()] {
			t.Errorf("expected a name of its own, got: %q", f.String())
		}
		seen[f.String()] = true
	}
	if got := container.Unknown.MIME(); got != "" {
		t.Errorf("expected %q, got: %q", "", got)
	}
	if got := container.Format(-1).String(); got != "Format(-1)" {
		t.Errorf("expected %q, got: %q", "Format(-1)", got)
	}
	if got := container.Format(100).MIME(); got != "" {
		t.Errorf("expected %q, got: %q", "", got)
	}
}

// member is a member of a zip file of zipFile.
type member struct {
	name  string
	data  string
	store bool
}

// zipFile makes a zip file of members.
func zipFile(t testing.TB, members ...member) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, m := range members {
		h := &zip.FileHeader{Name: m.name, Method: zip.Deflate}
		if m.store {
			h.Method = zip.Store
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if _, err := f.Write([]byte(m.data)); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	return buf.Bytes()
}

// rels returns the relationships of a package with one relationship of the
// type typ, written with the quote q and the prefix of the elements.
func rels(typ, q, prefix string) string {
	xmlns := "xmlns"
	if prefix != "" {
		xmlns += ":" + strings.TrimSuffix(prefix, ":")
	}
	return `<?xml version="1.0" encoding="utf-8"?><!-- a comment <Relationship Type="x"/> -->` +
		`<` + prefix + `Relationships ` + xmlns + `="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<` + prefix + `Relationship Id=` + q + `R1` + q + ` Target="/docProps/core.xml" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties"/>` +
		`<` + prefix + `Relationship Id=` + q + `R0` + q + "\n  Type = " + q + typ + q + ` Target="/FixedDocumentSequence.fdseq"/>` +
		`</` + prefix + `Relationships>`
}

// utf16LE returns s in UTF-16 with a byte order mark.
func utf16LE(s string) string {
	b := []byte{0xFF, 0xFE}
	for _, c := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	return string(b)
}

const (
	xpsType  = "http://schemas.microsoft.com/xps/2005/06/fixedrepresentation"
	oxpsType = "http://schemas.openxps.org/oxps/v1.0/fixedrepresentation"
)

// TestZip checks the formats of zip files that the samples do not hold.
func TestZip(t *testing.T) {
	t.Parallel()
	const png = "\x89PNG\r\n\x1a\n"
	xps := rels(xpsType, `"`, "")
	half := len(xps) / 2
	tests := []struct {
		name    string
		members []member
		want    container.Format
	}{
		{"empty", nil, container.Unknown},
		{"xps last", []member{
			{name: "FixedDocumentSequence.fdseq", data: "<FixedDocumentSequence/>"},
			{name: "_rels/.rels", data: xps},
		}, container.XPS},
		{"xps stored", []member{{name: "_rels/.rels", data: xps, store: true}}, container.XPS},
		{"xps upper case", []member{{name: "_RELS/.RELS", data: rels(strings.ToUpper(xpsType), `"`, "")}}, container.XPS},
		{"oxps single quotes", []member{{name: "_rels/.rels", data: rels(oxpsType, `'`, "")}}, container.OpenXPS},
		{"oxps prefix", []member{{name: "_rels/.rels", data: rels(oxpsType, `"`, "r:")}}, container.OpenXPS},
		{"oxps utf-16", []member{{name: "_rels/.rels", data: utf16LE(rels(oxpsType, `"`, ""))}}, container.OpenXPS},
		{"xps pieces", []member{
			{name: "_rels/.rels/[1].last.piece", data: xps[half:]},
			{name: "_rels/.rels/[0].piece", data: xps[:half]},
		}, container.XPS},
		{"xps missing piece", []member{{name: "_rels/.rels/[1].last.piece", data: xps}}, container.Unknown},
		{"docx", []member{
			{name: "_rels/.rels", data: rels("http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument", `"`, "")},
			{name: "word/document.xml", data: "<w:document/>"},
		}, container.Unknown},
		{"xps in a comment", []member{{name: "_rels/.rels", data: "<!-- <Relationship Type=\"" + xpsType + "\"/> -->"}}, container.Unknown},
		{"apk manifest last", []member{
			{name: "res/drawable/icon.png", data: png},
			{name: "classes.dex", data: "dex\n035\x00"},
			{name: "AndroidManifest.xml", data: "\x03\x00\x08\x00"},
		}, container.APK},
		{"apk no code", []member{
			{name: "resources.arsc", data: "\x02\x00\x0c\x00"},
			{name: "AndroidManifest.xml", data: "\x03\x00\x08\x00"},
		}, container.APK},
		{"manifest alone", []member{{name: "AndroidManifest.xml", data: "<manifest/>"}}, container.Unknown},
		{"aar", []member{
			{name: "AndroidManifest.xml", data: "<manifest/>"},
			{name: "classes.jar", data: "PK"},
		}, container.Unknown},
		{"ipa no folders", []member{
			{name: "iTunesMetadata.plist", data: "<plist/>"},
			{name: "Payload/My App.app/Info.plist", data: "bplist00"},
		}, container.IPA},
		{"payload not an app", []member{{name: "Payload/notes.txt", data: "x"}}, container.Unknown},
		{"payload .app alone", []member{{name: "Payload/.app/Info.plist", data: "x"}}, container.Unknown},
		{"krita deflated, not first", []member{
			{name: "maindoc.xml", data: "<DOC/>"},
			{name: "mimetype", data: "application/x-krita\n"},
		}, container.Krita},
		{"epub of images", []member{
			{name: "mimetype", data: "application/epub+zip", store: true},
			{name: "images/1.png", data: png},
		}, container.Unknown},
		{"cbz with extras", []member{
			{name: "Comic/", store: true},
			{name: "Comic/ComicInfo.xml", data: "<ComicInfo/>"},
			{name: "Comic/001.JPG", data: "\xFF\xD8\xFF"},
			{name: "Comic/002.webp", data: "RIFF"},
			{name: "Comic/scans.nfo", data: "scanned"},
			{name: "Comic/.DS_Store", data: "x"},
			{name: "__MACOSX/Comic/._001.JPG", data: "x"},
			{name: `Comic\Thumbs.db`, data: "x"},
		}, container.CBZ},
		{"cbz with a pdf", []member{
			{name: "001.png", data: png},
			{name: "readme.pdf", data: "%PDF-"},
		}, container.Unknown},
		{"only text", []member{{name: "readme.txt", data: "x"}}, container.Unknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := detect(zipFile(t, test.members...))
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if got != test.want {
				t.Errorf("expected %q, got: %q", test.want, got)
			}
		})
	}
}

// TestZipStub checks a zip file after other bytes, whose offsets do not
// count the bytes before it.
func TestZipStub(t *testing.T) {
	t.Parallel()
	b := append([]byte("PK\x03\x04"+strings.Repeat("\x00", 100)), readSample(t, "cbz.cbz")...)
	got, err := detect(b)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got != container.CBZ {
		t.Errorf("expected %q, got: %q", container.CBZ, got)
	}
}

// TestZip64 checks a zip file with more members than the end record of zip
// can count, which needs the end record of ZIP64.
func TestZip64(t *testing.T) {
	t.Parallel()
	members := make([]member, 1<<16+10)
	for i := range members {
		members[i] = member{name: fmt.Sprintf("p%d.png", i), store: true}
	}
	b := zipFile(t, members...)
	if !bytes.Contains(b[len(b)-200:], []byte("PK\x06\x06")) {
		t.Fatalf("expected the end record of ZIP64")
	}
	got, err := detect(b)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got != container.CBZ {
		t.Errorf("expected %q, got: %q", container.CBZ, got)
	}
	_, err = container.Detect(bytes.NewReader(b), int64(len(b)), container.Limits{Members: 1 << 16})
	if !errors.Is(err, container.ErrLimit) {
		t.Errorf("expected %v, got: %v", container.ErrLimit, err)
	}
}

// TestLimits checks the limits.
func TestLimits(t *testing.T) {
	t.Parallel()
	b := readSample(t, "apk.apk")
	_, err := container.Detect(bytes.NewReader(b), int64(len(b)), container.Limits{Members: 2})
	if !errors.Is(err, container.ErrLimit) {
		t.Errorf("expected %v, got: %v", container.ErrLimit, err)
	}
	// _rels/.rels is cut before the relationship of XPS.
	b = readSample(t, "xps.xps")
	got, err := container.Detect(bytes.NewReader(b), int64(len(b)), container.Limits{MemberSize: 100})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got != container.Unknown {
		t.Errorf("expected %q, got: %q", container.Unknown, got)
	}
	b = readSample(t, "encrypted.docx")
	_, err = container.Detect(bytes.NewReader(b), int64(len(b)), container.Limits{Members: 2})
	if !errors.Is(err, container.ErrLimit) {
		t.Errorf("expected %v, got: %v", container.ErrLimit, err)
	}
}

// TestCFB checks Compound Files that the samples do not hold.
func TestCFB(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		streams []string
		want    container.Format
	}{
		{"encrypted", []string{"EncryptedPackage", "EncryptionInfo", "\x06DataSpaces"}, container.EncryptedOOXML},
		{"encrypted summary", []string{"WordDocument", "1Table", "EncryptedSummary"}, container.Unknown},
		{"no package", []string{"EncryptionInfo"}, container.Unknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := detect(cfbFile(test.streams...))
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if got != test.want {
				t.Errorf("expected %q, got: %q", test.want, got)
			}
		})
	}
}

// cfbFile makes a Compound File of version 3 with empty streams of the
// names in its root, at most 3. Sector 0 holds the table of sectors, and
// sector 1 the directory. Each stream is on the right of the one before it
// in the tree of the root ([MS-CFB]).
func cfbFile(names ...string) []byte {
	const (
		endOfChain = 0xFFFFFFFE
		freeSect   = 0xFFFFFFFF
		fatSect    = 0xFFFFFFFD
		noStream   = 0xFFFFFFFF
	)
	le := binary.LittleEndian
	b := make([]byte, 3*512)
	h := b[:512]
	copy(h, "\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1")
	le.PutUint16(h[0x18:], 0x3E)
	le.PutUint16(h[0x1A:], 3)
	le.PutUint16(h[0x1C:], 0xFFFE)
	le.PutUint16(h[0x1E:], 9)
	le.PutUint16(h[0x20:], 6)
	le.PutUint32(h[0x2C:], 1)
	le.PutUint32(h[0x30:], 1)
	le.PutUint32(h[0x38:], 4096)
	le.PutUint32(h[0x3C:], endOfChain)
	le.PutUint32(h[0x44:], endOfChain)
	for i := range 109 {
		le.PutUint32(h[0x4C+4*i:], freeSect)
	}
	le.PutUint32(h[0x4C:], 0)
	fat := b[512:1024]
	for i := range 128 {
		le.PutUint32(fat[4*i:], freeSect)
	}
	le.PutUint32(fat[0:], fatSect)
	le.PutUint32(fat[4:], endOfChain)
	dir := b[1024:]
	entry := func(i int, name string, typ byte, right, child uint32) {
		e := dir[128*i : 128*(i+1)]
		u := utf16.Encode([]rune(name))
		for j, c := range u {
			le.PutUint16(e[2*j:], c)
		}
		le.PutUint16(e[0x40:], uint16(2*len(u)+2))
		e[0x42] = typ
		e[0x43] = 1
		le.PutUint32(e[0x44:], noStream)
		le.PutUint32(e[0x48:], right)
		le.PutUint32(e[0x4C:], child)
		le.PutUint32(e[0x74:], endOfChain)
	}
	entry(0, "Root Entry", 5, noStream, 1)
	for i, name := range names {
		right := uint32(i + 2)
		if i == len(names)-1 {
			right = noStream
		}
		entry(i+1, name, 2, right, noStream)
	}
	for i := len(names) + 1; i < 4; i++ {
		e := dir[128*i : 128*(i+1)]
		le.PutUint32(e[0x44:], noStream)
		le.PutUint32(e[0x48:], noStream)
		le.PutUint32(e[0x4C:], noStream)
	}
	return b
}

// TestMOBI checks books that the samples do not hold.
func TestMOBI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		b    []byte
		want container.Format
	}{
		{"joint", mobiFile(6, 121, 2, false), container.AZW3},
		{"joint with boundary", mobiFile(6, 121, 2, true), container.AZW3},
		{"no kf8", mobiFile(6, 121, 0xFFFFFFFF, false), container.Unknown},
		{"kf8 past the end", mobiFile(6, 121, 9, false), container.Unknown},
		{"kf8 is mobi", mobiFile(6, 121, 0, false), container.Unknown},
		{"other exth", mobiFile(6, 100, 2, false), container.Unknown},
		{"version 8", mobiFile(8, 0, 0, false), container.AZW3},
		{"calibre", calibreBook(0x850), container.AZW3},
		{"calibre with no exth flag", calibreBook(0x810), container.Unknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := detect(test.b)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if got != test.want {
				t.Errorf("expected %q, got: %q", test.want, got)
			}
		})
	}
}

// mobiFile makes a MOBI book with a MOBI header of the version in record 0.
// When exth is not 0, record 0 has an EXTH header with one record of the
// type exth and the value kf8. Record 1 is text, and record 2 has a MOBI
// header of version 8. With boundary, record 2 is BOUNDARY, and record 3
// has the MOBI header.
func mobiFile(version, exth, kf8 uint32, boundary bool) []byte {
	be := binary.BigEndian
	header := func(version, exth, kf8 uint32) []byte {
		rec := make([]byte, 16+0xE8)
		copy(rec[16:], "MOBI")
		be.PutUint32(rec[16+0x04:], 0xE8)
		be.PutUint32(rec[16+0x14:], version)
		if exth != 0 {
			be.PutUint32(rec[0x80:], 0x40) // the flags of the EXTH header
			rec = append(rec, "EXTH"...)
			rec = be.AppendUint32(rec, 12+12)
			rec = be.AppendUint32(rec, 1)
			rec = be.AppendUint32(rec, exth)
			rec = be.AppendUint32(rec, 12)
			rec = be.AppendUint32(rec, kf8)
		}
		return rec
	}
	records := [][]byte{header(version, exth, kf8), []byte("text")}
	if boundary {
		records = append(records, []byte("BOUNDARY"))
	}
	records = append(records, header(8, 0, 0))
	b := make([]byte, 78+8*len(records)+2)
	copy(b[60:], "BOOKMOBI")
	be.PutUint16(b[76:], uint16(len(records)))
	for i, rec := range records {
		be.PutUint32(b[78+8*i:], uint32(len(b)))
		b = append(b, rec...)
	}
	return b
}

// calibreBook makes a book with both forms, MOBI and KF8, as calibre writes
// it with --mobi-file-type both. Its records are the ones of such a book,
// mobi/testdata/book-both.mobi of ivy, with fewer bytes. Record 0 has a MOBI
// header of version 6 and 0x108 bytes, with flags, the flags of the EXTH
// header, at 0x80 of the record. calibre writes 0x850. Its EXTH header
// has records of odd lengths, and the record 121 comes late and gives 8,
// the record after BOUNDARY. Record 8 has a MOBI header of version 8.
func calibreBook(flags uint32) []byte {
	be := binary.BigEndian
	const headerLen = 0x108
	exth := func(typ uint32, data []byte) []byte {
		b := be.AppendUint32(nil, typ)
		b = be.AppendUint32(b, uint32(8+len(data)))
		return append(b, data...)
	}
	header := func(version, flags uint32, exths ...[]byte) []byte {
		rec := make([]byte, 16+headerLen)
		be.PutUint16(rec[0:], 2)     // PalmDOC compression
		be.PutUint32(rec[4:], 931)   // the length of the text
		be.PutUint16(rec[8:], 1)     // one text record
		be.PutUint16(rec[10:], 4096) // the size of a text record
		copy(rec[16:], "MOBI")       // the MOBI header
		be.PutUint32(rec[20:], headerLen)
		be.PutUint32(rec[24:], 2)     // a book
		be.PutUint32(rec[28:], 65001) // UTF-8
		be.PutUint32(rec[36:], version)
		be.PutUint32(rec[0x80:], flags)
		var recs []byte
		for _, e := range exths {
			recs = append(recs, e...)
		}
		rec = append(rec, "EXTH"...)
		rec = be.AppendUint32(rec, uint32(12+len(recs)))
		rec = be.AppendUint32(rec, uint32(len(exths)))
		rec = append(rec, recs...)
		// calibre pads the record after the EXTH header.
		return append(rec, make([]byte, 64)...)
	}
	u32 := func(v uint32) []byte { return be.AppendUint32(nil, v) }
	records := [][]byte{
		header(6, flags,
			exth(503, []byte("Sample Book")),
			exth(524, []byte("de")),
			exth(100, []byte("Ann Author")),
			exth(501, []byte("EBOK")),
			exth(129, []byte("kindle:embed:0002")),
			exth(121, u32(8)),
			exth(125, u32(3)),
			exth(131, u32(0)),
		),
		[]byte("<html><body><p>The text of the book.</p></body></html>"),
		{0, 0},
		[]byte("INDX\x00\x00\x00\xc0"),
		[]byte("\xff\xd8\xff\xe0\x00\x10JFIF"),
		[]byte("FLIS\x00\x00\x00\x08"),
		[]byte("FCIS\x00\x00\x00\x14"),
		[]byte("BOUNDARY"),
		header(8, 0x50,
			exth(503, []byte("Sample Book")),
			exth(125, u32(3)),
		),
		[]byte("<?xml version=\"1.0\"?>"),
		[]byte("FDST\x00\x00\x00\x0c"),
		[]byte("\xe9\x8e\r\n"),
	}
	b := make([]byte, 78+8*len(records)+2)
	copy(b, "Sample_Book")
	copy(b[60:], "BOOKMOBI")
	be.PutUint16(b[76:], uint16(len(records)))
	for i, rec := range records {
		be.PutUint32(b[78+8*i:], uint32(len(b)))
		be.PutUint32(b[78+8*i+4:], uint32(2*i))
		b = append(b, rec...)
	}
	return b
}

// errReader fails each read.
type errReader struct{}

var errRead = errors.New("read error")

func (errReader) ReadAt([]byte, int64) (int, error) { return 0, errRead }

// TestReadError checks that Detect returns an error of the reader.
func TestReadError(t *testing.T) {
	t.Parallel()
	_, err := container.Detect(errReader{}, 1000, container.Limits{})
	if !errors.Is(err, errRead) {
		t.Errorf("expected %v, got: %v", errRead, err)
	}
	if got, err := container.Detect(errReader{}, 0, container.Limits{}); err != nil || got != container.Unknown {
		t.Errorf("expected %q and no error, got: %q, %v", container.Unknown, got, err)
	}
}

// damaged returns copies of b that are cut short, and copies with one byte
// changed.
func damaged(b []byte, yield func([]byte)) {
	for n := range len(b) {
		yield(b[:n])
	}
	c := bytes.Clone(b)
	for i := range c {
		for _, x := range []byte{0xFF, 0x01, 0x80} {
			c[i] ^= x
			yield(c)
			c[i] ^= x
		}
	}
}

// TestDamaged checks that Detect returns no error other than ErrCorrupt
// and ErrLimit for damaged copies of the samples and of the files of the
// tests.
func TestDamaged(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{
		"calibre": calibreBook(0x850),
		"cfb":     cfbFile("EncryptionInfo", "EncryptedPackage"),
		"joint":   mobiFile(6, 121, 3, true),
		"xps":     zipFile(t, member{name: "_rels/.rels/[0].piece", data: rels(xpsType, `"`, "")}),
	}
	for _, s := range samples[:12] {
		files[s.file] = readSample(t, s.file)
	}
	for name, b := range files {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			damaged(b, func(c []byte) {
				if _, err := detect(c); err != nil && !errors.Is(err, container.ErrCorrupt) && !errors.Is(err, container.ErrLimit) {
					t.Fatalf("expected %v or %v, got: %v", container.ErrCorrupt, container.ErrLimit, err)
				}
			})
		})
	}
}

// FuzzDetect checks that Detect returns no error other than ErrCorrupt and
// ErrLimit, and does not panic.
func FuzzDetect(f *testing.F) {
	for _, s := range samples[:12] {
		f.Add(readSample(f, s.file))
	}
	f.Add(cfbFile("EncryptionInfo", "EncryptedPackage"))
	f.Add(mobiFile(6, 121, 3, true))
	f.Add(calibreBook(0x850))
	f.Fuzz(func(t *testing.T, b []byte) {
		if _, err := detect(b); err != nil && !errors.Is(err, container.ErrCorrupt) && !errors.Is(err, container.ErrLimit) {
			t.Fatalf("expected %v or %v, got: %v", container.ErrCorrupt, container.ErrLimit, err)
		}
	})
}

// TestZip64Extra checks a member whose sizes and offset are in the extra
// field of ZIP64, as a program writes them for a member past 4 GiB.
func TestZip64Extra(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	const name, data = "mimetype", "image/openraster"
	// A member that the central directory does not name comes first, so
	// that the member is not at offset 0.
	var b []byte
	b = le.AppendUint32(b, 0x04034b50)
	b = append(b, make([]byte, 22)...)
	b = le.AppendUint16(b, uint16(len(name)))
	b = le.AppendUint16(b, 0)
	b = append(b, name...)
	b = append(b, "filler"...)
	off := len(b)
	b = le.AppendUint32(b, 0x04034b50)
	b = append(b, make([]byte, 22)...)
	b = le.AppendUint16(b, uint16(len(name)))
	b = le.AppendUint16(b, 0)
	b = append(b, name...)
	b = append(b, data...)
	cd := len(b)
	b = le.AppendUint32(b, 0x02014b50)
	b = append(b, make([]byte, 16)...) // versions, flags, method, time, CRC
	b = le.AppendUint32(b, 0xFFFFFFFF) // the compressed size
	b = le.AppendUint32(b, 0xFFFFFFFF) // the size
	b = le.AppendUint16(b, uint16(len(name)))
	b = le.AppendUint16(b, 4+8*3+4) // the extra field
	b = le.AppendUint16(b, 0)       // the comment
	b = append(b, make([]byte, 8)...)
	b = le.AppendUint32(b, 0xFFFFFFFF) // the offset
	b = append(b, name...)
	// An extra field of another type, then the one of ZIP64.
	b = le.AppendUint16(b, 0x5455)
	b = le.AppendUint16(b, 0)
	b = le.AppendUint16(b, 0x0001)
	b = le.AppendUint16(b, 8*3)
	b = le.AppendUint64(b, uint64(len(data)))
	b = le.AppendUint64(b, uint64(len(data)))
	b = le.AppendUint64(b, uint64(off))
	end := len(b)
	b = le.AppendUint32(b, 0x06054b50)
	b = append(b, make([]byte, 4)...)
	b = le.AppendUint16(b, 1)
	b = le.AppendUint16(b, 1)
	b = le.AppendUint32(b, uint32(end-cd))
	b = le.AppendUint32(b, uint32(cd))
	b = le.AppendUint16(b, 0)
	got, err := detect(b)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got != container.OpenRaster {
		t.Errorf("expected %q, got: %q", container.OpenRaster, got)
	}
}
