package cfb

import (
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// This file reads the property sets of [MS-OLEPS].

// PropertyType is the type of the value of a property, a VARTYPE of
// [MS-OLEPS] 2.15. The flag VTVector marks a vector of the type in the low
// bits.
type PropertyType uint16

// The types of values that ParsePropertySet reads. It keeps the bytes of any
// other type.
const (
	VTEmpty    PropertyType = 0x0000
	VTNull     PropertyType = 0x0001
	VTI2       PropertyType = 0x0002
	VTI4       PropertyType = 0x0003
	VTR4       PropertyType = 0x0004
	VTR8       PropertyType = 0x0005
	VTCY       PropertyType = 0x0006
	VTDate     PropertyType = 0x0007
	VTBSTR     PropertyType = 0x0008
	VTError    PropertyType = 0x000A
	VTBool     PropertyType = 0x000B
	VTVariant  PropertyType = 0x000C
	VTI1       PropertyType = 0x0010
	VTUI1      PropertyType = 0x0011
	VTUI2      PropertyType = 0x0012
	VTUI4      PropertyType = 0x0013
	VTI8       PropertyType = 0x0014
	VTUI8      PropertyType = 0x0015
	VTInt      PropertyType = 0x0016
	VTUInt     PropertyType = 0x0017
	VTLPSTR    PropertyType = 0x001E
	VTLPWSTR   PropertyType = 0x001F
	VTFiletime PropertyType = 0x0040
	VTBlob     PropertyType = 0x0041
	VTCF       PropertyType = 0x0047
	VTCLSID    PropertyType = 0x0048
	VTVector   PropertyType = 0x1000
	VTArray    PropertyType = 0x2000
)

// The identifiers of properties that every section can have.
const (
	PIDDictionary = 0x00000000
	PIDCodePage   = 0x00000001
	PIDLocale     = 0x80000000
)

// The identifiers of the properties of the section FMTIDSummaryInformation,
// in [MS-OLEPS] 2.25.1.
const (
	PIDSITitle       = 0x02
	PIDSISubject     = 0x03
	PIDSIAuthor      = 0x04
	PIDSIKeywords    = 0x05
	PIDSIComments    = 0x06
	PIDSITemplate    = 0x07
	PIDSILastAuthor  = 0x08
	PIDSIRevNumber   = 0x09
	PIDSIEditTime    = 0x0A
	PIDSILastPrinted = 0x0B
	PIDSICreateTime  = 0x0C
	PIDSILastSave    = 0x0D
	PIDSIPageCount   = 0x0E
	PIDSIWordCount   = 0x0F
	PIDSICharCount   = 0x10
	PIDSIThumbnail   = 0x11
	PIDSIAppName     = 0x12
	PIDSIDocSecurity = 0x13
)

// The FMTIDs of the sections that Office writes. The stream
// "\x05SummaryInformation" holds FMTIDSummaryInformation, and the stream
// "\x05DocumentSummaryInformation" holds FMTIDDocSummaryInformation, and
// can hold FMTIDUserDefinedProperties after it.
var (
	FMTIDSummaryInformation    = mustGUID("F29F85E0-4FF9-1068-AB91-08002B27B3D9")
	FMTIDDocSummaryInformation = mustGUID("D5CDD502-2E9C-101B-9397-08002B2CF9AE")
	FMTIDUserDefinedProperties = mustGUID("D5CDD505-2E9C-101B-9397-08002B2CF9AE")
)

// The formats of the clipboard of Windows that a thumbnail uses, in the
// field Format of ClipboardData.
const (
	CFBitmap       = 2
	CFMetafilePict = 3
	CFDIB          = 8
	CFEnhMetafile  = 14
)

// PropertySet is a stream of properties: the header and its sections.
type PropertySet struct {
	Version  uint16 // 0 or 1
	SystemID uint32 // the system that wrote the stream. The high 16 bits name the system, and 2 is Windows
	CLSID    GUID
	Sections []*Section
}

// Section is a set of properties, which its FMTID names.
type Section struct {
	FMTID      GUID
	CodePage   uint16            // the value of the property PIDCodePage, or 0
	Names      map[uint32]string // the names of the dictionary, PIDDictionary, or nil
	Properties []Property        // the properties, in the order of the section, without the dictionary
}

// Property is a property of a section.
//
// Value holds a Go value for each type:
//
//   - VTEmpty, VTNull: nil
//   - VTI1, VTUI1, VTI2, VTUI2: int8, uint8, int16, uint16
//   - VTI4, VTInt: int32. VTUI4, VTUInt, VTError: uint32
//   - VTI8, VTCY: int64. VTUI8: uint64
//   - VTR4: float32. VTR8, VTDate: float64
//   - VTBool: bool
//   - VTLPSTR, VTBSTR: CodePageString
//   - VTLPWSTR: string
//   - VTFiletime: Filetime
//   - VTBlob: []byte
//   - VTCF: ClipboardData
//   - VTCLSID: GUID
//   - VTVector with one of these types: []any of the values
//   - VTVector with VTVariant: []any of Variant
//
// For any other type, Value holds the bytes after the type, up to the next
// property or to the end of the section, as a []byte.
type Property struct {
	ID    uint32
	Type  PropertyType
	Value any
}

// Variant is an element of a vector of VTVariant.
type Variant struct {
	Type  PropertyType
	Value any
}

// CodePageString is a string in the code page of its section.
type CodePageString struct {
	CodePage uint16 // the code page, such as 1252, or 1200 for UTF-16
	Bytes    []byte // the bytes of the string, with the NUL at the end
}

// Text returns the string before the first NUL, as UTF-8. It decodes the
// code pages 1200 (UTF-16), 65001 (UTF-8), 1252 (Windows Latin 1) and 28591
// (ISO 8859-1). For another code page, it decodes only a string of ASCII.
// When it cannot decode the string, it returns false.
func (s CodePageString) Text() (string, bool) {
	b := s.Bytes
	if s.CodePage == 1200 {
		u := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			c := le.Uint16(b[i:])
			if c == 0 {
				break
			}
			u = append(u, c)
		}
		return string(utf16.Decode(u)), true
	}
	if i := slices.Index(b, 0); i >= 0 {
		b = b[:i]
	}
	switch s.CodePage {
	case 65001:
		return string(b), utf8.Valid(b)
	case 1252, 28591:
		var sb strings.Builder
		for _, c := range b {
			r := rune(c)
			if s.CodePage == 1252 && c >= 0x80 && c < 0xA0 {
				r = cp1252[c-0x80]
			}
			sb.WriteRune(r)
		}
		return sb.String(), true
	}
	for _, c := range b {
		if c >= 0x80 {
			return "", false
		}
	}
	return string(b), true
}

