package container_test

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/xo/magic"
	"github.com/xo/magic/container"
)

// The content types of the main parts of Office Open XML that the tests
// write.
const (
	wordType  = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
	excelType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"
	pptType   = "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"
	visioType = "application/vnd.ms-visio.drawing.main+xml"
)

// contentTypes returns the part [Content_Types].xml of a package whose main
// part is main, of the content type typ.
func contentTypes(main, typ string) member {
	return member{name: "[Content_Types].xml", data: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
		`<Override PartName="/` + main + `" ContentType="` + typ + `"/>` +
		`</Types>`}
}

// officeRels is the part _rels/.rels of a package of Office.
var officeRels = member{name: "_rels/.rels", data: rels("http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument", `"`, "")}

// part is a part of a package with some XML.
func part(name string) member {
	return member{name: name, data: "<x/>"}
}

// folder is the member of a folder.
func folder(name string) member {
	return member{name: name, store: true}
}

// odfMimetype is the member mimetype of an OpenDocument file of the type
// typ, stored as OpenDocument says.
func odfMimetype(typ string) member {
	return member{name: "mimetype", data: typ, store: true}
}

// odfManifest is the member META-INF/manifest.xml of an OpenDocument file of
// the type typ.
func odfManifest(typ string) member {
	return member{name: "META-INF/manifest.xml", data: `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.2">` + "\n" +
		` <manifest:file-entry manifest:full-path="Pictures/1.png" manifest:media-type="image/png"/>` + "\n" +
		` <manifest:file-entry manifest:media-type="` + typ + `" manifest:full-path="/" manifest:version="1.2"/>` + "\n" +
		` <manifest:file-entry manifest:full-path="content.xml" manifest:media-type="text/xml"/>` + "\n" +
		`</manifest:manifest>`}
}

// extendedTime is an extra field of the type 0x5455, the time of a member,
// as Info-ZIP writes it.
var extendedTime = []byte{0x55, 0x54, 5, 0, 1, 0x80, 0x1A, 0x06, 0x5F}

const odtType = "application/vnd.oasis.opendocument.text"

