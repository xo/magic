package magic

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"flag"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

// update writes the samples of TestContainers in testdata again.
var update = flag.Bool("update", false, "write the samples of TestContainers in testdata again")

// containers are the samples of formats that live in a container, such as an
// XPS document in a zip file, which ivy tells apart (ivy D8). Each one is the
// smallest file that has the parts that libmagic reads. desc and mime are
// what libmagic reports at the commit of the ledger (D11, D48). The corpus of
// the test module holds the files of testdata, so the gates of the test
// module compare each one with the oracle.
var containers = []struct {
	file string
	make func() []byte
	desc string
	mime string
}{
	// XPS from Office and from the XPS Document Writer: the content types
	// come first, and libmagic finds no OOXML part that it knows.
	{"xps.xps", xpsSample("[Content_Types].xml", xpsNamespace), "Microsoft OOXML", "application/octet-stream"},
	// XPS from other programs: the sequence of documents comes first.
	{"xps-fdseq.xps", xpsSample("FixedDocumentSequence.fdseq", xpsNamespace), "Zip archive data, made by v2.0, extract using at least v2.0, last modified Jan 00 1980 00:00:00, uncompressed size 157, method=deflate", "application/zip"},
	// OpenXPS, ECMA-388, has other namespaces in the same parts.
	{"oxps.oxps", xpsSample("FixedDocumentSequence.fdseq", oxpsNamespace), "Zip archive data, made by v2.0, extract using at least v2.0, last modified Jan 00 1980 00:00:00, uncompressed size 153, method=deflate", "application/zip"},
	// An Office Open XML file that a password protects is a Compound File
	// with the streams EncryptionInfo and EncryptedPackage ([MS-OFFCRYPTO]).
	{"encrypted.docx", encryptedSample, "CDFV2 Encrypted", "application/encrypted"},
	// A MOBI book of Mobipocket and an AZW3 book of Kindle (KF8) differ
	// only in the version of the MOBI header.
	{"mobi.mobi", func() []byte { return mobiSample(6) }, `Mobipocket E-book "Sample Book", 1024 bytes uncompressed, version 6, codepage 65001`, "application/x-mobipocket-ebook"},
	{"azw3.azw3", func() []byte { return mobiSample(8) }, `Mobipocket E-book "Sample Book", 1024 bytes uncompressed, version 8, codepage 65001`, "application/x-mobipocket-ebook"},
	// A comic book archive is a zip file of images.
	{"cbz.cbz", zipSample(
		member{name: "page01.png", data: pngBytes, store: true},
		member{name: "page02.png", data: pngBytes, store: true},
	), "Zip archive data, made by v2.0, extract using at least v2.0, last modified Jan 00 1980 00:00:00, uncompressed size 67, method=store", "application/zip"},
	// libmagic names an APK by its first member, which apksigner and the
	// Android Gradle plugin write first.
	{"apk.apk", zipSample(
		member{name: "AndroidManifest.xml", data: []byte{3, 0, 8, 0, 8, 0, 0, 0}},
		member{name: "classes.dex", data: []byte("dex\n035\x00")},
		member{name: "resources.arsc", data: []byte{2, 0, 12, 0}, store: true},
	), "Android package (APK), with AndroidManifest.xml", "application/vnd.android.package-archive"},
	// Other tools write the resources first, and libmagic sees a zip file.
	{"apk-res.apk", zipSample(
		member{name: "res/layout/main.xml", data: []byte{3, 0, 8, 0, 8, 0, 0, 0}},
		member{name: "AndroidManifest.xml", data: []byte{3, 0, 8, 0, 8, 0, 0, 0}},
		member{name: "classes.dex", data: []byte("dex\n035\x00")},
	), "Zip archive data, made by v2.0, extract using at least v2.0, last modified Jan 00 1980 00:00:00, uncompressed size 8, method=deflate", "application/zip"},
	// An IPA holds the application in Payload/. libmagic prints the name of
	// the third member, and the bytes after it up to a 0.
	{"ipa.ipa", zipSample(
		member{name: "Payload/", store: true},
		member{name: "Payload/Sample.app/", store: true},
		member{name: "Payload/Sample.app/Info.plist", data: []byte("bplist00\x00\x00"), store: true},
	), "IOS/iPadOS IPA file containing Sample.app/Info.plistbplist00 iOS App", "application/x-ios-app"},
	// Krita and OpenRaster start with the member mimetype, as OpenDocument
	// does. libmagic prints the type, and gives the MIME type of zip.
	{"kra.kra", zipSample(
		member{name: "mimetype", data: []byte("application/x-krita"), store: true},
		member{name: "maindoc.xml", data: []byte("<DOC/>")},
		member{name: "mergedimage.png", data: pngBytes, store: true},
	), `Zip data (MIME type "application/x-krita"?)`, "application/zip"},
	{"ora.ora", zipSample(
		member{name: "mimetype", data: []byte("image/openraster"), store: true},
		member{name: "stack.xml", data: []byte("<image/>")},
		member{name: "mergedimage.png", data: pngBytes, store: true},
	), `Zip data (MIME type "image/openraster"?)`, "application/zip"},
}