// cp1252 maps the bytes 0x80 to 0x9F of code page 1252. A byte that the
// code page does not use maps to the same number, as Windows maps it.
var cp1252 = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

// Filetime is a FILETIME: the number of intervals of 100 ns since
// 1601-01-01 UTC. A property of a time holds a date, and PIDSIEditTime holds
// a duration.
type Filetime uint64

// unixOffset is the number of seconds from 1601-01-01 to 1970-01-01.
const unixOffset = 11644473600

// Time returns t as a time in UTC. It returns the zero time for 0.
func (t Filetime) Time() time.Time {
	if t == 0 {
		return time.Time{}
	}
	return time.Unix(int64(t/1e7)-unixOffset, int64(t%1e7)*100).UTC()
}

// Duration returns t as a duration. It returns the longest duration when t
// is longer.
func (t Filetime) Duration() time.Duration {
	if t > math.MaxInt64/100 {
		return math.MaxInt64
	}
	return time.Duration(t) * 100
}

// ClipboardData is the value of a property of type VTCF, such as the
// thumbnail of a document, PIDSIThumbnail.
//
// Tag says what follows it:
//
//   - -1: Format is a format of the clipboard of Windows, such as
//     CFMetafilePict, and Data holds the data. For CFMetafilePict, Data
//     starts with the 8 bytes of a METAFILEPICT of 16 bits, and a Windows
//     metafile follows. For CFDIB, Data holds a BITMAPINFO and the pixels,
//     and for CFEnhMetafile, an enhanced metafile.
//   - -2: Format is a format of the clipboard of the Macintosh.
//   - -3: Data starts with the FMTID of the format.
//   - A number over 0: Data starts with the name of the format, in that
//     number of bytes.
//   - 0: Data has no format.
type ClipboardData struct {
	Tag    int32
	Format uint32 // the format for the tags -1 and -2, or 0
	Data   []byte
}

