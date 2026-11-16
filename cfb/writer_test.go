package cfb_test

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/xo/magic/cfb"
)

// This file writes small Compound Files for the tests, as [MS-CFB] says.

const (
	difSect    = 0xFFFFFFFC
	fatSect    = 0xFFFFFFFD
	endOfChain = 0xFFFFFFFE
	freeSect   = 0xFFFFFFFF
	noStream   = 0xFFFFFFFF
)

var le = binary.LittleEndian

// node is a storage or a stream for the writer.
type node struct {
	name  string
	data  []byte
	dir   bool
	clsid cfb.GUID
	kids  []*node
}

func stream(name string, data []byte) *node { return &node{name: name, data: data} }

func storage(name string, kids ...*node) *node { return &node{name: name, dir: true, kids: kids} }

// layout is a file that build wrote, with the places of its parts.
type layout struct {
	b        []byte
	shift    uint
	fatIDs   []uint32          // the sectors of the table of sectors
	dirStart uint32            // the first sector of the directory
	miniFAT  uint32            // the first sector of the table of mini sectors
	index    map[string]int    // the entry of each path
	starts   map[string]uint32 // the first sector or mini sector of each stream
}

// sectorOff returns the offset of sector id in the file.
func (l *layout) sectorOff(id uint32) int {
	return (int(id) + 1) << l.shift
}

// setFAT changes the entry of sector id in the table of sectors.
func (l *layout) setFAT(id, v uint32) {
	per := uint32(1) << (l.shift - 2)
	le.PutUint32(l.b[l.sectorOff(l.fatIDs[id/per])+4*int(id%per):], v)
}

// setMiniFAT changes the entry of mini sector id. The table of mini sectors
// fills one sector in the tests.
func (l *layout) setMiniFAT(id, v uint32) {
	le.PutUint32(l.b[l.sectorOff(l.miniFAT)+4*int(id):], v)
}

// entryOff returns the offset of the directory entry of path. The directory
// is in sectors that follow one another.
func (l *layout) entryOff(path string) int {
	return l.sectorOff(l.dirStart) + 128*l.index[path]
}

// dirEntry is an entry of the directory as the writer makes it.
type dirEntry struct {
	name               string
	typ                byte
	clsid              cfb.GUID
	left, right, child uint32
	start              uint32
	size               uint64
}