// officeLayouts are zip files of OpenDocument and Office Open XML, with
// their members in many orders. libmagic names some of them, and container
// names the others.
var officeLayouts = []struct {
	name    string
	members []member
	format  container.Format
}{
	{"odt", []member{odfMimetype(odtType), part("content.xml"), odfManifest(odtType)}, container.ODText},
	{"odt mimetype deflated", []member{{name: "mimetype", data: odtType}, part("content.xml")}, container.ODText},
	{"odt mimetype last", []member{folder("Versions/"), part("Versions/Version1"), part("content.xml"), odfManifest(odtType), odfMimetype(odtType)}, container.ODText},
	{"odg mimetype with extra field", []member{
		{name: "mimetype", data: "application/vnd.oasis.opendocument.graphics", store: true, extra: extendedTime},
		part("content.xml"),
	}, container.ODGraphics},
	{"ods no mimetype", []member{part("content.xml"), odfManifest("application/vnd.oasis.opendocument.spreadsheet")}, container.ODSpreadsheet},
	{"otp mimetype last", []member{part("content.xml"), odfMimetype("application/vnd.oasis.opendocument.presentation-template")}, container.ODPresentationTemplate},
	{"odb database", []member{part("content.xml"), odfMimetype("application/vnd.oasis.opendocument.database")}, container.ODBase},

	{"docx", []member{contentTypes("word/document.xml", wordType), officeRels, part("word/document.xml")}, container.DOCX},
	{"docx rels first", []member{officeRels, part("word/document.xml"), contentTypes("word/document.xml", wordType)}, container.DOCX},
	{"docx part first", []member{part("word/document.xml"), contentTypes("word/document.xml", wordType)}, container.DOCX},
	{"docx styles first", []member{part("styles.xml"), contentTypes("word/document.xml", wordType), part("word/document.xml")}, container.DOCX},
	{"docx customXml first", []member{
		folder("customXml/"), part("customXml/item1.xml"), part("customXml/item2.xml"), part("customXml/itemProps1.xml"),
		part("customXml/itemProps2.xml"), folder("customXml/_rels/"), part("customXml/_rels/item1.xml.rels"),
		contentTypes("word/document.xml", wordType), part("word/document.xml"),
	}, container.DOCX},
	{"docx folder skipped", []member{
		contentTypes("word/document.xml", wordType), folder("_rels/"), officeRels, part("word/document.xml"),
	}, container.DOCX},
	{"docx after a folder", []member{
		contentTypes("word/document.xml", wordType), officeRels, folder("x/"), part("word/document.xml"),
	}, container.DOCX},
	{"docx sixth", []member{
		contentTypes("word/document.xml", wordType), part("docProps/a.xml"), part("docProps/b.xml"),
		part("docProps/c.xml"), part("docProps/d.xml"), part("word/document.xml"),
	}, container.DOCX},
	{"docx seventh", []member{
		contentTypes("word/document.xml", wordType), part("docProps/a.xml"), part("docProps/b.xml"),
		part("docProps/c.xml"), part("docProps/d.xml"), part("docProps/e.xml"), part("word/document.xml"),
	}, container.DOCX},
	{"docx after a thumbnail", []member{
		contentTypes("word/document.xml", wordType), {name: "docProps/thumbnail.jpeg", data: strings.Repeat("\xFF", 20000), store: true},
		part("word/document.xml"),
	}, container.DOCX},
	{"docx docProps folder first", []member{folder("docProps/"), part("word/document.xml"), contentTypes("word/document.xml", wordType)}, container.DOCX},
	{"docm", []member{
		part("docProps/app.xml"), part("docProps/core.xml"), part("docProps/custom.xml"), part("docProps/x.xml"),
		part("docProps/y.xml"), part("docProps/z.xml"),
		contentTypes("word/document.xml", "application/vnd.ms-word.document.macroEnabled.main+xml"), part("word/document.xml"),
	}, container.DOCX},
	{"xlsx", []member{contentTypes("xl/workbook.xml", excelType), part("xl/workbook.xml")}, container.XLSX},
	{"xlsx late", []member{part("styles.xml"), contentTypes("xl/workbook.xml", excelType), part("xl/workbook.xml")}, container.XLSX},
	{"xlsb late", []member{part("a.xml"), contentTypes("xl/workbook.bin", "application/vnd.ms-excel.sheet.binary.macroEnabled.main"), part("xl/workbook.bin")}, container.XLSX},
	{"pptx", []member{contentTypes("ppt/presentation.xml", pptType), part("ppt/presentation.xml")}, container.PPTX},
	{"pptx late", []member{part("a.xml"), contentTypes("ppt/presentation.xml", pptType), part("ppt/presentation.xml")}, container.PPTX},
	{"vsdx", []member{contentTypes("visio/document.xml", visioType), part("visio/document.xml")}, container.VSDX},
	{"vsdx late", []member{part("a.xml"), contentTypes("visio/document.xml", visioType), part("visio/document.xml")}, container.VSDX},
}

// TestOffice checks that container names an OpenDocument or Office Open XML
// file when libmagic does not, and only then. The port gives the output of
// libmagic (D48), so it tells what libmagic names.
func TestOffice(t *testing.T) {
	t.Parallel()
	for _, test := range officeLayouts {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b := zipFile(t, test.members...)
			r, err := magic.Detect(context.Background(), b)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			want := test.format
			if r.MIME == test.format.MIME() {
				want = container.Unknown
			}
			got, err := detect(b)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if got != want {
				t.Errorf("expected %q, got: %q", want, got)
			}
		})
	}
}