// Section returns the first section with the FMTID id, or nil.
func (ps *PropertySet) Section(id GUID) *Section {
	for _, s := range ps.Sections {
		if s.FMTID == id {
			return s
		}
	}
	return nil
}

// Thumbnail returns the property PIDSIThumbnail of the section
// FMTIDSummaryInformation, when it has the type VTCF.
func (ps *PropertySet) Thumbnail() (ClipboardData, bool) {
	s := ps.Section(FMTIDSummaryInformation)
	if s == nil {
		return ClipboardData{}, false
	}
	p, ok := s.Property(PIDSIThumbnail)
	if !ok {
		return ClipboardData{}, false
	}
	cd, ok := p.Value.(ClipboardData)
	return cd, ok
}

// Property returns the first property with the identifier id.
func (s *Section) Property(id uint32) (Property, bool) {
	for _, p := range s.Properties {
		if p.ID == id {
			return p, true
		}
	}
	return Property{}, false
}

// ParsePropertySet reads the property set in b, such as the bytes of the
// stream "\x05SummaryInformation". It returns an error that wraps ErrCorrupt
// when the header or a section is damaged. It leaves out a property whose
// value does not fit in its section.
//
// The values in b stay in the result: a string, a blob or the data of the
// clipboard is a part of b, not a copy.
func ParsePropertySet(b []byte) (*PropertySet, error) {
	if len(b) < 28 {
		return nil, fmt.Errorf("%w: property set of %d bytes", ErrCorrupt, len(b))
	}
	if o := le.Uint16(b); o != 0xFFFE {
		return nil, fmt.Errorf("%w: property set with byte order %#x", ErrCorrupt, o)
	}
	ps := &PropertySet{Version: le.Uint16(b[2:]), SystemID: le.Uint32(b[4:])}
	copy(ps.CLSID[:], b[8:24])
	n := le.Uint32(b[24:])
	if uint64(n) > uint64(len(b)-28)/20 {
		return nil, fmt.Errorf("%w: %d sections in %d bytes", ErrCorrupt, n, len(b))
	}
	for i := range int(n) {
		h := b[28+20*i:]
		s, err := parseSection(b, le.Uint32(h[16:]))
		if err != nil {
			return nil, fmt.Errorf("%w, in section %d", err, i)
		}
		copy(s.FMTID[:], h[:16])
		ps.Sections = append(ps.Sections, s)
	}
	return ps, nil
}

