// Package container names the format that a container holds, when libmagic
// names only the container (D64, D65). magic.Detect names an XPS document, a
// comic book archive and most Android packages as a zip file, an encrypted
// Office Open XML file as an encrypted Compound File, and an AZW3 book as a
// Mobipocket book. It names an OpenDocument or Office Open XML file only when
// its members are in the order that libmagic reads, and it does not name the
// files of iWork, Visio 2003 to 2010 or Publisher. Detect reads the container
// and names the format inside.
//
// Detect reads three containers:
//
//   - A zip file: the central directory, and at most one member for each
//     rule, such as `mimetype`, `_rels/.rels` or `[Content_Types].xml`. It
//     finds XPS, OpenXPS, OpenDocument, Office Open XML, iWork, CBZ, APK,
//     IPA, Krita and OpenRaster.
//   - A Compound File: the directory, with the package cfb. It finds an
//     encrypted Office Open XML file, Visio and Publisher.
//   - A Mobipocket book: the first record, and the first record of the KF8
//     part. It finds AZW3.
//
// magic does not import this package, so a program that does not import it
// does not pay for it (D38). Detect does not change the output of
// magic.Detect.
//
// A damaged or hostile file must not crash Detect or make it hang. Detect
// stops at the values of Limits, reads each member through a limit, and
// returns an error that wraps ErrCorrupt or ErrLimit.
package container

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

// The errors of the package. Detect wraps them with the place of the fault.
var (
	// ErrCorrupt reports a container that its specification does not allow,
	// such as a zip file whose central directory ends before its last
	// member.
	ErrCorrupt = errors.New("container: corrupt file")
	// ErrLimit reports a container that is over one of the values of Limits.
	ErrLimit = errors.New("container: file over the limit")
)

// The default values of Limits.
const (
	DefaultMembers    = 1 << 18
	DefaultMemberSize = 1 << 16
)

// Limits holds the limits of Detect. A field of 0 takes its default value.
type Limits struct {
	// Members is the most members that the central directory of a zip file
	// can have, and the most entries that the directory of a Compound File
	// can have. The default is DefaultMembers.
	Members int
	// MemberSize is the most bytes that Detect reads from one member of a
	// zip file, after it decompresses them, and from the first record of a
	// Mobipocket book. Detect reads at most 4 times this number of
	// compressed bytes. The default is DefaultMemberSize, 64 KiB.
	MemberSize int
}

// Format is a format that a container holds.
type Format int

// The formats. Unknown is a file that holds none of them.
const (
	Unknown Format = iota
	// XPS is an XPS document of Microsoft, in a zip file.
	XPS
	// OpenXPS is an OpenXPS document of ECMA-388, in a zip file.
	OpenXPS
	// EncryptedOOXML is an Office Open XML file that a password protects, in
	// a Compound File ([MS-OFFCRYPTO]). Only the key tells whether it is a
	// Word, Excel or PowerPoint file.
	//
	// [MS-OFFCRYPTO]: https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-offcrypto/
	EncryptedOOXML
	// AZW3 is a Kindle book of the format KF8, alone or after a MOBI part.
	AZW3
	// CBZ is a comic book archive: a zip file of images.
	CBZ
	// APK is an Android package.
	APK
	// IPA is an iOS application package.
	IPA
	// Krita is a Krita document.
	Krita
	// OpenRaster is an OpenRaster image.
	OpenRaster

	// The OpenDocument formats, one for each type that libmagic names from
	// the member mimetype. Detect names one only when libmagic does not:
	// when the member mimetype is not the first member, is compressed, or
	// has an extra field, or when the file has no member mimetype and
	// META-INF/manifest.xml gives the type.

	// ODText is an OpenDocument text document (.odt).
	ODText
	// ODTextTemplate is an OpenDocument text template (.ott).
	ODTextTemplate
	// ODTextWeb is an OpenDocument template of an HTML document (.oth).
	ODTextWeb
	// ODTextMaster is an OpenDocument master document (.odm).
	ODTextMaster
	// ODTextMasterTemplate is an OpenDocument master template (.otm).
	ODTextMasterTemplate
	// ODGraphics is an OpenDocument drawing (.odg).
	ODGraphics
	// ODGraphicsTemplate is an OpenDocument drawing template (.otg).
	ODGraphicsTemplate
	// ODPresentation is an OpenDocument presentation (.odp).
	ODPresentation
	// ODPresentationTemplate is an OpenDocument presentation template
	// (.otp).
	ODPresentationTemplate
	// ODSpreadsheet is an OpenDocument spreadsheet (.ods).
	ODSpreadsheet
	// ODSpreadsheetTemplate is an OpenDocument spreadsheet template (.ots).
	ODSpreadsheetTemplate
	// ODChart is an OpenDocument chart (.odc).
	ODChart
	// ODChartTemplate is an OpenDocument chart template (.otc).
	ODChartTemplate
	// ODFormula is an OpenDocument formula (.odf).
	ODFormula
	// ODFormulaTemplate is an OpenDocument formula template (.otf).
	ODFormulaTemplate
	// ODBase is an OpenDocument database (.odb).
	ODBase
	// ODImage is an OpenDocument image (.odi).
	ODImage
	// ODImageTemplate is an OpenDocument image template (.oti).
	ODImageTemplate

	// The Office Open XML formats. libmagic names a Word, Excel, PowerPoint
	// or Visio file by the folder of its parts, and gives one MIME type for
	// each program, so a template or a file with macros has the MIME type of
	// the document. Detect does the same, from the content type of the main
	// part in [Content_Types].xml. It names one only when libmagic does not:
	// when no member in the first members that libmagic reads names the
	// folder.

	// DOCX is a Word document, template or document with macros.
	DOCX
	// XLSX is an Excel workbook, template, add-in or workbook with macros,
	// in XML or in the binary form (.xlsb).
	XLSX
	// PPTX is a PowerPoint presentation, slide show, template, add-in or
	// presentation with macros.
	PPTX
	// VSDX is a Visio 2013 drawing, template or stencil, with or without
	// macros.
	VSDX

	// VSD is a drawing, template or stencil of Visio 2000 to 2010, in a
	// Compound File.
	VSD
	// PUB is a Publisher document, in a Compound File.
	PUB

	// Pages is an Apple Pages document.
	Pages
	// Numbers is an Apple Numbers spreadsheet.
	Numbers
	// Keynote is an Apple Keynote presentation.
	Keynote
)