// build writes a Compound File with sectors of 1<<shift bytes, whose root
// holds kids. A stream of less than 4096 bytes goes in the mini stream.
func build(t testing.TB, shift uint, kids ...*node) *layout {
	t.Helper()
	ss := 1 << shift
	per := ss / 4
	l := &layout{shift: shift, index: map[string]int{}, starts: map[string]uint32{}}
	var (
		sectors [][]byte
		fat     []uint32
		mini    []byte
		minifat []uint32
		ents    = []dirEntry{{name: "Root Entry", typ: 5, left: noStream, right: noStream}}
	)
	alloc := func(data []byte) uint32 {
		if len(data) == 0 {
			return endOfChain
		}
		start := uint32(len(sectors))
		for i := 0; i < len(data); i += ss {
			s := make([]byte, ss)
			copy(s, data[i:])
			sectors = append(sectors, s)
			fat = append(fat, uint32(len(sectors)))
		}
		fat[len(fat)-1] = endOfChain
		return start
	}
	var add func(path string, n *node) uint32
	// tree makes a balanced tree of the entries idx, so that an in-order walk
	// gives them in order.
	var tree func(idx []uint32) uint32
	tree = func(idx []uint32) uint32 {
		if len(idx) == 0 {
			return noStream
		}
		m := len(idx) / 2
		ents[idx[m]].left = tree(idx[:m])
		ents[idx[m]].right = tree(idx[m+1:])
		return idx[m]
	}
	children := func(path string, kids []*node) uint32 {
		idx := make([]uint32, 0, len(kids))
		for _, k := range kids {
			p := k.name
			if path != "" {
				p = path + "/" + k.name
			}
			idx = append(idx, add(p, k))
		}
		return tree(idx)
	}
	add = func(path string, n *node) uint32 {
		i := uint32(len(ents))
		l.index[path] = int(i)
		ents = append(ents, dirEntry{name: n.name, clsid: n.clsid, child: noStream, start: endOfChain})
		if n.dir {
			ents[i].typ = 1
			c := children(path, n.kids)
			ents[i].child = c
			return i
		}
		ents[i].typ = 2
		ents[i].size = uint64(len(n.data))
		switch {
		case len(n.data) == 0:
		case len(n.data) < 4096:
			start := uint32(len(mini) / 64)
			for j := 0; j < len(n.data); j += 64 {
				b := make([]byte, 64)
				copy(b, n.data[j:])
				mini = append(mini, b...)
				minifat = append(minifat, uint32(len(mini)/64))
			}
			minifat[len(minifat)-1] = endOfChain
			ents[i].start = start
		default:
			ents[i].start = alloc(n.data)
		}
		l.starts[path] = ents[i].start
		return i
	}
	ents[0].child = children("", kids)
	ents[0].start = alloc(mini)
	ents[0].size = uint64(len(mini))
	var mf []byte
	for _, v := range minifat {
		mf = le.AppendUint32(mf, v)
	}
	for len(mf)%ss != 0 {
		mf = le.AppendUint32(mf, freeSect)
	}
	l.miniFAT = alloc(mf)
	nminifat := len(mf) / ss
	var dir []byte
	for _, e := range ents {
		dir = appendEntry(dir, e)
	}
	for len(dir)%ss != 0 {
		dir = appendEntry(dir, dirEntry{left: noStream, right: noStream, child: noStream})
	}
	l.dirStart = alloc(dir)
	// The table of sectors covers its own sectors and the DIFAT sectors.
	n := len(sectors)
	k, d := 0, 0
	for {
		k2 := (n + k + d + per - 1) / per
		d2 := 0
		if k2 > 109 {
			d2 = (k2 - 109 + per - 2) / (per - 1)
		}
		if k2 == k && d2 == d {
			break
		}
		k, d = k2, d2
	}
	for i := range k {
		l.fatIDs = append(l.fatIDs, uint32(n+i))
		fat = append(fat, fatSect)
	}
	for range d {
		fat = append(fat, difSect)
	}
	for len(fat)%per != 0 {
		fat = append(fat, freeSect)
	}
	for i := range k {
		s := make([]byte, ss)
		for j := range per {
			le.PutUint32(s[4*j:], fat[i*per+j])
		}
		sectors = append(sectors, s)
	}
	rest := l.fatIDs[min(k, 109):]
	for i := range d {
		s := make([]byte, ss)
		for j := range per - 1 {
			v := uint32(freeSect)
			if len(rest) > 0 {
				v, rest = rest[0], rest[1:]
			}
			le.PutUint32(s[4*j:], v)
		}
		next := uint32(endOfChain)
		if i < d-1 {
			next = uint32(n + k + i + 1)
		}
		le.PutUint32(s[4*(per-1):], next)
		sectors = append(sectors, s)
	}
	h := make([]byte, ss)
	le.PutUint64(h, 0xE11AB1A1E011CFD0)
	le.PutUint16(h[24:], 0x3E)
	major := uint16(3)
	if shift == 12 {
		major = 4
		le.PutUint32(h[40:], uint32(len(dir)/ss))
	}
	le.PutUint16(h[26:], major)
	le.PutUint16(h[28:], 0xFFFE)
	le.PutUint16(h[30:], uint16(shift))
	le.PutUint16(h[32:], 6)
	le.PutUint32(h[44:], uint32(k))
	le.PutUint32(h[48:], l.dirStart)
	le.PutUint32(h[56:], 4096)
	le.PutUint32(h[60:], l.miniFAT)
	le.PutUint32(h[64:], uint32(nminifat))
	if d > 0 {
		le.PutUint32(h[68:], uint32(n+k))
	} else {
		le.PutUint32(h[68:], endOfChain)
	}
	le.PutUint32(h[72:], uint32(d))
	for i := range 109 {
		v := uint32(freeSect)
		if i < k {
			v = l.fatIDs[i]
		}
		le.PutUint32(h[76+4*i:], v)
	}
	l.b = h
	for _, s := range sectors {
		l.b = append(l.b, s...)
	}
	return l
}

// appendEntry appends the 128 bytes of the directory entry e to b.
func appendEntry(b []byte, e dirEntry) []byte {
	var x [128]byte
	u := utf16.Encode([]rune(e.name))
	for i, c := range u {
		le.PutUint16(x[2*i:], c)
	}
	if e.name != "" {
		le.PutUint16(x[64:], uint16(2*len(u)+2))
	}
	x[66] = e.typ
	x[67] = 1
	le.PutUint32(x[68:], e.left)
	le.PutUint32(x[72:], e.right)
	le.PutUint32(x[76:], e.child)
	copy(x[80:], e.clsid[:])
	le.PutUint32(x[116:], e.start)
	le.PutUint64(x[120:], e.size)
	return append(b, x[:]...)
}

// pattern returns n bytes that differ in each sector, so that a sector read
// from the wrong place shows.
func pattern(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7+i/509) ^ seed
	}
	return b
}

// The values of a property set for the tests. Each function returns the
// bytes of a value with its type, padded to 4 bytes, as [MS-OLEPS] says.

// typed returns the value b of type t.
func typed(t cfb.PropertyType, b ...[]byte) []byte {
	out := le.AppendUint32(nil, uint32(t))
	for _, x := range b {
		out = append(out, x...)
	}
	return pad4(out)
}

