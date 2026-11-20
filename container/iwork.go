package container

import (
	"encoding/binary"
	"path"
	"strings"
)

// The facts that tell the files of iWork apart. libmagic names none of them.
//
// A file of iWork 2013 and later is a zip file of the folders Index/, Data/
// and Metadata/. Index/ holds the objects of the document, in IWA files,
// which iwaType reads. Pages and Numbers have members of the same names, and
// only Keynote has slides. Apache Tika
// (IWork13PackageParser.java) names a file Keynote when it holds
// Index/Slide.iwa, Index/MasterSlide.iwa, or such a name with "-" and a
// number. The first object of Index/Document.iwa tells Pages from Numbers:
// in the samples of Tika, testPages2013.pages, testNumbers2013.numbers and
// testKeynote2013.key, its type is 10000 in Pages, and 1 in Numbers and in
// Keynote. A comment of Tika names the type 10000 TP::DocumentArchive, the
// document of Pages.
//
// iWork can also save a document as a package: a folder with the extension
// .pages, .numbers or .key, that holds Index.zip, Data/ and Metadata/. macOS
// finds the format of a package by its extension. Detect names a zip file
// of such a folder, such as testKeynote2018.key of Tika, by the extension of
// the folder, and does not read Index.zip, which is compressed in the zip
// file.
//
// A file of iWork 2005 to 2009 holds index.xml, or index.apxl for Keynote.
// Tika (IWorkPackageParser.java) also reads presentation.apxl, and tells the
// formats apart by the root element: "presentation" in the namespace
// iworkKeynote, or "document" in the namespace iworkNumbers or iworkPages.
const (
	iworkKeynote = "http://developer.apple.com/namespaces/keynote2"
	iworkNumbers = "http://developer.apple.com/namespaces/ls"
	iworkPages   = "http://developer.apple.com/namespaces/sl"

	// The types of the first object of Index/Document.iwa.
	iwaPages   = 10000
	iwaNumbers = 1

	// iwaHead is the bytes of Index/Document.iwa that Detect reads. The
	// first object starts in the first bytes.
	iwaHead = 1 << 10
)

// iworkFolders maps the extension of the folder of a package of iWork to its
// format.
var iworkFolders = map[string]Format{
	".pages":   Pages,
	".numbers": Numbers,
	".key":     Keynote,
}

// addIWork adds the member name to s, when it is one that names a file of
// iWork.
func (s *zipScan) addIWork(name string, m *zipMember) {
	switch name {
	case "Index/Document.iwa":
		s.iwa = m
	case "index.xml", "index.apxl", "presentation.apxl":
		s.iworkIndex = m
	}
	if rest, ok := strings.CutPrefix(name, "Index/"); ok && strings.HasSuffix(rest, ".iwa") {
		rest = strings.TrimSuffix(rest, ".iwa")
		for _, slide := range []string{"Slide", "MasterSlide"} {
			if rest == slide || strings.HasPrefix(rest, slide+"-") {
				s.slides = true
			}
		}
	}
	// A folder of a package, such as Talk.key/Index.zip.
	if dir, file, ok := strings.Cut(name, "/"); ok {
		switch file {
		case "Index.zip", "index.xml", "index.apxl":
			if f := iworkFolders[strings.ToLower(path.Ext(dir))]; f != Unknown && len(dir) > len(path.Ext(dir)) {
				s.iworkFolder = f
			}
		}
	}
}

// iwork returns the format of a file of iWork, or Unknown.
func (z *zipFile) iwork(s *zipScan) Format {
	switch {
	case s.iwa != nil && s.slides:
		return Keynote
	case s.iwa != nil:
		switch t, _ := iwaType(z.read(s.iwa, min(iwaHead, int64(z.lim.MemberSize)))); t {
		case iwaPages:
			return Pages
		case iwaNumbers:
			return Numbers
		}
		return Unknown
	case s.iworkFolder != Unknown:
		return s.iworkFolder
	case s.iworkIndex != nil:
		return iworkRoot(z.read(s.iworkIndex, int64(z.lim.MemberSize)))
	}
	return Unknown
}

// iworkRoot returns the format that the root element of b, the index of a
// file of iWork 2005 to 2009, gives.
func iworkRoot(b []byte) Format {
	f := Unknown
	elements(b, func(name string, attrs []xmlAttr) bool {
		xmlns := "xmlns"
		if i := strings.LastIndexByte(name, ':'); i >= 0 {
			xmlns += ":" + name[:i]
		}
		var ns string
		for _, a := range attrs {
			if a.name == xmlns {
				ns = strings.TrimSpace(a.value)
			}
		}
		switch local := localName(name); {
		case local == "presentation" && ns == iworkKeynote:
			f = Keynote
		case local == "document" && ns == iworkNumbers:
			f = Numbers
		case local == "document" && ns == iworkPages:
			f = Pages
		}
		// Only the root element names the format.
		return false
	})
	return f
}

