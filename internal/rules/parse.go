package rules

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// Group is a rule: a line at level 0 and the lines that continue it, with
// their annotations. It is magic_entry of apprentice.c.
type Group []Entry

// Error is a line that libmagic refuses. libmagic refuses the whole database
// when one line fails, and so does Compile.
type Error struct {
	File string
	Line int
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("%s, line %d: %s", e.File, e.Line, e.Msg) }

// ParseFile reads the rules of one file, as load_1 of apprentice.c reads
// them. name is the name that libmagic tucks after the empty description of
// a line that has none, such as "magic/elf" for the file elf in the folder
// that the build compiles.
func ParseFile(name string, data []byte) ([]Group, error) {
	p := parser{file: name}
	var line []byte
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			// A last line with no newline is parsed, and does not count as
			// a line.
			line, data = data, nil
		} else {
			line, data = data[:i], data[i+1:]
			p.lineno++
		}
		if err := p.line(line); err != nil {
			return nil, err
		}
	}
	if p.cur != nil {
		p.groups = append(p.groups, p.cur)
	}
	return p.groups, nil
}

type parser struct {
	file   string
	lineno int
	cur    Group
	groups []Group
}

func (p *parser) fail(format string, args ...any) error {
	return &Error{File: p.file, Line: p.lineno, Msg: fmt.Sprintf(format, args...)}
}

// line handles one line, without its newline.
func (p *parser) line(line []byte) error {
	if len(line) == 0 || line[0] == '#' {
		return nil
	}
	if line[0] == '!' && at(line, 1) == ':' {
		return p.annotation(line)
	}
	for {
		again, err := p.parse(line)
		if err != nil || !again {
			return err
		}
		// A new rule at level 0 ends the current one, and the line is
		// read again.
		p.groups = append(p.groups, p.cur)
		p.cur = nil
	}
}

// annotation handles a line such as "!:mime image/gif". The name matches as a
// prefix, as in load_1, so "!:mimetype" is "mime" with the text "type".
func (p *parser) annotation(line []byte) error {
	rest := line[2:]
	for _, a := range []struct {
		name string
		fn   func([]byte) error
	}{
		{"mime", p.mime},
		{"apple", p.apple},
		{"ext", p.ext},
		{"strength", p.strength},
	} {
		// load_1 counts the newline in the length, so a name that ends the
		// line still matches.
		if len(rest)+1 > len(a.name) && bytes.HasPrefix(rest, []byte(a.name)) {
			if p.cur == nil {
				return p.fail("no current entry for !:%s", a.name)
			}
			return a.fn(rest[len(a.name):])
		}
	}
	return p.fail("unknown !: entry %q", line)
}

// last returns the line of the current rule that an annotation belongs to.
func (p *parser) last() *Entry { return &p.cur[len(p.cur)-1] }

func (p *parser) mime(s []byte) error {
	return p.extra(s, p.last().MIME[:], "MIME", "+-/.$?:{};=", true)
}

func (p *parser) apple(s []byte) error {
	return p.extra(s, p.last().Apple[:], "APPLE", "!+-./?", false)
}

func (p *parser) ext(s []byte) error {
	return p.extra(s, p.last().Ext[:], "EXTENSION", ",!+-/@?_$&~.", false)
}

// extra is parse_extra of apprentice.c. It copies the good characters of s
// into buf, which is a field of the entry.
func (p *parser) extra(s, buf []byte, name, extra string, nul bool) error {
	e := p.last()
	if buf[0] != 0 {
		return p.fail("the current entry already has a %s type", name)
	}
	if e.Desc[0] == 0 {
		return p.fail("the current entry does not yet have a description for a %s type", name)
	}
	l := 0
	for at(s, l) != 0 && at(s, l) < 0x80 && isSpace(at(s, l)) {
		l++
	}
	good := func(c byte) bool {
		return c < 0x80 && isAlnum(c) || c != 0 && bytes.IndexByte([]byte(extra), c) >= 0
	}
	i := 0
	for at(s, l) != 0 && i < len(buf) && good(at(s, l)) {
		buf[i] = s[l]
		i++
		l++
	}
	if i == len(buf) && at(s, l) != 0 {
		if nul {
			buf[len(buf)-1] = 0
		}
	} else if nul {
		buf[i] = 0
	}
	if i == 0 {
		return p.fail("bad magic entry %q", s)
	}
	return nil
}