func pad4(b []byte) []byte {
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func u16(v uint16) []byte { return le.AppendUint16(nil, v) }
func u32(v uint32) []byte { return le.AppendUint32(nil, v) }
func u64(v uint64) []byte { return le.AppendUint64(nil, v) }

// lpstr returns a CodePageString without padding: its size, its bytes and a
// NUL.
func lpstr(s string) []byte {
	return append(le.AppendUint32(nil, uint32(len(s)+1)), append([]byte(s), 0)...)
}

// lpwstr returns a UnicodeString without padding.
func lpwstr(s string) []byte {
	u := utf16.Encode([]rune(s + "\x00"))
	b := le.AppendUint32(nil, uint32(len(u)))
	for _, c := range u {
		b = le.AppendUint16(b, c)
	}
	return b
}

// prop is a property of a section for the tests. val holds the type and the
// value, or the dictionary for the identifier 0.
type prop struct {
	id  uint32
	val []byte
}

// section is a section of a property set for the tests.
type section struct {
	fmtid cfb.GUID
	props []prop
}

// propertySet returns the bytes of a property set with the sections secs.
func propertySet(secs ...section) []byte {
	b := u16(0xFFFE)
	b = append(b, u16(0)...)
	b = append(b, u32(0x00020006)...)
	b = append(b, make([]byte, 16)...)
	b = append(b, u32(uint32(len(secs)))...)
	off := len(b) + 20*len(secs)
	var body []byte
	for _, s := range secs {
		b = append(b, s.fmtid[:]...)
		b = append(b, u32(uint32(off+len(body)))...)
		hdr := 8 + 8*len(s.props)
		var vals []byte
		var tab []byte
		for _, p := range s.props {
			tab = append(tab, u32(p.id)...)
			tab = append(tab, u32(uint32(hdr+len(vals)))...)
			vals = append(vals, p.val...)
		}
		sec := u32(uint32(hdr + len(vals)))
		sec = append(sec, u32(uint32(len(s.props)))...)
		sec = append(sec, tab...)
		sec = append(sec, vals...)
		body = append(body, sec...)
	}
	return append(b, body...)
}

// dictionary returns the value of the property 0: the names of ids, in a
// code page of one byte.
func dictionary(names map[uint32]string, order ...uint32) []byte {
	b := u32(uint32(len(order)))
	for _, id := range order {
		b = append(b, u32(id)...)
		b = append(b, lpstr(names[id])...)
	}
	return pad4(b)
}

// summary returns a SummaryInformation stream with a thumbnail of the format
// cf and the data data.
func summary(cf uint32, data []byte) []byte {
	thumb := u32(uint32(8 + len(data)))
	thumb = append(thumb, u32(0xFFFFFFFF)...)
	thumb = append(thumb, u32(cf)...)
	thumb = append(thumb, data...)
	return propertySet(section{cfb.FMTIDSummaryInformation, []prop{
		{cfb.PIDCodePage, typed(cfb.VTI2, u16(1252))},
		{cfb.PIDSITitle, typed(cfb.VTLPSTR, lpstr("Caf\xe9 \x80"))},
		{cfb.PIDSIAuthor, typed(cfb.VTLPSTR, lpstr("A. Writer"))},
		{cfb.PIDSIEditTime, typed(cfb.VTFiletime, u64(90*60*1e7))},
		{cfb.PIDSICreateTime, typed(cfb.VTFiletime, u64(132539328000000000))},
		{cfb.PIDSIPageCount, typed(cfb.VTI4, u32(3))},
		{cfb.PIDSIThumbnail, typed(cfb.VTCF, thumb)},
		{cfb.PIDSIAppName, typed(cfb.VTLPWSTR, lpwstr("Microsoft Office Word"))},
		{cfb.PIDSIDocSecurity, typed(cfb.VTI4, u32(0))},
		{0x40, typed(0x0099, []byte("rawbytes"))},
	}})
}

// docSummary returns a DocumentSummaryInformation stream with vectors, and
// a section of user properties with a dictionary.
func docSummary() []byte {
	// The strings of the vector of TitlesOfParts: the first is padded, the
	// second is not, as some writers write them.
	titles := u32(2)
	titles = append(titles, pad4(lpstr("Intro"))...)
	titles = append(titles, lpstr("Body")...)
	heading := u32(2)
	heading = append(heading, typed(cfb.VTLPSTR, lpstr("Title"))...)
	heading = append(heading, typed(cfb.VTI4, u32(1))...)
	return propertySet(
		section{cfb.FMTIDDocSummaryInformation, []prop{
			{cfb.PIDCodePage, typed(cfb.VTI2, u16(1252))},
			{0x0C, typed(cfb.VTVector|cfb.VTVariant, heading)},
			{0x0D, typed(cfb.VTVector|cfb.VTLPSTR, titles)},
			{0x0F, typed(cfb.VTVector|cfb.VTI2, u32(3), u16(1), u16(2), u16(0xFFFF))},
			{0x10, typed(cfb.VTBool, u16(0xFFFF))},
		}},
		section{cfb.FMTIDUserDefinedProperties, []prop{
			{cfb.PIDCodePage, typed(cfb.VTI2, u16(1252))},
			{cfb.PIDDictionary, dictionary(map[uint32]string{2: "Client", 3: "Due"}, 2, 3)},
			{2, typed(cfb.VTLPSTR, lpstr("ACME"))},
			{3, typed(cfb.VTFiletime, u64(0))},
		}},
	)
}

// joinNames returns the names of es, joined with commas.
func joinNames(es []*cfb.Entry) string {
	names := make([]string, len(es))
	for i, e := range es {
		names[i] = e.Name
	}
	return strings.Join(names, ",")
}