// IWA is the format of the objects of iWork 2013 and later. An IWA file is
// a list of chunks. A chunk has a header of 4 bytes, the type 0 and a length
// of 3 bytes in little endian, and data that Snappy compresses, with no
// stream identifier and no checksum. The data holds objects. An object
// starts with the length of its ArchiveInfo, as a varint, and the
// ArchiveInfo, a message of Protocol Buffers. Field 2 of ArchiveInfo is a
// MessageInfo, and field 1 of MessageInfo is the type of the object. The
// notes of Sean Patrick O'Brien on the format of iWork
// (github.com/obriensp/iWorkFileFormat, Docs/index.md) give this layout.
//
// iwaType reads the type of the first object of b, the start of an IWA file.
// The first element of Snappy data is a literal, because no data comes
// before it to copy, so iwaType reads the type without decompressing. It
// returns false when b does not hold the type.
func iwaType(b []byte) (uint64, bool) {
	if len(b) < 4 || b[0] != 0 {
		return 0, false
	}
	b = b[4:min(len(b), 4+(int(b[1])|int(b[2])<<8|int(b[3])<<16))]
	// The length of the data after Snappy decompresses it.
	if _, n := binary.Uvarint(b); n > 0 {
		b = b[n:]
	} else {
		return 0, false
	}
	lit, ok := snappyLiteral(b)
	if !ok {
		return 0, false
	}
	size, n := binary.Uvarint(lit)
	if n <= 0 || size > uint64(len(lit)-n) {
		return 0, false
	}
	wire, _, info := protoField(lit[n:n+int(size)], 2)
	if wire != protoBytes {
		return 0, false
	}
	wire, typ, _ := protoField(info, 1)
	if wire != protoVarint {
		return 0, false
	}
	return typ, true
}

// snappyLiteral returns the data of the literal at the start of b, an
// element of Snappy data. A literal has the tag 0 in its two low bits. The
// six high bits hold the length less 1, or 60 to 63 for a length of 1 to 4
// bytes after the tag, in little endian. It returns false for another
// element. The data can be cut at the end of b.
func snappyLiteral(b []byte) ([]byte, bool) {
	if len(b) == 0 || b[0]&3 != 0 {
		return nil, false
	}
	n, b := uint64(b[0]>>2), b[1:]
	if n >= 60 {
		k := int(n - 59)
		if len(b) < k {
			return nil, false
		}
		n = 0
		for i := k - 1; i >= 0; i-- {
			n = n<<8 | uint64(b[i])
		}
		b = b[k:]
	}
	return b[:min(uint64(len(b)), n+1)], true
}

// The wire types of Protocol Buffers that protoField reads, and protoNone,
// which is no wire type.
const (
	protoNone   = 8
	protoVarint = 0
	protoI64    = 1
	protoBytes  = 2
	protoI32    = 5
)

// protoField returns the wire type and the value of the first field num of
// the message b: a number for protoVarint, or the bytes for protoBytes. It
// returns protoNone when b does not hold the field, or holds a value that
// it cannot read before it.
func protoField(b []byte, num uint64) (uint64, uint64, []byte) {
	for len(b) > 0 {
		key, n := binary.Uvarint(b)
		if n <= 0 {
			return protoNone, 0, nil
		}
		b = b[n:]
		var x uint64
		var data []byte
		switch key & 7 {
		case protoVarint:
			if x, n = binary.Uvarint(b); n <= 0 {
				return protoNone, 0, nil
			}
			b = b[n:]
		case protoI64:
			if len(b) < 8 {
				return protoNone, 0, nil
			}
			b = b[8:]
		case protoBytes:
			size, n := binary.Uvarint(b)
			if n <= 0 || size > uint64(len(b)-n) {
				return protoNone, 0, nil
			}
			data, b = b[n:n+int(size)], b[n+int(size):]
		case protoI32:
			if len(b) < 4 {
				return protoNone, 0, nil
			}
			b = b[4:]
		default:
			return protoNone, 0, nil
		}
		if key>>3 == num {
			return key & 7, x, data
		}
	}
	return protoNone, 0, nil
}
