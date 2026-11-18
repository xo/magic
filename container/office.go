package container

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/xo/magic/cfb"
)

// detectCFB reads the directory of the Compound File. An Office Open XML
// file that a password protects has the streams EncryptionInfo and
// EncryptedPackage in its root ([MS-OFFCRYPTO] 2.3.4.4). A binary Office
// file whose properties a password protects has the stream EncryptedSummary,
// and libmagic names both "CDFV2 Encrypted". It has no EncryptedPackage, so
// Detect gives Unknown for it.
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
	var info, pkg bool
	for _, e := range f.Root().Children {
		if e.Type != cfb.StreamEntry {
			continue
		}
		switch {
		case strings.EqualFold(e.Name, "EncryptionInfo"):
			info = true
		case strings.EqualFold(e.Name, "EncryptedPackage"):
			pkg = true
		}
	}
	if info && pkg {
		return EncryptedOOXML, nil
	}
	return Unknown, nil
}
