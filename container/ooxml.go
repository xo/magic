package container

import (
	"cmp"
	"slices"
	"strings"
)

// mainTypes maps the content type of the main part of an Office Open XML
// package to its format. The content types of Word, Excel and PowerPoint
// are the ones of ECMA-376 Part 1 and of [MS-OFFMACRO2], and the ones of
// Visio are the ones of [MS-VSDX]. libmagic gives one MIME type for each
// program (msooxml of file), so a template, an add-in and a file with macros
// map to the format of the document.
//
// [MS-OFFMACRO2]: https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-offmacro2/
// [MS-VSDX]: https://learn.microsoft.com/en-us/openspecs/sharepoint_protocols/ms-vsdx/
var mainTypes = map[string]Format{
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml": DOCX,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml": DOCX,
	"application/vnd.ms-word.document.macroenabled.main+xml":                           DOCX,
	"application/vnd.ms-word.template.macroenabledtemplate.main+xml":                   DOCX,

	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml":    XLSX,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.template.main+xml": XLSX,
	"application/vnd.ms-excel.sheet.macroenabled.main+xml":                          XLSX,
	"application/vnd.ms-excel.template.macroenabled.main+xml":                       XLSX,
	"application/vnd.ms-excel.addin.macroenabled.main+xml":                          XLSX,
	"application/vnd.ms-excel.sheet.binary.macroenabled.main":                       XLSX,

	"application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml": PPTX,
	"application/vnd.openxmlformats-officedocument.presentationml.slideshow.main+xml":    PPTX,
	"application/vnd.openxmlformats-officedocument.presentationml.template.main+xml":     PPTX,
	"application/vnd.ms-powerpoint.presentation.macroenabled.main+xml":                   PPTX,
	"application/vnd.ms-powerpoint.slideshow.macroenabled.main+xml":                      PPTX,
	"application/vnd.ms-powerpoint.template.macroenabled.main+xml":                       PPTX,
	"application/vnd.ms-powerpoint.addin.macroenabled.main+xml":                          PPTX,

	"application/vnd.ms-visio.drawing.main+xml":               VSDX,
	"application/vnd.ms-visio.template.main+xml":              VSDX,
	"application/vnd.ms-visio.stencil.main+xml":               VSDX,
	"application/vnd.ms-visio.drawing.macroenabled.main+xml":  VSDX,
	"application/vnd.ms-visio.template.macroenabled.main+xml": VSDX,
	"application/vnd.ms-visio.stencil.macroenabled.main+xml":  VSDX,
}

// mainType returns the format of the main part that b, the part
// [Content_Types].xml, gives in an element Override or Default. A content
// type compares without case, as a MIME type does.
func mainType(b []byte) Format {
	f := Unknown
	elements(b, func(name string, attrs []xmlAttr) bool {
		if local := localName(name); local != "Override" && local != "Default" {
			return true
		}
		if v, ok := attr(attrs, "ContentType"); ok {
			f = mainTypes[strings.ToLower(strings.TrimSpace(v))]
		}
		return f == Unknown
	})
	return f
}

// The values that msooxml of file uses to find the folder of a program in
// the name of a member.
var (
	// msooxmlFolders are the folders of the parts of Word, PowerPoint,
	// Excel and Visio.
	msooxmlFolders = []string{"word/", "ppt/", "xl/", "visio/"}
	// msooxmlFirst are the fixed strings of the regular expression that the
	// first member must match. The expression also matches a name with
	// ".md" or ".png", after any text.
	msooxmlFirst = []string{"[Content_Types].xml", "_rels/.rels", "docProps", "customXml"}
)

// The values of the search of msooxml of file for the next local header.
const (
	// msooxmlMembers is the number of members that msooxml reads.
	msooxmlMembers = 6
	// msooxmlSkip is where the next search starts, from the start of the
	// local header that the search before found. The search stops after
	// the signature of 4 bytes. The rule reads the name 26 bytes after the
	// signature, and starts the next search 26 bytes after the start of the
	// name.
	msooxmlSkip = 4 + 26 + 26
	// msooxmlSearch is the bytes that the search reads.
	msooxmlSearch = 10000
	// msooxmlKeep is the number of members of the central directory that
	// zipScan keeps for libmagicOOXML.
	msooxmlKeep = 16
)

// zipHead is one of the first members of the central directory, for
// libmagicOOXML.
type zipHead struct {
	offset uint64 // the offset of the local header, from the base
	folder bool   // the name starts with a folder of msooxmlFolders
	match  int    // the end of the match of msooxmlFirst in the name, or -1
}

// newZipHead returns the zipHead of the member name at offset.
func newZipHead(name string, offset uint64) zipHead {
	h := zipHead{offset: offset, match: -1}
	for _, f := range msooxmlFolders {
		if strings.HasPrefix(name, f) {
			h.folder = true
		}
	}
	// The regular expression of msooxml takes the leftmost match, and the
	// longest of those. ".*\.md" and ".*\.png" match from the start.
	first := -1
	match := func(i, end int) {
		if first < 0 || i < first || i == first && end > h.match {
			first, h.match = i, end
		}
	}
	for _, s := range msooxmlFirst {
		if i := strings.Index(name, s); i >= 0 {
			match(i, i+len(s))
		}
	}
	for _, s := range []string{".md", ".png"} {
		if i := strings.LastIndex(name, s); i >= 0 {
			match(0, i+len(s))
		}
	}
	return h
}

// libmagicOOXML reports whether libmagic names the zip file as Word, Excel,
// PowerPoint or Visio, as msooxml of file does. msooxml reads the name of
// the first member. If the name starts with a folder of msooxmlFolders,
// libmagic names the file. If the name matches msooxmlFirst, msooxml
// searches for the next local header, up to five times, and libmagic names
// the file when one of these members starts with a folder.
//
// The search starts msooxmlSkip bytes after the local header before it, so
// it skips a local header that comes sooner, such as the one after a folder.
// It reads msooxmlSearch bytes. libmagicOOXML takes the local headers from
// the offsets of the central directory. It does not find the signature of a
// local header in the data of a member, which the search of libmagic can
// find.
//
// It returns false when it cannot tell. Detect then names the file. If
// libmagic also names it, both give the same MIME type, and no program is
// left with a file that neither names.
func (z *zipFile) libmagicOOXML(s *zipScan) bool {
	if z.base != 0 || len(s.head) == 0 {
		return false
	}
	h := slices.Clone(s.head)
	slices.SortFunc(h, func(a, b zipHead) int { return cmp.Compare(a.offset, b.offset) })
	switch {
	case h[0].offset != 0:
		return false
	case h[0].folder:
		return true
	case h[0].match < 0:
		return false
	}
	// The first match is in the name, at 30 bytes from the local header.
	start, i := uint64(zipLocalLen+h[0].match+26), 0
	for range msooxmlMembers - 1 {
		i++
		for i < len(h) && h[i].offset < start {
			i++
		}
		if i == len(h) || h[i].offset-start >= msooxmlSearch {
			return false
		}
		if h[i].folder {
			return true
		}
		start = h[i].offset + msooxmlSkip
	}
	return false
}