// parseSection reads the section at off in b.
func parseSection(b []byte, off uint32) (*Section, error) {
	if uint64(off)+8 > uint64(len(b)) {
		return nil, fmt.Errorf("%w: section at %d past the end", ErrCorrupt, off)
	}
	size := le.Uint32(b[off:])
	if size < 8 {
		return nil, fmt.Errorf("%w: section of %d bytes", ErrCorrupt, size)
	}
	// A section that the stream cuts keeps the properties in the stream.
	sec := b[off:min(uint64(off)+uint64(size), uint64(len(b)))]
	n := le.Uint32(sec[4:])
	if uint64(n) > uint64(len(sec)-8)/8 {
		return nil, fmt.Errorf("%w: %d properties in %d bytes", ErrCorrupt, n, len(sec))
	}
	type entry struct{ id, off uint32 }
	ents := make([]entry, n)
	offs := make([]uint32, 0, n)
	for i := range ents {
		ents[i] = entry{le.Uint32(sec[8+8*i:]), le.Uint32(sec[12+8*i:])}
		offs = append(offs, ents[i].off)
	}
	slices.Sort(offs)
	// end returns the end of the property at o: the next property, or the
	// end of the section.
	end := func(o uint32) uint32 {
		i, _ := slices.BinarySearch(offs, o+1)
		if i < len(offs) && offs[i] >= o+4 && offs[i] <= uint32(len(sec)) {
			return offs[i]
		}
		return uint32(len(sec))
	}
	s := &Section{}
	for _, e := range ents {
		if e.id != PIDCodePage || uint64(e.off)+6 > uint64(len(sec)) {
			continue
		}
		if PropertyType(le.Uint16(sec[e.off:])) == VTI2 {
			s.CodePage = le.Uint16(sec[e.off+4:])
		}
		break
	}
	for _, e := range ents {
		if uint64(e.off)+4 > uint64(len(sec)) || e.off < 8 {
			continue
		}
		if e.id == PIDDictionary {
			if s.Names == nil {
				s.Names = parseDictionary(sec[e.off:], s.CodePage)
			}
			continue
		}
		t := PropertyType(le.Uint16(sec[e.off:]))
		v := sec[e.off+4:]
		p := Property{ID: e.id, Type: t}
		switch {
		case t == VTVariant:
			// A variant stands only in a vector.
			p.Value = sec[e.off+4 : end(e.off)]
		case t&VTVector != 0 && t&^VTVector < VTVector:
			val, ok := parseVector(v, t&^VTVector, s.CodePage)
			if !ok {
				continue
			}
			p.Value = val
		default:
			val, _, ok := parseValue(v, t, s.CodePage, 0)
			if !ok {
				continue
			}
			if val == nil && t != VTEmpty && t != VTNull {
				val = sec[e.off+4 : end(e.off)]
			}
			p.Value = val
		}
		s.Properties = append(s.Properties, p)
	}
	return s, nil
}

// parseValue reads a value of the type t at the start of b. It returns the
// value, its size without padding, and false when b is too short. For a type
// that it does not read, it returns nil and true. depth counts the variants
// that hold the value.
func parseValue(b []byte, t PropertyType, cp uint16, depth int) (any, int, bool) {
	fixed := func(n int) bool { return len(b) >= n }
	switch t {
	case VTEmpty, VTNull:
		return nil, 0, true
	case VTI1, VTUI1:
		if !fixed(1) {
			return nil, 0, false
		}
		if t == VTI1 {
			return int8(b[0]), 1, true
		}
		return b[0], 1, true
	case VTI2, VTUI2, VTBool:
		if !fixed(2) {
			return nil, 0, false
		}
		v := le.Uint16(b)
		switch t {
		case VTI2:
			return int16(v), 2, true
		case VTBool:
			return v != 0, 2, true
		}
		return v, 2, true
	case VTI4, VTInt, VTUI4, VTUInt, VTError, VTR4:
		if !fixed(4) {
			return nil, 0, false
		}
		v := le.Uint32(b)
		switch t {
		case VTI4, VTInt:
			return int32(v), 4, true
		case VTR4:
			return math.Float32frombits(v), 4, true
		}
		return v, 4, true
	case VTI8, VTCY, VTUI8, VTR8, VTDate, VTFiletime:
		if !fixed(8) {
			return nil, 0, false
		}
		v := le.Uint64(b)
		switch t {
		case VTI8, VTCY:
			return int64(v), 8, true
		case VTR8, VTDate:
			return math.Float64frombits(v), 8, true
		case VTFiletime:
			return Filetime(v), 8, true
		}
		return v, 8, true
	case VTCLSID:
		if !fixed(16) {
			return nil, 0, false
		}
		var g GUID
		copy(g[:], b)
		return g, 16, true
	case VTLPSTR, VTBSTR, VTBlob, VTCF:
		if !fixed(4) {
			return nil, 0, false
		}
		n := le.Uint32(b)
		if uint64(n) > uint64(len(b)-4) {
			return nil, 0, false
		}
		d := b[4 : 4+n]
		switch t {
		case VTBlob:
			return d, 4 + int(n), true
		case VTCF:
			cd, ok := parseClipboard(d)
			return cd, 4 + int(n), ok
		}
		return CodePageString{CodePage: cp, Bytes: d}, 4 + int(n), true
	case VTLPWSTR:
		if !fixed(4) {
			return nil, 0, false
		}
		n := le.Uint32(b)
		if uint64(n) > uint64(len(b)-4)/2 {
			return nil, 0, false
		}
		s, _ := CodePageString{CodePage: 1200, Bytes: b[4 : 4+2*n]}.Text()
		return s, 4 + 2*int(n), true
	case VTVariant:
		if depth > 0 || !fixed(4) {
			return nil, 0, false
		}
		vt := PropertyType(le.Uint16(b))
		v, n, ok := parseValue(b[4:], vt, cp, depth+1)
		if !ok || (v == nil && vt != VTEmpty && vt != VTNull) {
			return nil, 0, false
		}
		return Variant{Type: vt, Value: v}, 4 + n, true
	}
	return nil, 0, true
}