// TestContainers checks what the package reports for each sample of
// containers. With -update, it writes the samples again.
func TestContainers(t *testing.T) {
	t.Parallel()
	for _, c := range containers {
		path := filepath.Join("testdata", c.file)
		if *update {
			if err := os.WriteFile(path, c.make(), 0o644); err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !bytes.Equal(b, c.make()) {
			t.Errorf("%s: expected the sample that the test makes. Run the test with -update", c.file)
		}
		r, err := Detect(context.Background(), b)
		if err != nil {
			t.Fatalf("%s: expected no error, got: %v", c.file, err)
		}
		if r.Description != c.desc {
			t.Errorf("%s: expected %q, got: %q", c.file, c.desc, r.Description)
		}
		if r.MIME != c.mime {
			t.Errorf("%s: expected %q, got: %q", c.file, c.mime, r.MIME)
		}
	}
}

// pngBytes is a PNG image of 1 by 1 pixel.
var pngBytes = []byte("\x89PNG\r\n\x1a\n" +
	"\x00\x00\x00\x0dIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x00\x00\x00\x00\x3a\x7e\x9b\x55" +
	"\x00\x00\x00\x0aIDAT\x78\x9c\x63\x60\x00\x00\x00\x02\x00\x01\xe5\x27\xde\xfc" +
	"\x00\x00\x00\x00IEND\xae\x42\x60\x82")

// member is one member of a zip file of zipSample.
type member struct {
	name  string
	data  []byte
	store bool
}

// zipSample returns a function that makes a zip file of members, as most
// programs write it: the sizes and the CRC are in each local header, and no
// header has an extra field. A member has no time, which libmagic prints as
// "Jan 00 1980".
func zipSample(members ...member) func() []byte {
	return func() []byte {
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)
		for _, m := range members {
			data, method := m.data, zip.Deflate
			if m.store {
				method = zip.Store
			} else {
				var c bytes.Buffer
				fw, err := flate.NewWriter(&c, flate.BestCompression)
				if err != nil {
					panic(err)
				}
				if _, err := fw.Write(m.data); err != nil {
					panic(err)
				}
				if err := fw.Close(); err != nil {
					panic(err)
				}
				data = c.Bytes()
			}
			// Version 2.0 of the format, on MS-DOS, as most programs write.
			h := &zip.FileHeader{
				Name:               m.name,
				Method:             method,
				CreatorVersion:     20,
				ReaderVersion:      20,
				CRC32:              crc32.ChecksumIEEE(m.data),
				CompressedSize64:   uint64(len(data)),
				UncompressedSize64: uint64(len(m.data)),
			}
			f, err := w.CreateRaw(h)
			if err != nil {
				panic(err)
			}
			if _, err := f.Write(data); err != nil {
				panic(err)
			}
		}
		if err := w.Close(); err != nil {
			panic(err)
		}
		return buf.Bytes()
	}
}

// The namespaces of the documents of XPS and of OpenXPS.
const (
	xpsNamespace  = "http://schemas.microsoft.com/xps/2005/06"
	oxpsNamespace = "http://schemas.openxps.org/oxps/v1.0"
)