// TestOfficeOrder checks the layouts of TestOffice that each name in its
// own way, so that TestOffice does not pass with no layout that container
// names.
func TestOfficeOrder(t *testing.T) {
	t.Parallel()
	tests := map[string]container.Format{
		"odt":                           container.Unknown,
		"odt mimetype deflated":         container.ODText,
		"odt mimetype last":             container.ODText,
		"odg mimetype with extra field": container.ODGraphics,
		"ods no mimetype":               container.ODSpreadsheet,
		"docx":                          container.Unknown,
		"docx rels first":               container.Unknown,
		"docx part first":               container.Unknown,
		"docx styles first":             container.DOCX,
		"docx customXml first":          container.DOCX,
		"docx folder skipped":           container.Unknown,
		"docx after a folder":           container.DOCX,
		"docx sixth":                    container.Unknown,
		"docx seventh":                  container.DOCX,
		"docx after a thumbnail":        container.DOCX,
		"docm":                          container.DOCX,
		"vsdx":                          container.Unknown,
		"vsdx late":                     container.VSDX,
	}
	for _, test := range officeLayouts {
		want, ok := tests[test.name]
		if !ok {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := detect(zipFile(t, test.members...))
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if got != want {
				t.Errorf("expected %q, got: %q", want, got)
			}
		})
	}
}

// TestOfficeOther checks zip files that hold no format of Office.
func TestOfficeOther(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		members []member
	}{
		{"unknown odf type", []member{part("content.xml"), odfMimetype("application/vnd.oasis.opendocument.other")}},
		{"epub mimetype last", []member{part("OEBPS/content.opf"), odfMimetype("application/epub+zip")}},
		{"manifest of no root", []member{part("content.xml"), {name: "META-INF/manifest.xml", data: `<manifest:file-entry manifest:full-path="a" manifest:media-type="` + odtType + `"/>`}}},
		{"manifest in a comment", []member{part("content.xml"), {name: "META-INF/manifest.xml", data: `<!-- <manifest:file-entry manifest:full-path="/" manifest:media-type="` + odtType + `"/> -->`}}},
		{"no main part", []member{part("a.xml"), contentTypes("docProps/core.xml", "application/xml")}},
		{"xps content types", []member{part("a.xml"), contentTypes("FixedDocSeq.fdseq", "application/vnd.ms-package.xps-fixeddocumentsequence+xml")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := detect(zipFile(t, test.members...))
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if got != container.Unknown {
				t.Errorf("expected %q, got: %q", container.Unknown, got)
			}
		})
	}
}

// TestXPSOrder checks XPS documents with [Content_Types].xml first, as the
// XPS Document Writer writes them, and later (item 35 of docs/BACKLOG.md).
// libmagic names the first "Microsoft OOXML" and the second a zip file.
func TestXPSOrder(t *testing.T) {
	t.Parallel()
	xps := member{name: "_rels/.rels", data: rels(xpsType, `"`, "")}
	types := contentTypes("FixedDocSeq.fdseq", "application/vnd.ms-package.xps-fixeddocumentsequence+xml")
	tests := map[string][]member{
		"types first": {types, xps, {name: "docProps/thumbnail.jpeg", data: "\xFF\xD8\xFF", store: true}, part("FixedDocSeq.fdseq")},
		"types later": {part("FixedDocSeq.fdseq"), part("Documents/1/FixedDoc.fdoc"), xps, types},
	}
	for name, members := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := detect(zipFile(t, members...))
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if got != container.XPS {
				t.Errorf("expected %q, got: %q", container.XPS, got)
			}
		})
	}
}

// iwa returns the start of an IWA file whose first object has the type typ:
// a chunk of Snappy data with one literal, which holds the length of the
// ArchiveInfo, the ArchiveInfo and the object.
func iwa(typ uint64) string {
	msg := []byte{0x08}
	msg = binary.AppendUvarint(msg, typ)
	msg = append(msg, 0x12, 0x03, 0x01, 0x00, 0x05, 0x18, 0x02) // the version and the length
	info := append([]byte{0x08, 0x01, 0x12}, byte(len(msg)))
	info = append(info, msg...)
	lit := append([]byte{byte(len(info))}, info...)
	lit = append(lit, 0x0A, 0x00)
	data := binary.AppendUvarint(nil, uint64(len(lit)))
	data = append(data, 60<<2, byte(len(lit)-1)) // a literal with a length of 1 byte
	data = append(data, lit...)
	b := make([]byte, 0, 4+len(data))
	b = append(b, 0, byte(len(data)), 0, 0)
	return string(append(b, data...))
}