// parseVector reads a vector of values of the type t at the start of b.
func parseVector(b []byte, t PropertyType, cp uint16) ([]any, bool) {
	if len(b) < 4 {
		return nil, false
	}
	n := le.Uint32(b)
	b = b[4:]
	if uint64(n) > uint64(len(b)) {
		return nil, false
	}
	vals := make([]any, 0, n)
	for range n {
		v, size, ok := parseValue(b, t, cp, 0)
		if !ok || (v == nil && t != VTEmpty && t != VTNull) {
			return nil, false
		}
		vals = append(vals, v)
		b = b[size:]
		switch t {
		case VTLPSTR, VTBSTR, VTLPWSTR, VTBlob, VTCF, VTVariant:
			// [MS-OLEPS] pads each of these values to 4 bytes. Some writers
			// do not pad the strings of a vector. The padding is zero, and the
			// size of the next string has a byte that is not, so only zeros
			// are taken as padding.
			pad := (4 - size%4) % 4
			if pad <= len(b) && !slices.ContainsFunc(b[:pad], func(c byte) bool { return c != 0 }) {
				b = b[pad:]
			}
		}
	}
	return vals, true
}

// parseClipboard reads the data of a value of type VTCF, after its size.
func parseClipboard(d []byte) (ClipboardData, bool) {
	if len(d) < 4 {
		return ClipboardData{}, false
	}
	cd := ClipboardData{Tag: int32(le.Uint32(d)), Data: d[4:]}
	if cd.Tag == -1 || cd.Tag == -2 {
		if len(cd.Data) < 4 {
			return ClipboardData{}, false
		}
		cd.Format = le.Uint32(cd.Data)
		cd.Data = cd.Data[4:]
	}
	return cd, true
}

// parseDictionary reads the names of a dictionary, [MS-OLEPS] 2.17. It reads
// the entries that fit in b.
func parseDictionary(b []byte, cp uint16) map[uint32]string {
	if len(b) < 4 {
		return nil
	}
	n := le.Uint32(b)
	b = b[4:]
	names := make(map[uint32]string, min(n, uint32(len(b)/8)))
	for range n {
		if len(b) < 8 {
			break
		}
		id, size := le.Uint32(b), uint64(le.Uint32(b[4:]))
		if cp == 1200 {
			size *= 2
		}
		if size > uint64(len(b)-8) {
			break
		}
		s := CodePageString{CodePage: cp, Bytes: b[8 : 8+size]}
		text, ok := s.Text()
		if !ok {
			text = strings.ToValidUTF8(string(s.Bytes[:max(len(s.Bytes)-1, 0)]), "�")
		}
		names[id] = text
		b = b[8+size:]
		if cp == 1200 {
			// A name of UTF-16 is padded to 4 bytes.
			b = b[min(int((4-size%4)%4), len(b)):]
		}
	}
	return names
}

// mustGUID returns the GUID of the string s, such as
// "F29F85E0-4FF9-1068-AB91-08002B27B3D9".
func mustGUID(s string) GUID {
	h, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(h) != 16 {
		panic("cfb: bad GUID " + s)
	}
	var g GUID
	le.PutUint32(g[0:], uint32(h[0])<<24|uint32(h[1])<<16|uint32(h[2])<<8|uint32(h[3]))
	le.PutUint16(g[4:], uint16(h[4])<<8|uint16(h[5]))
	le.PutUint16(g[6:], uint16(h[6])<<8|uint16(h[7]))
	copy(g[8:], h[8:])
	return g
}
