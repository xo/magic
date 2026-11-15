// Package rules compiles the rules of file, the text of magic/Magdir, into
// the entries that the engine of the port matches (D35).
//
// The compiler gives the same entries as the compiler of libmagic 5.48, in
// the same order, field by field. Its tests compare the result with the
// magic.mgc that the oracle built (D30), byte for byte.
package rules

// Type is the type of a rule, such as byte, belong or string. The values are
// the values of FILE_* in file.h of file 5.48, because the compiled entries
// hold them.
type Type uint8

// The types of file 5.48.
const (
	Invalid Type = iota
	Byte
	Short
	Default
	Long
	String
	Date
	BEShort
	BELong
	BEDate
	LEShort
	LELong
	LEDate
	PString
	LDate
	BELDate
	LELDate
	Regex
	BEString16
	LEString16
	Search
	MEDate
	MELDate
	MELong
	Quad
	LEQuad
	BEQuad
	QDate
	LEQDate
	BEQDate
	QLDate
	LEQLDate
	BEQLDate
	Float
	BEFloat
	LEFloat
	Double
	BEDouble
	LEDouble
	BEID3
	LEID3
	Indirect
	QWDate
	LEQWDate
	BEQWDate
	Name
	Use
	Clear
	DER
	GUID
	LEGUID
	BEGUID
	Offset
	BEVarint
	LEVarint
	MSDOSDate
	LEMSDOSDate
	BEMSDOSDate
	MSDOSTime
	LEMSDOSTime
	BEMSDOSTime
	Octal
)

// typeName is one row of the table of types. The order is the order of
// type_tbl in apprentice.c, because a type is found by the first name that is
// a prefix of the text.
type typeName struct {
	name string
	typ  Type
}

// typeNames is type_tbl of apprentice.c, in its order. The order is not the
// order of the values: leid3 comes before beid3.
var typeNames = []typeName{
	{"invalid", Invalid},
	{"byte", Byte},
	{"short", Short},
	{"default", Default},
	{"long", Long},
	{"string", String},
	{"date", Date},
	{"beshort", BEShort},
	{"belong", BELong},
	{"bedate", BEDate},
	{"leshort", LEShort},
	{"lelong", LELong},
	{"ledate", LEDate},
	{"pstring", PString},
	{"ldate", LDate},
	{"beldate", BELDate},
	{"leldate", LELDate},
	{"regex", Regex},
	{"bestring16", BEString16},
	{"lestring16", LEString16},
	{"search", Search},
	{"medate", MEDate},
	{"meldate", MELDate},
	{"melong", MELong},
	{"quad", Quad},
	{"lequad", LEQuad},
	{"bequad", BEQuad},
	{"qdate", QDate},
	{"leqdate", LEQDate},
	{"beqdate", BEQDate},
	{"qldate", QLDate},
	{"leqldate", LEQLDate},
	{"beqldate", BEQLDate},
	{"float", Float},
	{"befloat", BEFloat},
	{"lefloat", LEFloat},
	{"double", Double},
	{"bedouble", BEDouble},
	{"ledouble", LEDouble},
	{"leid3", LEID3},
	{"beid3", BEID3},
	{"indirect", Indirect},
	{"qwdate", QWDate},
	{"leqwdate", LEQWDate},
	{"beqwdate", BEQWDate},
	{"name", Name},
	{"use", Use},
	{"clear", Clear},
	{"der", DER},
	{"guid", GUID},
	{"leguid", LEGUID},
	{"beguid", BEGUID},
	{"offset", Offset},
	{"bevarint", BEVarint},
	{"levarint", LEVarint},
	{"msdosdate", MSDOSDate},
	{"lemsdosdate", LEMSDOSDate},
	{"bemsdosdate", BEMSDOSDate},
	{"msdostime", MSDOSTime},
	{"lemsdostime", LEMSDOSTime},
	{"bemsdostime", BEMSDOSTime},
	{"octal", Octal},
}

// specialNames is special_tbl of apprentice.c: the names that are not types
// and cannot take the prefix "u".
var specialNames = []typeName{
	{"der", DER},
	{"name", Name},
	{"use", Use},
	{"octal", Octal},
}

// names holds the name of each type, by its value.
var names = func() [Octal + 1]string {
	var out [Octal + 1]string
	for _, n := range typeNames {
		out[n.typ] = n.name
	}
	return out
}()

// String returns the name of t, as the rules write it.
func (t Type) String() string {
	if t <= Octal {
		return names[t]
	}
	return "invalid"
}

// IsString reports whether t holds a string value, as IS_STRING of file.h.
func (t Type) IsString() bool {
	switch t {
	case String, PString, BEString16, LEString16, Regex, Search, Indirect, Name, Use, Octal:
		return true
	}
	return false
}

// size returns the number of bytes that a value of t has, as typesize in
// apprentice.c, or 0 when t has no fixed size.
func (t Type) size() int {
	switch t {
	case Byte:
		return 1
	case Short, LEShort, BEShort, MSDOSDate, BEMSDOSDate, LEMSDOSDate,
		MSDOSTime, BEMSDOSTime, LEMSDOSTime:
		return 2
	case Long, LELong, BELong, MELong, Date, LEDate, BEDate, MEDate, LDate,
		LELDate, BELDate, MELDate, Float, BEFloat, LEFloat, BEID3, LEID3:
		return 4
	case Quad, BEQuad, LEQuad, QDate, LEQDate, BEQDate, QLDate, LEQLDate,
		BEQLDate, QWDate, LEQWDate, BEQWDate, Double, BEDouble, LEDouble,
		Offset, BEVarint, LEVarint:
		return 8
	case LEGUID, BEGUID, GUID:
		return 16
	}
	return 0
}