// iworkIndex returns an index of iWork 2005 to 2009 whose root element is
// prefix:local, in the namespace ns.
func iworkIndex(name, prefix, local, ns string) member {
	return member{name: name, data: `<?xml version="1.0"?>` + "\n" +
		`<!-- a comment -->` + "\n" +
		`<` + prefix + `:` + local + ` xmlns:sfa="http://developer.apple.com/namespaces/sfa" xmlns:` + prefix + `="` + ns + `" sfa:ID="SL-0">` +
		`<` + prefix + `:body/></` + prefix + `:` + local + `>`}
}

// TestIWork checks the files of iWork. The members are the ones of the
// samples of Apache Tika, with fewer of them.
func TestIWork(t *testing.T) {
	t.Parallel()
	meta := []member{
		{name: "Metadata/BuildVersionHistory.plist", data: "<plist/>"},
		{name: "preview.jpg", data: "\xFF\xD8\xFF", store: true},
		{name: "Metadata/Properties.plist", data: "bplist00", store: true},
		{name: "Metadata/DocumentIdentifier", data: "9A256E95", store: true},
	}
	with := func(members ...member) []member {
		return append(members, meta...)
	}
	const (
		sl  = "http://developer.apple.com/namespaces/sl"
		ls  = "http://developer.apple.com/namespaces/ls"
		key = "http://developer.apple.com/namespaces/keynote2"
	)
	tests := []struct {
		name    string
		members []member
		want    container.Format
	}{
		{"pages", with(
			part("Index/DocumentStylesheet.iwa"), part("Index/Tables/DataList.iwa"),
			member{name: "Index/Document.iwa", data: iwa(10000), store: true}, part("Index/ViewState.iwa"),
		), container.Pages},
		{"numbers", with(
			part("Index/Tables/DataList.iwa"), member{name: "Index/Document.iwa", data: iwa(1), store: true},
			part("Index/CalculationEngine.iwa"), part("Index/ObjectContainer.iwa"),
		), container.Numbers},
		{"keynote", with(
			part("Index/MasterSlide-10.iwa"), part("Index/Tables/DataList.iwa"),
			member{name: "Index/Document.iwa", data: iwa(1), store: true}, part("Index/Slide-2.iwa"),
		), container.Keynote},
		{"keynote slide", with(member{name: "Index/Document.iwa", data: iwa(1), store: true}, part("Index/Slide.iwa")), container.Keynote},
		{"deflated", with(member{name: "Index/Document.iwa", data: iwa(10000)}), container.Pages},
		{"other type", with(member{name: "Index/Document.iwa", data: iwa(2), store: true}), container.Unknown},
		{"not iwa", with(member{name: "Index/Document.iwa", data: "\x01\x02\x03\x04\x05", store: true}), container.Unknown},
		{"slides alone", with(part("Index/Slide-1.iwa"), part("Index/MasterSlide.iwa")), container.Unknown},
		{"other slide", with(member{name: "Index/Document.iwa", data: iwa(1), store: true}, part("Index/Slides.iwa")), container.Numbers},
		{"package", []member{
			folder("Presentation.key/"), folder("Presentation.key/Data/"), folder("Presentation.key/Metadata/"),
			{name: "Presentation.key/Index.zip", data: "PK\x03\x04"},
			{name: "Presentation.key/Metadata/BuildVersionHistory.plist", data: "<plist/>"},
		}, container.Keynote},
		{"package of pages", []member{{name: "Report.PAGES/Index.zip", data: "PK"}, {name: "Report.PAGES/preview.jpg", data: "\xFF\xD8\xFF"}}, container.Pages},
		{"package of numbers 2009", []member{{name: "Budget.numbers/index.xml", data: "<x/>"}}, container.Numbers},
		{"package with no name", []member{{name: ".key/Index.zip", data: "PK"}}, container.Unknown},
		{"package too deep", []member{{name: "a/Talk.key/Index.zip", data: "PK"}}, container.Unknown},
		{"pages 2009", []member{iworkIndex("index.xml", "sl", "document", sl), {name: "QuickLook/Thumbnail.jpg", data: "\xFF\xD8\xFF"}}, container.Pages},
		{"numbers 2009", []member{iworkIndex("index.xml", "ls", "document", ls)}, container.Numbers},
		{"keynote 2009", []member{iworkIndex("index.apxl", "key", "presentation", key)}, container.Keynote},
		{"keynote presentation.apxl", []member{iworkIndex("presentation.apxl", "key", "presentation", key)}, container.Keynote},
		{"index of another namespace", []member{iworkIndex("index.xml", "sl", "document", "http://example.com/sl")}, container.Unknown},
		{"index with no prefix", []member{{name: "index.xml", data: `<document xmlns="` + sl + `"/>`}}, container.Pages},
		{"index with the wrong root", []member{iworkIndex("index.xml", "sl", "presentation", sl)}, container.Unknown},
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

// The class IDs of the root of Visio and Publisher, as a Compound File
// holds them.
var (
	visio2003 = [16]byte{0x14, 0x1A, 0x02, 0x00, 0, 0, 0, 0, 0xC0, 0, 0, 0, 0, 0, 0, 0x46}
	visio2000 = [16]byte{0x13, 0x1A, 0x02, 0x00, 0, 0, 0, 0, 0xC0, 0, 0, 0, 0, 0, 0, 0x46}
	pub97     = [16]byte{0x01, 0x12, 0x02, 0x00, 0, 0, 0, 0, 0, 0xC0, 0, 0, 0, 0, 0, 0x46}
	pub95     = [16]byte{0x00, 0x12, 0x02, 0x00, 0, 0, 0, 0, 0, 0xC0, 0, 0, 0, 0, 0, 0x46}
)

// TestVisioPublisher checks the Compound Files of Visio and Publisher. The
// entries are the ones of the files of Apache POI.
func TestVisioPublisher(t *testing.T) {
	t.Parallel()
	visio := []cfbEntry{{name: "VisioDocument"}, {name: "VisioInformation"}, {name: "\x05SummaryInformation"}}
	pub := []cfbEntry{{name: "Quill", storage: true}, {name: "Contents"}, {name: "Envelope"}}
	tests := []struct {
		name    string
		clsid   [16]byte
		entries []cfbEntry
		want    container.Format
	}{
		{"visio 2003", visio2003, visio, container.VSD},
		{"visio 2000", visio2000, visio, container.VSD},
		{"visio class alone", visio2003, []cfbEntry{{name: "x"}}, container.VSD},
		{"visio stream alone", [16]byte{}, visio, container.VSD},
		{"publisher 97", pub97, pub, container.PUB},
		{"publisher 95", pub95, pub, container.PUB},
		{"publisher class alone", pub97, []cfbEntry{{name: "x"}}, container.PUB},
		{"publisher streams alone", [16]byte{}, pub, container.PUB},
		{"contents alone", [16]byte{}, []cfbEntry{{name: "Contents"}, {name: "SesnDescriptor"}}, container.Unknown},
		{"quill stream", [16]byte{}, []cfbEntry{{name: "Quill"}, {name: "Contents"}}, container.Unknown},
		{"visio storage", [16]byte{}, []cfbEntry{{name: "VisioDocument", storage: true}}, container.Unknown},
		{"publisher number with the last part of visio", [16]byte{0x01, 0x12, 0x02, 0x00, 0, 0, 0, 0, 0xC0, 0, 0, 0, 0, 0, 0, 0x46}, []cfbEntry{{name: "x"}}, container.Unknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := detect(cfbFileOf(test.clsid, test.entries...))
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if got != test.want {
				t.Errorf("expected %q, got: %q", test.want, got)
			}
		})
	}
}