// strength is parse_strength of apprentice.c. It applies to the line at
// level 0.
func (p *parser) strength(s []byte) error {
	e := &p.cur[0]
	if e.FactorOp != 0 {
		return p.fail("the current entry already has a strength type")
	}
	if e.Type == Name {
		return p.fail("strength is not supported in name entries")
	}
	l := 0
	eat := func() {
		for at(s, l) != 0 && at(s, l) < 0x80 && isSpace(at(s, l)) {
			l++
		}
	}
	eat()
	switch c := at(s, l); c {
	case 0:
	case '+', '-', '*', '/':
		e.FactorOp = c
		l++
	default:
		return p.fail("unknown factor op %q", c)
	}
	eat()
	factor, end, _ := strtoull(s, l)
	if factor > 255 {
		e.FactorOp = 0
		return p.fail("too large factor %d", factor)
	}
	if c := at(s, end); c != 0 && !isSpace(c) {
		e.FactorOp = 0
		return p.fail("bad factor %q", s[l:])
	}
	e.Factor = uint8(factor)
	if e.Factor == 0 && e.FactorOp == '/' {
		e.FactorOp, e.Factor = 0, 0
		return p.fail("cannot divide by a factor of 0")
	}
	return nil
}

// parse is parse of apprentice.c, with the checks that MAGIC_CHECK turns on,
// because the compiler of libmagic turns it on. It returns again when the
// line starts a new rule while one is open.
func (p *parser) parse(line []byte) (again bool, err error) {
	l := 0
	level := 0
	for at(line, l) == '>' {
		l++
		level++
	}
	var e *Entry
	if level != 0 {
		if p.cur == nil {
			return false, p.fail("no current entry for a continuation")
		}
		p.cur = append(p.cur, Entry{})
		e = &p.cur[len(p.cur)-1]
		e.ContLevel = uint8(level)
	} else {
		if p.cur != nil {
			return true, nil
		}
		p.cur = Group{{}}
		e = &p.cur[0]
	}
	e.Lineno = uint32(p.lineno)

	if at(line, l) == '&' {
		l++
		e.Flag |= FlagOffAdd
	}
	if at(line, l) == '(' {
		l++
		e.Flag |= FlagIndir
		if e.Flag&FlagOffAdd != 0 {
			e.Flag = e.Flag&^FlagOffAdd | FlagIndirOffAdd
		}
		if at(line, l) == '&' {
			l++
			e.Flag |= FlagOffAdd
		}
	}
	if e.ContLevel == 0 && e.Flag&(FlagOffAdd|FlagIndirOffAdd) != 0 {
		return false, p.fail("relative offset at level 0")
	}
	if c := at(line, l); c == '-' || c == '+' {
		l++
		if c == '-' {
			e.Flag |= FlagOffNegative
		} else {
			e.Flag |= FlagOffPositive
		}
	}
	off, end, _ := strtol(line, l)
	if end == l {
		return false, p.fail("offset %q invalid", line[l:])
	}
	e.Offset = int32(off)
	l = end

	if e.Flag&FlagIndir != 0 {
		if l, err = p.indirect(e, line, l); err != nil {
			return false, err
		}
	}
	l = eatab(line, l)

	if l, err = p.typ(e, line, l); err != nil {
		return false, err
	}
	if e.Type == Name && level != 0 {
		return false, p.fail("name entries can only be declared at top level")
	}

	if at(line, l) == '~' {
		if !e.Type.IsString() {
			e.MaskOp |= OpInverse
		}
		l++
	}
	e.Mask = 0
	if e.Type == PString {
		e.setStrFlags(PString1LE)
	}
	if op, ok := getOp(at(line, l)); ok {
		if e.Type.IsString() {
			if op != OpDivide {
				return false, p.fail("invalid string or indirect op %q", at(line, l))
			}
			if e.Type == Indirect {
				l, err = p.indirectModifier(e, line, l)
			} else {
				l, err = p.stringModifier(e, line, l)
			}
			if err != nil {
				return false, err
			}
		} else {
			l = opModifier(e, line, l, op)
		}
	}
	l = eatab(line, l)

	switch c := at(line, l); c {
	case '>', '<':
		e.Reln = c
		l++
		if at(line, l) == '=' {
			return false, p.fail("%c= not supported", c)
		}
	case '&', '^', '=':
		e.Reln = c
		l++
		if at(line, l) == '=' {
			// HP compatibility: &= and the like mean & and the like.
			l++
		}
	case '!':
		e.Reln = c
		l++
	default:
		e.Reln = '='
		if c == 'x' && (isSpace(at(line, l+1)) && at(line, l+1) < 0x80 || at(line, l+1) == 0) {
			e.Reln = 'x'
			l++
		}
	}

	if e.Reln != 'x' {
		if at(line, l) == 0 {
			return false, p.fail("incomplete magic %q", line)
		}
		if l, err = p.value(e, line, l); err != nil {
			return false, err
		}
	}

	l = eatab(line, l)
	switch {
	case at(line, l) == '\b':
		l++
		e.Flag |= FlagNoSpace
	case at(line, l) == '\\' && at(line, l+1) == 'b':
		l += 2
		e.Flag |= FlagNoSpace
	}
	desc := line[min(l, len(line)):]
	if n := copy(e.Desc[:], desc); n == len(e.Desc) {
		// The description is cut at 63 bytes, with a NUL at the end.
		e.Desc[len(e.Desc)-1] = 0
	}
	if e.Desc[0] == 0 {
		// libmagic tucks the name of the file after an empty description,
		// for debugging. It is part of the entry, and of its order.
		copy(e.Desc[1:len(e.Desc)-1], p.file)
	}
	if err := checkFormat(e); err != nil {
		return false, p.fail("%v", err)
	}
	return false, nil
}