// formats holds the name, the MIME type and the description of each format.
// A MIME type is the one of shared-mime-info of freedesktop.org. Where
// shared-mime-info has none, it is the one of libmagic.
var formats = [...]struct {
	name, mime, desc string
}{
	Unknown:        {"unknown", "", ""},
	XPS:            {"xps", "application/vnd.ms-xpsdocument", "XPS document"},
	OpenXPS:        {"oxps", "application/oxps", "OpenXPS document"},
	EncryptedOOXML: {"encrypted-ooxml", "application/encrypted", "Microsoft Office Open XML document, encrypted"},
	AZW3:           {"azw3", "application/vnd.amazon.mobi8-ebook", "Kindle book document (KF8)"},
	CBZ:            {"cbz", "application/vnd.comicbook+zip", "Comic book archive (zip container)"},
	APK:            {"apk", "application/vnd.android.package-archive", "Android package"},
	IPA:            {"ipa", "application/x-ios-app", "iOS application package"},
	Krita:          {"krita", "application/x-krita", "Krita document"},
	OpenRaster:     {"openraster", "image/openraster", "OpenRaster image"},

	ODText:                 {"odt", "application/vnd.oasis.opendocument.text", "ODT document"},
	ODTextTemplate:         {"ott", "application/vnd.oasis.opendocument.text-template", "ODT template"},
	ODTextWeb:              {"oth", "application/vnd.oasis.opendocument.text-web", "OTH template"},
	ODTextMaster:           {"odm", "application/vnd.oasis.opendocument.text-master", "ODM document"},
	ODTextMasterTemplate:   {"otm", "application/vnd.oasis.opendocument.text-master-template", "ODM template"},
	ODGraphics:             {"odg", "application/vnd.oasis.opendocument.graphics", "ODG drawing"},
	ODGraphicsTemplate:     {"otg", "application/vnd.oasis.opendocument.graphics-template", "ODG template"},
	ODPresentation:         {"odp", "application/vnd.oasis.opendocument.presentation", "ODP presentation"},
	ODPresentationTemplate: {"otp", "application/vnd.oasis.opendocument.presentation-template", "ODP template"},
	ODSpreadsheet:          {"ods", "application/vnd.oasis.opendocument.spreadsheet", "ODS spreadsheet"},
	ODSpreadsheetTemplate:  {"ots", "application/vnd.oasis.opendocument.spreadsheet-template", "ODS template"},
	ODChart:                {"odc", "application/vnd.oasis.opendocument.chart", "ODC chart"},
	ODChartTemplate:        {"otc", "application/vnd.oasis.opendocument.chart-template", "ODC template"},
	ODFormula:              {"odf", "application/vnd.oasis.opendocument.formula", "ODF formula"},
	ODFormulaTemplate:      {"otf", "application/vnd.oasis.opendocument.formula-template", "ODF template"},
	ODBase:                 {"odb", "application/vnd.oasis.opendocument.base", "ODB database"},
	ODImage:                {"odi", "application/vnd.oasis.opendocument.image", "ODI image"},
	ODImageTemplate:        {"oti", "application/vnd.oasis.opendocument.image-template", "OpenDocument Image Template"},

	DOCX: {"docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "Word 2007 document"},
	XLSX: {"xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "Excel 2007 spreadsheet"},
	PPTX: {"pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", "PowerPoint 2007 presentation"},
	VSDX: {"vsdx", "application/vnd.ms-visio.drawing.main+xml", "Office Open XML Visio drawing"},

	VSD: {"vsd", "application/vnd.visio", "Microsoft Visio document"},
	PUB: {"pub", "application/vnd.ms-publisher", "Microsoft Publisher document"},

	Pages:   {"pages", "application/vnd.apple.pages", "Apple Pages document"},
	Numbers: {"numbers", "application/vnd.apple.numbers", "Apple Numbers spreadsheet"},
	Keynote: {"keynote", "application/vnd.apple.keynote", "Apple Keynote 5 presentation"},
}

// String returns the short name of f, such as "xps" or "azw3".
func (f Format) String() string {
	if f < 0 || int(f) >= len(formats) {
		return fmt.Sprintf("Format(%d)", int(f))
	}
	return formats[f].name
}

// MIME returns the MIME type of f, such as "application/oxps", or "" for
// Unknown.
func (f Format) MIME() string {
	if f < 0 || int(f) >= len(formats) {
		return ""
	}
	return formats[f].mime
}

// Description returns the description of f, such as "OpenXPS document", or
// "" for Unknown.
func (f Format) Description() string {
	if f < 0 || int(f) >= len(formats) {
		return ""
	}
	return formats[f].desc
}

// The signatures of the three containers.
var (
	zipSignature  = []byte("PK\x03\x04")
	cfbSignature  = []byte("\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1")
	mobiSignature = []byte("BOOKMOBI")
)

// Detect reads the zip file, the Compound File or the Mobipocket book of size
// bytes in r, and returns the format that it holds. A program calls it after
// magic.Detect names one of these containers. Detect finds the container by
// its signature, so it also takes any other file.
//
// Detect returns Unknown and no error for a file that is none of the three
// containers, and for a container that holds none of the formats. It also
// returns Unknown for a format that libmagic names, such as a MOBI book with
// no KF8 part, or an OpenDocument file whose first member is mimetype. For a
// damaged
// container, the error wraps ErrCorrupt. For a container over a value of
// lim, it wraps ErrLimit. An error of r is returned as it is, with the
// place of the read.
func Detect(r io.ReaderAt, size int64, lim Limits) (Format, error) {
	if lim.Members <= 0 {
		lim.Members = DefaultMembers
	}
	if lim.MemberSize <= 0 {
		lim.MemberSize = DefaultMemberSize
	}
	if size <= 0 {
		return Unknown, nil
	}
	var head [68]byte
	n, err := r.ReadAt(head[:min(size, int64(len(head)))], 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return Unknown, fmt.Errorf("container: reading the signature: %w", err)
	}
	b := head[:n]
	switch {
	case bytes.HasPrefix(b, zipSignature):
		return detectZip(r, size, lim)
	case bytes.HasPrefix(b, cfbSignature):
		return detectCFB(r, size, lim)
	case len(b) >= 68 && bytes.Equal(b[60:68], mobiSignature):
		return detectMOBI(r, size, lim)
	}
	return Unknown, nil
}

// readFull reads len(p) bytes at off. A read that ends early gives an error
// that wraps ErrCorrupt. Another error of r is wrapped with what.
func readFull(r io.ReaderAt, p []byte, off int64, what string) error {
	n, err := r.ReadAt(p, off)
	switch {
	case n == len(p):
		return nil
	case err == nil || errors.Is(err, io.EOF):
		return fmt.Errorf("%w: %s ends at the end of the file", ErrCorrupt, what)
	}
	return fmt.Errorf("container: reading %s: %w", what, err)
}
