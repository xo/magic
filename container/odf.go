package container

import (
	"bytes"
	"strings"
)

// odfTypes maps the MIME type of an OpenDocument file to its format. The
// types are the ones that libmagic names from the member mimetype (archive
// of file). shared-mime-info gives application/vnd.oasis.opendocument.base
// for the older name application/vnd.oasis.opendocument.database.
var odfTypes = map[string]Format{
	"application/vnd.oasis.opendocument.text":                  ODText,
	"application/vnd.oasis.opendocument.text-template":         ODTextTemplate,
	"application/vnd.oasis.opendocument.text-web":              ODTextWeb,
	"application/vnd.oasis.opendocument.text-master":           ODTextMaster,
	"application/vnd.oasis.opendocument.text-master-template":  ODTextMasterTemplate,
	"application/vnd.oasis.opendocument.graphics":              ODGraphics,
	"application/vnd.oasis.opendocument.graphics-template":     ODGraphicsTemplate,
	"application/vnd.oasis.opendocument.presentation":          ODPresentation,
	"application/vnd.oasis.opendocument.presentation-template": ODPresentationTemplate,
	"application/vnd.oasis.opendocument.spreadsheet":           ODSpreadsheet,
	"application/vnd.oasis.opendocument.spreadsheet-template":  ODSpreadsheetTemplate,
	"application/vnd.oasis.opendocument.chart":                 ODChart,
	"application/vnd.oasis.opendocument.chart-template":        ODChartTemplate,
	"application/vnd.oasis.opendocument.formula":               ODFormula,
	"application/vnd.oasis.opendocument.formula-template":      ODFormulaTemplate,
	"application/vnd.oasis.opendocument.base":                  ODBase,
	"application/vnd.oasis.opendocument.database":              ODBase,
	"application/vnd.oasis.opendocument.image":                 ODImage,
	"application/vnd.oasis.opendocument.image-template":        ODImageTemplate,
}

// libmagicMimetype is what archive of file reads at offset 26 of a zip file
// to find a member mimetype: a name of 8 bytes, an extra field of 0 bytes,
// the name, and data that starts with "application/". libmagic then names
// the file from the data, so Detect does not.
const libmagicMimetype = "\x08\x00\x00\x00mimetypeapplication/"

// libmagicNamesMimetype reports whether libmagic reads the member mimetype
// of the zip file, because it is the first member, stored, with no extra
// field.
func (z *zipFile) libmagicNamesMimetype() bool {
	b := make([]byte, 26+len(libmagicMimetype))
	if readFull(z.r, b, 0, "the first local header") != nil {
		return false
	}
	return string(b[26:]) == libmagicMimetype
}

// odfManifestType returns the format that b, the part META-INF/manifest.xml
// of an OpenDocument file, gives to the root of the package: the attribute
// media-type of the element file-entry whose attribute full-path is "/"
// (OpenDocument 1.2 Part 3, 4.3 and 3.3). The package can lack the member
// mimetype, but not the manifest.
func odfManifestType(b []byte) Format {
	f := Unknown
	elements(b, func(name string, attrs []xmlAttr) bool {
		if localName(name) != "file-entry" {
			return true
		}
		if path, ok := attr(attrs, "full-path"); !ok || path != "/" {
			return true
		}
		v, _ := attr(attrs, "media-type")
		f = odfTypes[strings.TrimSpace(v)]
		return false
	})
	return f
}

// odfMimetype returns the format of b, the member mimetype, or Unknown for
// a type that is not one of OpenDocument.
func odfMimetype(b []byte) Format {
	return odfTypes[string(bytes.TrimSpace(b))]
}