// xpsSample returns a function that makes an XPS document of one page, with
// the member first at its start.
func xpsSample(first, ns string) func() []byte {
	parts := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="utf-8"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="fdseq" ContentType="application/vnd.ms-package.xps-fixeddocumentsequence+xml"/>` +
			`<Default Extension="fdoc" ContentType="application/vnd.ms-package.xps-fixeddocument+xml"/>` +
			`<Default Extension="fpage" ContentType="application/vnd.ms-package.xps-fixedpage+xml"/></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="utf-8"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="R0" Type="` + ns + `/fixedrepresentation" Target="/FixedDocumentSequence.fdseq"/>` +
			`</Relationships>`,
		"FixedDocumentSequence.fdseq": `<FixedDocumentSequence xmlns="` + ns + `">` +
			`<DocumentReference Source="/Documents/1/FixedDocument.fdoc"/></FixedDocumentSequence>`,
		"Documents/1/FixedDocument.fdoc": `<FixedDocument xmlns="` + ns + `">` +
			`<PageContent Source="/Documents/1/Pages/1.fpage"/></FixedDocument>`,
		"Documents/1/Pages/1.fpage": `<FixedPage xmlns="` + ns + `" Width="816" Height="1056" xml:lang="en-US">` +
			`<Path Data="M 96,96 L 720,96 720,960 96,960 Z" Fill="#FF336699"/></FixedPage>`,
	}
	order := []string{first}
	for _, name := range []string{
		"[Content_Types].xml", "_rels/.rels", "FixedDocumentSequence.fdseq",
		"Documents/1/FixedDocument.fdoc", "Documents/1/Pages/1.fpage",
	} {
		if name != first {
			order = append(order, name)
		}
	}
	members := make([]member, 0, len(order))
	for _, name := range order {
		members = append(members, member{name: name, data: []byte(parts[name])})
	}
	return zipSample(members...)
}

// encryptedSample makes a Compound File of version 3 that holds the two
// streams of an encrypted Office Open XML file, with no data. libmagic reads
// only the directory. Sector 0 holds the table of sectors, and sector 1 the
// directory ([MS-CFB]).
func encryptedSample() []byte {
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
	le.PutUint16(h[0x18:], 0x3E)   // the minor version
	le.PutUint16(h[0x1A:], 3)      // the major version
	le.PutUint16(h[0x1C:], 0xFFFE) // the order of the bytes
	le.PutUint16(h[0x1E:], 9)      // sectors of 512 bytes
	le.PutUint16(h[0x20:], 6)      // mini sectors of 64 bytes
	le.PutUint32(h[0x2C:], 1)      // one sector of the table
	le.PutUint32(h[0x30:], 1)      // the directory
	le.PutUint32(h[0x38:], 4096)   // the cutoff of the mini stream
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
	entry := func(i int, name string, typ byte, left, right, child uint32) {
		e := dir[128*i : 128*(i+1)]
		u := utf16.Encode([]rune(name))
		for j, c := range u {
			le.PutUint16(e[2*j:], c)
		}
		le.PutUint16(e[0x40:], uint16(2*len(u)+2))
		e[0x42] = typ
		e[0x43] = 1 // black
		le.PutUint32(e[0x44:], left)
		le.PutUint32(e[0x48:], right)
		le.PutUint32(e[0x4C:], child)
		le.PutUint32(e[0x74:], endOfChain)
	}
	// A shorter name sorts first, so EncryptionInfo is the root of the tree
	// of the root storage, and EncryptedPackage is on its right.
	entry(0, "Root Entry", 5, noStream, noStream, 1)
	entry(1, "EncryptionInfo", 2, noStream, 2, noStream)
	entry(2, "EncryptedPackage", 2, noStream, noStream, noStream)
	for i := 3; i < 4; i++ {
		e := dir[128*i : 128*(i+1)]
		le.PutUint32(e[0x44:], noStream)
		le.PutUint32(e[0x48:], noStream)
		le.PutUint32(e[0x4C:], noStream)
	}
	return b
}

// mobiSample makes a MOBI book of one text record, with a MOBI header of the
// version. Its first record holds the header of PalmDOC, the MOBI header and
// the full name. Every number is big-endian.
func mobiSample(version uint32) []byte {
	be := binary.BigEndian
	const title = "Sample Book"
	// The PalmDB header, and a list of two records.
	db := make([]byte, 78+2*8+2, 2048)
	copy(db, "Sample_Book")
	copy(db[60:], "BOOKMOBI")
	be.PutUint16(db[76:], 2)
	rec0 := make([]byte, 16+0xE8, 16+0xE8+len(title)+2)
	be.PutUint16(rec0[0:], 2)    // PalmDOC, which leaves ASCII as it is
	be.PutUint32(rec0[4:], 1024) // the length of the text
	be.PutUint16(rec0[8:], 1)    // one text record
	be.PutUint16(rec0[10:], 4096)
	m := rec0[16:]
	copy(m, "MOBI")
	be.PutUint32(m[0x04:], 0xE8)  // the length of the MOBI header
	be.PutUint32(m[0x08:], 2)     // a book
	be.PutUint32(m[0x0C:], 65001) // UTF-8
	be.PutUint32(m[0x14:], version)
	be.PutUint32(m[0x44:], uint32(len(rec0))) // the full name follows the header
	be.PutUint32(m[0x48:], uint32(len(title)))
	be.PutUint32(m[0x58:], version) // the version that a reader needs
	rec0 = append(rec0, title...)
	rec0 = append(rec0, 0, 0)
	text := bytes.Repeat([]byte("A line of the text of the sample book.\n"), 27)[:1024]
	be.PutUint32(db[78:], uint32(len(db)))
	be.PutUint32(db[86:], uint32(len(db)+len(rec0)))
	be.PutUint32(db[86+4:], 1)
	db = append(db, rec0...)
	return append(db, text...)
}