// eatab skips spaces, as EATAB of apprentice.c.
func eatab(s []byte, l int) int {
	for at(s, l) != 0 && at(s, l) < 0x80 && isSpace(at(s, l)) {
		l++
	}
	return l
}

// getOp is get_op of apprentice.c.
func getOp(c byte) (uint8, bool) {
	switch c {
	case '&':
		return OpAnd, true
	case '|':
		return OpOr, true
	case '^':
		return OpXor, true
	case '+':
		return OpAdd, true
	case '-':
		return OpMinus, true
	case '*':
		return OpMultiply, true
	case '/':
		return OpDivide, true
	case '%':
		return OpModulo, true
	}
	return 0, false
}

// indirect reads the rest of an indirect offset, such as ".l+4)".
func (p *parser) indirect(e *Entry, line []byte, l int) (int, error) {
	e.InType = Long
	if c := at(line, l); c == '.' || c == ',' {
		if c == ',' {
			e.InOp |= OpSigned
		}
		l++
		switch at(line, l) {
		case 'l':
			e.InType = LELong
		case 'L':
			e.InType = BELong
		case 'm':
			e.InType = MELong
		case 'h', 's':
			e.InType = LEShort
		case 'H', 'S':
			e.InType = BEShort
		case 'c', 'b', 'C', 'B':
			e.InType = Byte
		case 'e', 'f', 'g':
			e.InType = LEDouble
		case 'E', 'F', 'G':
			e.InType = BEDouble
		case 'i':
			e.InType = LEID3
		case 'I':
			e.InType = BEID3
		case 'o':
			e.InType = Octal
		case 'q':
			e.InType = LEQuad
		case 'Q':
			e.InType = BEQuad
		default:
			return l, p.fail("indirect offset type %q invalid", at(line, l))
		}
		l++
	}
	if at(line, l) == '~' {
		e.InOp |= OpInverse
		l++
	}
	if op, ok := getOp(at(line, l)); ok {
		e.InOp |= op
		l++
	}
	if at(line, l) == '(' {
		e.InOp |= OpIndirect
		l++
	}
	if isDigit(at(line, l)) || at(line, l) == '-' {
		v, end, _ := strtol(line, l)
		if end == l {
			return l, p.fail("in_offset %q invalid", line[l:])
		}
		e.InOffset = int32(v)
		l = end
	}
	if at(line, l) != ')' {
		return l, p.fail("missing ')' in indirect offset")
	}
	l++
	if e.InOp&OpIndirect != 0 {
		if at(line, l) != ')' {
			return l, p.fail("missing ')' in indirect offset")
		}
		l++
	}
	return l, nil
}

