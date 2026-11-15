package rules

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// The table is the form of a Database that the package embeds (D35). It is
// smaller than magic.mgc, and it compresses well, because it stores each
// field as a column:
//
//	"MAGT" version uint8
//	count of set 0, count of set 1     uvarint each
//	the 32 bytes of the fixed fields of each entry, byte 0 of every entry
//	first, then byte 1 of every entry, and so on. The line number is the
//	difference from the line of the entry before.
//	then, for value, desc, MIME, Apple and ext in turn, the field of each
//	entry without its zeros at the end, as a uvarint length and the bytes
//
// The fixed fields are the first 32 bytes of Encode. A table holds every
// byte of each entry but one kind: the name of the file that libmagic tucks
// after an empty description, which nothing reads.

const (
	tableMagic   = "MAGT"
	tableVersion = 3
	fixedSize    = 32
)

// columns are the fields of an entry after the fixed ones, by their place
// in Encode.
var columns = [...][2]int{{32, 160}, {160, 224}, {224, 304}, {304, 312}, {312, 432}}

// MarshalTable writes db as a table.
func (db *Database) MarshalTable() []byte {
	var all []*Entry
	out := append([]byte(tableMagic), tableVersion)
	for _, set := range db.Sets {
		n := 0
		for _, g := range set {
			for i := range g {
				all = append(all, &g[i])
				n++
			}
		}
		out = binary.AppendUvarint(out, uint64(n))
	}
	enc := make([][EntrySize]byte, len(all))
	var prev uint32
	for i, e := range all {
		enc[i] = e.Encode()
		if e.Desc[0] == 0 {
			// The name of the file that libmagic tucks after an empty
			// description is for debugging, and nothing reads it.
			clear(enc[i][160:224])
		}
		// The line numbers grow by little from one entry to the next, so
		// their differences compress better than the numbers.
		le.PutUint32(enc[i][20:], e.Lineno-prev)
		prev = e.Lineno
	}
	for c := range fixedSize {
		for i := range enc {
			out = append(out, enc[i][c])
		}
	}
	for _, col := range columns {
		for i := range enc {
			f := enc[i][col[0]:col[1]]
			n := len(f)
			for n > 0 && f[n-1] == 0 {
				n--
			}
			out = binary.AppendUvarint(out, uint64(n))
			out = append(out, f[:n]...)
		}
	}
	return out
}

var le = binary.LittleEndian

// errTable is the error of a table that cannot be read.
var errTable = errors.New("bad table")

// UnmarshalTable reads a table that MarshalTable wrote.
func UnmarshalTable(b []byte) (*Database, error) {
	if len(b) < len(tableMagic)+1 || string(b[:len(tableMagic)]) != tableMagic {
		return nil, fmt.Errorf("reading the table: %w: no header", errTable)
	}
	if b[len(tableMagic)] != tableVersion {
		return nil, fmt.Errorf("reading the table: %w: version %d", errTable, b[len(tableMagic)])
	}
	p := len(tableMagic) + 1
	var counts [2]int
	for i := range counts {
		n, w := binary.Uvarint(b[p:])
		if w <= 0 || n > uint64(len(b)) {
			return nil, fmt.Errorf("reading the table: %w: bad count", errTable)
		}
		counts[i] = int(n)
		p += w
	}
	total := counts[0] + counts[1]
	if len(b)-p < total*fixedSize {
		return nil, fmt.Errorf("reading the table: %w: short", errTable)
	}
	enc := make([][EntrySize]byte, total)
	for c := range fixedSize {
		for i := range enc {
			enc[i][c] = b[p]
			p++
		}
	}
	for _, col := range columns {
		for i := range enc {
			n, w := binary.Uvarint(b[p:])
			if w <= 0 || n > uint64(col[1]-col[0]) || uint64(len(b)-p-w) < n {
				return nil, fmt.Errorf("reading the table: %w: bad field", errTable)
			}
			p += w
			copy(enc[i][col[0]:], b[p:p+int(n)])
			p += int(n)
		}
	}
	if p != len(b) {
		return nil, fmt.Errorf("reading the table: %w: %d bytes left", errTable, len(b)-p)
	}
	var prev uint32
	for i := range enc {
		prev += le.Uint32(enc[i][20:])
		le.PutUint32(enc[i][20:], prev)
	}
	var db Database
	k := 0
	for s, n := range counts {
		entries := make([]Entry, n)
		for i := range entries {
			entries[i] = Decode(&enc[k])
			k++
		}
		// A rule starts at each entry of level 0.
		for i := 0; i < n; {
			j := i + 1
			for j < n && entries[j].ContLevel != 0 {
				j++
			}
			db.Sets[s] = append(db.Sets[s], Group(entries[i:j:j]))
			i = j
		}
	}
	return &db, nil
}

// Decode reads an entry that Encode wrote.
func Decode(b *[EntrySize]byte) Entry {
	var e Entry
	e.Flag = Flag(le.Uint16(b[0:]))
	e.ContLevel = b[2]
	e.Factor = b[3]
	e.Reln = b[4]
	e.Vallen = b[5]
	e.Type = Type(b[6])
	e.InType = Type(b[7])
	e.InOp = b[8]
	e.MaskOp = b[9]
	e.FactorOp = b[11]
	e.Offset = int32(le.Uint32(b[12:]))
	e.InOffset = int32(le.Uint32(b[16:]))
	e.Lineno = le.Uint32(b[20:])
	e.Mask = le.Uint64(b[24:])
	copy(e.Value[:], b[32:160])
	copy(e.Desc[:], b[160:224])
	copy(e.MIME[:], b[224:304])
	copy(e.Apple[:], b[304:312])
	copy(e.Ext[:], b[312:432])
	return e
}
