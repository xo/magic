package container

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/xo/magic/cfb"
)

// The class IDs of the root storage of Visio 2000 to 2010 and of
// Publisher, as ole2compounddocs of file gives them:
// {00021A13-0000-0000-C000-000000000046} for Visio 2000 to 2002,
// {00021A14-0000-0000-C000-000000000046} for Visio 2003 to 2010,
// {00021200-0000-0000-00C0-000000000046} for Publisher 95 and
// {00021201-0000-0000-00C0-000000000046} for Publisher 97 and later. A
// GUID keeps its first three fields in little endian.
var (
	clsidVisio2000 = cfb.GUID{0x13, 0x1A, 0x02, 0x00, 0, 0, 0, 0, 0xC0, 0, 0, 0, 0, 0, 0, 0x46}
	clsidVisio2003 = cfb.GUID{0x14, 0x1A, 0x02, 0x00, 0, 0, 0, 0, 0xC0, 0, 0, 0, 0, 0, 0, 0x46}
	clsidPub95     = cfb.GUID{0x00, 0x12, 0x02, 0x00, 0, 0, 0, 0, 0, 0xC0, 0, 0, 0, 0, 0, 0x46}
	clsidPub97     = cfb.GUID{0x01, 0x12, 0x02, 0x00, 0, 0, 0, 0, 0, 0xC0, 0, 0, 0, 0, 0, 0x46}
)

// detectCFB reads the directory of the Compound File. An Office Open XML
// file that a password protects has the streams EncryptionInfo and
// EncryptedPackage in its root ([MS-OFFCRYPTO] 2.3.4.4). A binary Office
// file whose properties a password protects has the stream EncryptedSummary,
// and libmagic names both "CDFV2 Encrypted". It has no EncryptedPackage, so
// Detect gives Unknown for it.
//
// A drawing of Visio and a document of Publisher have a class ID of their
// own in the root storage. libmagic has rules for these class IDs, but at
// the commit of the ledger it stops before them with "Cannot read section
// info" (item 17 of docs/BACKLOG.md). A program can write no class ID, so
// Detect also reads the streams: VisioDocument in the root of Visio, and
// Contents with the storage Quill in the root of Publisher, as the files of
// Apache POI hold them.
func detectCFB(r io.ReaderAt, size int64, lim Limits) (Format, error) {
	f, err := cfb.Open(r, size, cfb.Limits{Entries: lim.Members})
	switch {
	case errors.Is(err, cfb.ErrLimit):
		return Unknown, fmt.Errorf("%w: %w", ErrLimit, err)
	case errors.Is(err, cfb.ErrCorrupt):
		return Unknown, fmt.Errorf("%w: %w", ErrCorrupt, err)
	case err != nil:
		return Unknown, err
	}
	root := f.Root()
	switch root.CLSID {
	case clsidVisio2000, clsidVisio2003:
		return VSD, nil
	case clsidPub95, clsidPub97:
		return PUB, nil
	}
	var info, pkg, visio, contents, quill bool
	for _, e := range root.Children {
		switch {
		case e.Type == cfb.StorageEntry:
			quill = quill || strings.EqualFold(e.Name, "Quill")
		case e.Type != cfb.StreamEntry:
		case strings.EqualFold(e.Name, "EncryptionInfo"):
			info = true
		case strings.EqualFold(e.Name, "EncryptedPackage"):
			pkg = true
		case strings.EqualFold(e.Name, "VisioDocument"):
			visio = true
		case strings.EqualFold(e.Name, "Contents"):
			contents = true
		}
	}
	switch {
	case info && pkg:
		return EncryptedOOXML, nil
	case visio:
		return VSD, nil
	case contents && quill:
		return PUB, nil
	}
	return Unknown, nil
}