// lookup finds the first name of tbl that is a prefix of s at l, as get_type
// of apprentice.c.
func lookup(tbl []typeName, s []byte, l int) (Type, int) {
	for _, t := range tbl {
		if bytes.HasPrefix(s[min(l, len(s)):], []byte(t.name)) {
			return t.typ, l + len(t.name)
		}
	}
	return Invalid, l
}

// typ reads the type of a line, as the part of parse that starts "Parse the
// type".
func (p *parser) typ(e *Entry, line []byte, l int) (int, error) {
	if at(line, l) == 'u' {
		t, end := lookup(typeNames, line, l+1)
		if t == Invalid {
			t, end = standardInteger(line, l)
		}
		e.Type = t
		l = end
		if t != Invalid {
			e.Flag |= FlagUnsigned
		}
	} else {
		t, end := lookup(typeNames, line, l)
		if t == Invalid {
			switch {
			case at(line, l) == 'd':
				t, end = standardInteger(line, l)
			case at(line, l) == 's' && !isAlpha(at(line, l+1)):
				t, end = String, l+1
			}
		}
		e.Type = t
		l = end
	}
	if e.Type == Invalid {
		e.Type, l = lookup(specialNames, line, l)
	}
	if e.Type == Invalid {
		return l, p.fail("type %q invalid", line[min(l, len(line)):])
	}
	return l, nil
}

// standardInteger is get_standard_integer_type of apprentice.c: d, u, dC, uS,
// d4 and the like.
func standardInteger(s []byte, l int) (Type, int) {
	c := at(s, l+1)
	switch {
	case at(s, l) == 0 || c == 0:
		return Invalid, l
	case isAlpha(c):
		var t Type
		switch c {
		case 'C':
			t = Byte
		case 'S':
			t = Short
		case 'I', 'L':
			t = Long
		case 'Q':
			t = Quad
		default:
			return Invalid, l
		}
		return t, l + 2
	case isDigit(c):
		if isDigit(at(s, l+2)) {
			return Invalid, l
		}
		var t Type
		switch c {
		case '1':
			t = Byte
		case '2':
			t = Short
		case '4':
			t = Long
		case '8':
			t = Quad
		default:
			return Invalid, l
		}
		return t, l + 2
	}
	return Long, l + 1
}

// opModifier is parse_op_modifier of apprentice.c: the mask of a number,
// such as &0x7f.
func opModifier(e *Entry, line []byte, l int, op uint8) int {
	l++
	e.MaskOp |= op
	v, end, _ := strtoull(line, l)
	e.Mask = signExtend(e, v)
	return eatSize(line, end)
}

// eatSize skips the size of a number, such as the UL of 10UL.
func eatSize(s []byte, l int) int {
	if lower(at(s, l)) == 'u' {
		l++
	}
	switch lower(at(s, l)) {
	case 'l', 's', 'h', 'b', 'c':
		l++
	}
	return l
}

// signExtend is file_signextend of apprentice.c.
func signExtend(e *Entry, v uint64) uint64 {
	if e.Flag&FlagUnsigned != 0 {
		return v
	}
	switch e.Type {
	case Byte:
		return uint64(int64(int8(v)))
	case Short, BEShort, LEShort:
		return uint64(int64(int16(v)))
	case Date, BEDate, LEDate, MEDate, LDate, BELDate, LELDate, MELDate,
		Long, BELong, LELong, MELong, Float, BEFloat, LEFloat,
		MSDOSDate, BEMSDOSDate, LEMSDOSDate, MSDOSTime, BEMSDOSTime, LEMSDOSTime:
		return uint64(int64(int32(v)))
	}
	return v
}

// indirectModifier is parse_indirect_modifier of apprentice.c.
func (p *parser) indirectModifier(e *Entry, line []byte, l int) (int, error) {
	for {
		l++
		c := at(line, l)
		if isSpace(c) {
			return l, nil
		}
		if c != 'r' {
			return l, p.fail("indirect modifier %q invalid", c)
		}
		e.setStrFlags(e.StrFlags() | IndirectRelative)
	}
}

