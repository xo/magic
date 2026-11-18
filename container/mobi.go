package container

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// The parts of a Mobipocket book that Detect reads. A Mobipocket book is a
// database of Palm OS: a header of 78 bytes, a list of records of 8 bytes
// each, and the records. The first record holds the header of PalmDOC, 16
// bytes, and the MOBI header after it. Each offset of a field of the MOBI
// header counts from the start of the MOBI header, so it is 16 less than
// the offset in the record. calibre and KindleUnpack read the flags of the
// EXTH header at 0x80 of the record.
const (
	pdbHeaderLen   = 78
	pdbRecordLen   = 8
	palmDOCLen     = 16
	mobiVersionOff = 0x14 // the version of the format in the MOBI header
	mobiFlagsOff   = 0x70 // the flags of the EXTH header in the MOBI header
	mobiHasEXTH    = 0x40
	exthKF8        = 121 // the record of the EXTH that names the KF8 part
	exthNone       = 0xFFFFFFFF
)

var be = binary.BigEndian

// mobiFile is a Mobipocket book that Detect reads.
type mobiFile struct {
	r       io.ReaderAt
	size    int64
	records int
}

// detectMOBI reads the MOBI header of the first record. A MOBI header of
// version 8 is a book of KF8 alone, AZW3. A book with both forms, MOBI and
// KF8, has a MOBI header of version 6, and the record 121 of its EXTH header
// gives the number of the first record of the KF8 part, whose MOBI header
// has version 8. calibre and KindleUnpack read the joint books so.
func detectMOBI(r io.ReaderAt, size int64, lim Limits) (Format, error) {
	var h [pdbHeaderLen]byte
	if err := readFull(r, h[:], 0, "the header of the book"); err != nil {
		return Unknown, err
	}
	m := &mobiFile{r: r, size: size, records: int(be.Uint16(h[76:]))}
	if m.records == 0 {
		return Unknown, fmt.Errorf("%w: a book with no record", ErrCorrupt)
	}
	rec0, err := m.record(0, lim.MemberSize)
	if err != nil {
		return Unknown, err
	}
	version, ok := mobiVersion(rec0)
	switch {
	case !ok:
		// A PalmDOC book with no MOBI header.
		return Unknown, nil
	case version >= 8:
		return AZW3, nil
	}
	kf8, ok := kf8Record(rec0)
	if !ok || kf8 >= m.records {
		return Unknown, nil
	}
	b, err := m.record(kf8, palmDOCLen+mobiVersionOff+4+len("BOUNDARY"))
	if err != nil {
		return Unknown, err
	}
	// Some books name the record BOUNDARY before the KF8 part.
	if bytes.HasPrefix(b, []byte("BOUNDARY")) && kf8+1 < m.records {
		if b, err = m.record(kf8+1, palmDOCLen+mobiVersionOff+4); err != nil {
			return Unknown, err
		}
	}
	if v, ok := mobiVersion(b); ok && v >= 8 {
		return AZW3, nil
	}
	return Unknown, nil
}

// record returns at most limit bytes of the record i.
func (m *mobiFile) record(i, limit int) ([]byte, error) {
	var e [2 * pdbRecordLen]byte
	n := 2 * pdbRecordLen
	if i+1 >= m.records {
		n = pdbRecordLen
	}
	if err := readFull(m.r, e[:n], pdbHeaderLen+int64(i)*pdbRecordLen, "the list of records"); err != nil {
		return nil, err
	}
	start, end := int64(be.Uint32(e[:])), m.size
	if n > pdbRecordLen {
		end = int64(be.Uint32(e[pdbRecordLen:]))
	}
	if start > end || end > m.size {
		return nil, fmt.Errorf("%w: record %d is past the end of the book", ErrCorrupt, i)
	}
	b := make([]byte, min(end-start, int64(limit)))
	if err := readFull(m.r, b, start, "a record"); err != nil {
		return nil, err
	}
	return b, nil
}

// mobiVersion returns the version of the MOBI header of the record b, and
// false when b has no MOBI header.
func mobiVersion(b []byte) (uint32, bool) {
	if len(b) < palmDOCLen+mobiVersionOff+4 || string(b[palmDOCLen:palmDOCLen+4]) != "MOBI" {
		return 0, false
	}
	return be.Uint32(b[palmDOCLen+mobiVersionOff:]), true
}

// kf8Record returns the number of the first record of the KF8 part, from
// the record 121 of the EXTH header of the first record b.
func kf8Record(b []byte) (int, bool) {
	m := b[palmDOCLen:]
	if len(m) < mobiFlagsOff+4 || be.Uint32(m[mobiFlagsOff:])&mobiHasEXTH == 0 {
		return 0, false
	}
	n := be.Uint32(m[4:]) // the length of the MOBI header
	if uint64(n)+12 > uint64(len(m)) {
		return 0, false
	}
	exth := m[n:]
	if string(exth[:4]) != "EXTH" {
		return 0, false
	}
	// The length of the EXTH header counts its 12 bytes.
	end := min(uint64(len(exth)), uint64(be.Uint32(exth[4:])))
	if end < 12 {
		return 0, false
	}
	count, recs := be.Uint32(exth[8:]), exth[12:end]
	for ; count > 0 && len(recs) >= 8; count-- {
		typ, size := be.Uint32(recs), be.Uint32(recs[4:])
		if size < 8 || uint64(size) > uint64(len(recs)) {
			return 0, false
		}
		if typ == exthKF8 && size >= 12 {
			v := be.Uint32(recs[8:])
			if v == exthNone || v > 1<<16 {
				return 0, false
			}
			return int(v), true
		}
		recs = recs[size:]
	}
	return 0, false
}