// stringModifier is parse_string_modifier of apprentice.c, such as the /cW of
// string/cW.
func (p *parser) stringModifier(e *Entry, line []byte, l int) (int, error) {
	setLen := func(v uint32) { e.setStrFlags(e.StrFlags()&^pstringLen | v) }
	for {
		l++
		c := at(line, l)
		if isSpace(c) {
			break
		}
		flag := uint32(0)
		switch c {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			v, end, _ := strtoull(line, l)
			e.setStrRange(uint32(v))
			l = end - 1
		case 'W':
			flag = StrCompactWhitespace
		case 'w':
			flag = StrCompactOptionalWhitespace
		case 'c':
			flag = StrIgnoreLowercase
		case 'C':
			flag = StrIgnoreUppercase
		case 's':
			flag = RegexOffsetStart
		case 'b':
			flag = StrBinTest
		case 't':
			flag = StrTextTest
		case 'T':
			flag = StrTrim
		case 'f':
			flag = StrFullWord
		case 'B', 'H', 'h', 'L':
			if e.Type != PString {
				return l, p.fail("string modifier %q invalid", c)
			}
			setLen(map[byte]uint32{'B': PString1LE, 'H': PString2BE, 'h': PString2LE, 'L': PString4BE}[c])
		case 'l':
			if e.Type != PString && e.Type != Regex {
				return l, p.fail("string modifier %q invalid", c)
			}
			setLen(PString4LE)
		case 'J':
			if e.Type != PString {
				return l, p.fail("string modifier %q invalid", c)
			}
			flag = PStringLengthIncludesItself
		default:
			return l, p.fail("string modifier %q invalid", c)
		}
		e.setStrFlags(e.StrFlags() | flag)
		// Several slashes are allowed, for readability.
		if at(line, l+1) == '/' && !isSpace(at(line, l+2)) {
			l++
		}
	}
	return l, p.checkStringModifier(e)
}

// checkStringModifier is string_modifier_check of apprentice.c.
func (p *parser) checkStringModifier(e *Entry) error {
	f := e.StrFlags()
	if (e.Type != Regex || f&RegexLineCount == 0) && e.Type != PString && f&pstringLen != 0 {
		return p.fail("'/BHhLl' modifiers are only allowed for pascal strings")
	}
	switch e.Type {
	case BEString16, LEString16:
		if f != 0 {
			return p.fail("no modifiers allowed for 16-bit strings")
		}
	case String, PString:
		if f&RegexOffsetStart != 0 {
			return p.fail("'/s' only allowed on regex and search")
		}
	case Search:
		if e.StrRange() == 0 {
			e.setStrRange(100)
			return p.fail("missing range; defaulting to 100")
		}
	case Regex:
		if f&(StrCompactWhitespace|StrCompactOptionalWhitespace) != 0 {
			return p.fail("'/W' and '/w' are not allowed on regex")
		}
	default:
		return p.fail("coding error: type %v", e.Type)
	}
	return nil
}

// value is getvalue of apprentice.c.
func (p *parser) value(e *Entry, line []byte, l int) (int, error) {
	switch e.Type {
	case BEString16, LEString16, String, PString, Regex, Search, Name, Use, DER, Octal:
		end, err := p.str(e, line, l)
		if err != nil {
			return l, err
		}
		// libmagic compiles a regular expression here, and refuses the rule
		// if it does not compile. The port compiles it when it matches.
		return end, nil
	}
	if e.Reln == 'x' {
		return l, nil
	}
	switch e.Type {
	case Float, BEFloat, LEFloat:
		v, end, ok := strtod(line, l, 32)
		binary.LittleEndian.PutUint32(e.Value[:], math.Float32bits(float32(v)))
		if ok {
			l = end
		}
		return l, nil
	case Double, BEDouble, LEDouble:
		v, end, ok := strtod(line, l, 64)
		binary.LittleEndian.PutUint64(e.Value[:], math.Float64bits(v))
		if ok {
			l = end
		}
		return l, nil
	case BEGUID, LEGUID, GUID:
		if !parseGUID(line[min(l, len(line)):], e.Value[:16]) {
			return l, p.fail("error parsing guid %q", line[l:])
		}
		return l + 36, nil
	}
	ull, end, ok := strtoull(line, l)
	binary.LittleEndian.PutUint64(e.Value[:], signExtend(e, ull))
	if end == l {
		return l, p.fail("unparsable number %q", line[l:])
	}
	ts := e.Type.size()
	if ts == 0 {
		return l, p.fail("expected numeric type, got %v", e.Type)
	}
	q := l
	for isSpace(at(line, q)) {
		q++
	}
	if at(line, q) == '-' && ull != math.MaxUint64 {
		ull = -ull
	}
	var x uint64
	var y bool
	switch ts {
	case 1:
		x = ull &^ 0xff
		y = x&^0xff != ^uint64(0xff)
	case 2:
		x = ull &^ 0xffff
		y = x&^0xffff != ^uint64(0xffff)
	case 4:
		x = ull &^ 0xffffffff
		y = x&^0xffffffff != ^uint64(0xffffffff)
	}
	if x != 0 && y {
		return l, p.fail("overflow for numeric type %v value %#x", e.Type, ull)
	}
	if ok {
		l = eatSize(line, end)
	}
	return l, nil
}

// parseGUID is file_parse_guid of funcs.c, which stores the GUID in the
// layout of struct guid on a little-endian machine.
func parseGUID(s []byte, out []byte) bool {
	hex := func(i, n int) (uint64, bool) {
		var v uint64
		for k := range n {
			d := digit(at(s, i+k), 16)
			if d < 0 {
				return 0, false
			}
			v = v<<4 | uint64(d)
		}
		return v, true
	}
	d1, ok1 := hex(0, 8)
	if !ok1 || at(s, 8) != '-' {
		return false
	}
	d2, ok2 := hex(9, 4)
	if !ok2 || at(s, 13) != '-' {
		return false
	}
	d3, ok3 := hex(14, 4)
	if !ok3 || at(s, 18) != '-' {
		return false
	}
	binary.LittleEndian.PutUint32(out[0:], uint32(d1))
	binary.LittleEndian.PutUint16(out[4:], uint16(d2))
	binary.LittleEndian.PutUint16(out[6:], uint16(d3))
	for k, i := range []int{19, 21, 24, 26, 28, 30, 32, 34} {
		v, ok := hex(i, 2)
		if !ok {
			return false
		}
		out[8+k] = byte(v)
	}
	return at(s, 23) == '-'
}

// str is getstr of apprentice.c: it reads a string with C escapes, up to an
// unescaped space, into Value, and sets Vallen.
func (p *parser) str(e *Entry, line []byte, l int) (int, error) {
	out := e.Value[:0]
	for {
		c := at(line, l)
		if c == 0 {
			break
		}
		l++
		if isSpace(c) {
			l--
			break
		}
		if len(out) >= len(e.Value)-1 {
			return l, p.fail("string too long: %q", line)
		}
		if c != '\\' {
			out = append(out, c)
			continue
		}
		c = at(line, l)
		l++
		switch c {
		case 0:
			// An escape at the end of the line ends the string.
			l--
		case 'a':
			out = append(out, '\a')
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'v':
			out = append(out, '\v')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			v := int(c - '0')
			if d := at(line, l); d >= '0' && d <= '7' {
				v = v<<3 | int(d-'0')
				l++
				if d := at(line, l); d >= '0' && d <= '7' {
					v = v<<3 | int(d-'0')
					l++
				}
			}
			out = append(out, byte(v))
		case 'x':
			v := int('x')
			if d := hexDigit(at(line, l)); d >= 0 {
				v = d
				l++
				if d := hexDigit(at(line, l)); d >= 0 {
					v = v<<4 + d
					l++
				}
			}
			out = append(out, byte(v))
		default:
			// Any other escaped character is itself, such as \ \< or \\.
			out = append(out, c)
		}
		if c == 0 {
			break
		}
	}
	n := len(out)
	clear(e.Value[n:])
	e.Vallen = uint8(n)
	if e.Type == PString {
		size := pstringLengthSize(e)
		if size == 0 {
			return l, p.fail("bad pascal string length %d", e.StrFlags()&pstringLen)
		}
		if n+size > len(e.Value) {
			return l, p.fail("pascal string too long")
		}
		e.Vallen += uint8(size)
	}
	return l, nil
}

// hexDigit is hextoint of apprentice.c.
func hexDigit(c byte) int {
	if c >= 0x80 {
		return -1
	}
	return digit(c, 16)
}

// pstringLengthSize is file_pstring_length_size of apprentice.c, with 0 for a
// bad size.
func pstringLengthSize(e *Entry) int {
	switch e.StrFlags() & pstringLen {
	case PString1LE:
		return 1
	case PString2LE, PString2BE:
		return 2
	case PString4LE, PString4BE:
		return 4
	}
	return 0
}
