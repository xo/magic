package engine

import (
	"bytes"
	"io"
	"math"
	"strconv"

	"github.com/xo/magic/internal/rules"
)

// This file is what softmagic.c does: it runs the compiled rules on a
// buffer. The order of the reads, the offsets and the state that one rule
// leaves for the next are the ones of libmagic, because the output depends
// on them.

// The limits of a magic_set, from file.h.
const (
	indirMax    = 50
	nameMax     = 150
	regexMax    = 8192
	bytesMax    = 7 * 1024 * 1024
	encodingMax = 64 * 1024
)

// separator is FILE_SEPARATOR, which goes between two results.
const separator = "\n- "

// levelInfo is struct level_info: what one level of continuation remembers.
type levelInfo struct {
	off      int32
	gotMatch bool
}

// buffer is struct buffer: the bytes that the rules read. data is nil for a
// buffer that libmagic would leave NULL. file is set when the bytes came
// from a file, which libmagic can read again for an offset from the end.
type buffer struct {
	data []byte
	file *fileInfo

	// ebuf is the end of the file, which fill reads the first time a rule
	// needs it.
	ebuf    []byte
	efilled bool
	ebad    bool
}

// fileInfo is what libmagic knows of an open file.
type fileInfo struct {
	regular bool
	size    int64
	// r reads the file, for a detector that reads past the buffer.
	r io.ReaderAt
	// tail holds the last bytes of the file, as many as bytesMax.
	tail []byte
}

// fill is buffer_fill: it reads the end of the file, as many bytes as the
// buffer holds. It returns false for a file that is not regular.
func (b *buffer) fill() bool {
	if b.efilled {
		return !b.ebad
	}
	if b.file == nil {
		return true
	}
	if !b.file.regular {
		b.efilled, b.ebad = true, true
		return false
	}
	n := min(int(b.file.size), len(b.data))
	if n == 0 {
		b.ebuf = nil
		return true
	}
	t := b.file.tail
	b.ebuf = t[len(t)-min(n, len(t)):]
	b.efilled = true
	return true
}

// elen is the length of the end of the file that fill read.
func (b *buffer) elen() int { return len(b.ebuf) }

// table is one set of compiled rules, as one mlist of libmagic.
type table struct {
	entries []rules.Entry
	rx      []regexSlot
}

// matchVars are the variables that the calls of match share through
// pointers in libmagic.
type matchVars struct {
	printed   bool // printed_something
	needSep   bool // need_separator
	firstline bool
}

// match is match of softmagic.c. It tries the n entries of t from start, a
// rule and its continuations at a time, on b at offset o. It returns -1 on
// an error, and otherwise the value of returnval.
func (st *state) match(t *table, start, n int, b *buffer, o uint64, mode rules.Flag, text, flip bool,
	ic, nc *uint16, mv *matchVars, returnval *int, found *bool) int {
	var bb buffer
	var rv0 int
	var found0 bool
	if returnval == nil {
		returnval = &rv0
	}
	if found == nil {
		found = &found0
	}
	print := st.flags&NoDesc == 0
	contLevel := 0
	st.checkMem(contLevel)

	for mi := 0; mi < n; mi++ {
		m := &t.entries[start+mi]
		flush := false

		skip := func() {
			// Skip the continuations of this rule.
			for mi < n-1 && t.entries[start+mi+1].ContLevel != 0 {
				mi++
			}
			contLevel = 0
		}

		if m.Type != rules.Name {
			f := m.StrFlags() & (rules.StrBinTest | rules.StrTextTest)
			if m.Type.IsString() && (text && f == rules.StrBinTest || !text && f == rules.StrTextTest) ||
				m.Flag&mode != mode {
				skip()
				continue
			}
		}
		if !st.setOffset(m, &bb, b, o, contLevel) {
			if b.ebad {
				continue
			}
			skip()
			continue
		}

		switch st.mget(t, start+mi, b, &bb, o, contLevel, mode, text, flip, ic, nc, mv, returnval, found) {
		case -1:
			return -1
		case 0:
			flush = m.Reln != '!'
		default:
			if m.Type == rules.Indirect {
				*found = true
				*returnval = 1
			}
			switch st.check(t, start+mi) {
			case -1:
				return -1
			case 0:
				flush = true
			default:
				flush = false
			}
		}
		if flush {
			skip()
			continue
		}

		if e := st.annotate(m, mv.firstline); e != 0 {
			*found = true
			mv.needSep = true
			mv.printed = true
			*returnval = 1
			mv.firstline = false
			return e
		}

		if m.Desc[0] != 0 {
			*found = true
			if print {
				*returnval = 1
				mv.needSep = true
				mv.printed = true
				st.printSep(mv.firstline)
				if st.mprint(m) == -1 {
					return -1
				}
			}
		}

		if st.moffset(m, len(bb.data), o, &st.li[contLevel].off) <= 0 {
			skip()
			continue
		}

		contLevel++
		st.checkMem(contLevel)

		for mi+1 < n && t.entries[start+mi+1].ContLevel != 0 {
			mi++
			m = &t.entries[start+mi]
			if contLevel < int(m.ContLevel) {
				continue
			}
			if contLevel > int(m.ContLevel) {
				// The end of the continuations at level contLevel.
				contLevel = int(m.ContLevel)
			}
			if !st.setOffset(m, &bb, b, o, contLevel) {
				if b.ebad {
					continue
				}
				// goto flush of the C code: skip the rest of the rule.
				skip()
				goto next
			}
			if m.Flag&rules.FlagOffAdd != 0 {
				if contLevel == 0 {
					return 0
				}
				st.offset += uint32(st.li[contLevel-1].off)
			}

			flush = false
			switch st.mget(t, start+mi, b, &bb, o, contLevel, mode, text, flip, ic, nc, mv, returnval, found) {
			case -1:
				return -1
			case 0:
				if m.Reln != '!' {
					continue
				}
				flush = true
			default:
				if m.Type == rules.Indirect {
					*found = true
					*returnval = 1
				}
			}

			r := 1
			if !flush {
				r = st.check(t, start+mi)
			}
			switch r {
			case -1:
				return -1
			case 0:
			default:
				if m.Type == rules.Clear {
					st.li[contLevel].gotMatch = false
				} else if st.li[contLevel].gotMatch {
					if m.Type == rules.Default {
						break
					}
				} else {
					st.li[contLevel].gotMatch = true
				}

				if e := st.annotate(m, mv.firstline); e != 0 {
					*found = true
					mv.needSep = true
					mv.printed = true
					*returnval = 1
					mv.firstline = false
					return e
				}
				if m.Desc[0] != 0 {
					*found = true
				}
				if print && m.Desc[0] != 0 {
					*returnval = 1
					if !mv.printed {
						mv.printed = true
						st.printSep(mv.firstline)
					}
					if mv.needSep && m.Flag&rules.FlagNoSpace == 0 {
						st.printf(" ")
					}
					if st.mprint(m) == -1 {
						return -1
					}
					mv.needSep = true
				}

				if st.moffset(m, len(bb.data), o, &st.li[contLevel].off) <= 0 {
					contLevel--
				}
				// Try the continuations at the next level.
				contLevel++
				st.checkMem(contLevel)
			}
		}
		if mv.printed {
			mv.firstline = false
		}
		if *found {
			if st.flags&Continue == 0 {
				return *returnval
			}
			// So that a separator is printed.
			mv.printed = false
			mv.firstline = false
		}
		contLevel = 0
	next:
	}
	return *returnval
}

// checkMem is file_check_mem: it makes room for the level and clears its
// match.
func (st *state) checkMem(level int) {
	if level >= len(st.li) {
		st.li = append(st.li, make([]levelInfo, 20+level-len(st.li))...)
	}
	st.li[level].gotMatch = false
}

// setOffset is msetoffset: it sets the offset of m, and the buffer bb that
// m reads. It returns false when m cannot be read.
func (st *state) setOffset(m *rules.Entry, bb, b *buffer, o uint64, contLevel int) bool {
	if m.Flag&rules.FlagOffNegative != 0 && !(contLevel > 0 && m.Flag&(rules.FlagOffAdd|rules.FlagIndirOffAdd) != 0) {
		if !b.fill() {
			return false
		}
		if o != 0 {
			st.magErr("non zero offset %d at level %d", o, contLevel)
			return false
		}
		if b.file == nil {
			if uint64(uint32(m.Offset)) > uint64(len(b.data)) {
				return false
			}
			st.offset = uint32(len(b.data)) - uint32(m.Offset)
			st.eoffset = st.offset
		} else {
			if uint64(uint32(m.Offset)) > uint64(b.elen()) {
				return false
			}
			*bb = buffer{data: b.ebuf}
			st.offset = uint32(b.elen()) - uint32(m.Offset)
			st.eoffset = st.offset
		}
		return true
	}
	offset := m.Offset
	if m.Flag&rules.FlagOffNegative != 0 {
		// A negative offset that is relative to a match, as offset -m.Offset
		// of libmagic.
		offset = -m.Offset
	}
	if m.Flag&rules.FlagOffPositive != 0 || contLevel == 0 || m.Flag&rules.FlagOffNegative != 0 {
		*bb = buffer{data: b.data}
		st.offset = uint32(offset)
		st.eoffset = 0
	} else {
		st.offset = st.eoffset + uint32(offset)
	}
	return true
}

// offsetOOB is offset_oob: whether i bytes at o fall out of n bytes.
func offsetOOB(n int, o int64, i int) bool {
	if o < 0 || uint64(o) > uint64(n) {
		return true
	}
	return uint64(i) > uint64(n)-uint64(o)
}

// sext is SEXT: v as a signed or an unsigned number of bits.
func sext(signed bool, bits int, v uint64) int64 {
	if !signed {
		return int64(v & (1<<bits - 1))
	}
	switch bits {
	case 8:
		return int64(int8(v))
	case 16:
		return int64(int16(v))
	case 32:
		return int64(int32(v))
	}
	return int64(v)
}

// cvtFlip is cvt_flip: the type with the other byte order, for a use with
// '^'.
func cvtFlip(t rules.Type, flip bool) rules.Type {
	if !flip {
		return t
	}
	switch t {
	case rules.BEShort:
		return rules.LEShort
	case rules.BELong:
		return rules.LELong
	case rules.BEDate:
		return rules.LEDate
	case rules.BELDate:
		return rules.LELDate
	case rules.BEQuad:
		return rules.LEQuad
	case rules.BEQDate:
		return rules.LEQDate
	case rules.BEQLDate:
		return rules.LEQLDate
	case rules.BEQWDate:
		return rules.LEQWDate
	case rules.LEShort:
		return rules.BEShort
	case rules.LELong:
		return rules.BELong
	case rules.LEDate:
		return rules.BEDate
	case rules.LELDate:
		return rules.BELDate
	case rules.LEQuad:
		return rules.BEQuad
	case rules.LEQDate:
		return rules.BEQDate
	case rules.LEQLDate:
		return rules.BEQLDate
	case rules.LEQWDate:
		return rules.BEQWDate
	case rules.BEFloat:
		return rules.LEFloat
	case rules.LEFloat:
		return rules.BEFloat
	case rules.BEDouble:
		return rules.LEDouble
	case rules.LEDouble:
		return rules.BEDouble
	case rules.BEMSDOSDate:
		return rules.LEMSDOSDate
	case rules.LEMSDOSDate:
		return rules.BEMSDOSDate
	case rules.BEMSDOSTime:
		return rules.LEMSDOSTime
	case rules.LEMSDOSTime:
		return rules.BEMSDOSTime
	}
	return t
}

// cvtID3 is cvt_id3: an ID3 size, 7 bits in each byte.
func cvtID3(v uint32) uint32 {
	return v&0x7f | (v>>8)&0x7f<<7 | (v>>16)&0x7f<<14 | (v>>24)&0x7f<<21
}

// mcopy is mcopy: it copies the bytes of a value at offset into the value,
// or points the search at them.
func (st *state) mcopy(typ rules.Type, indir bool, s []byte, offset, nbytes uint32, m *rules.Entry) {
	size := uint32(len(st.val))
	if !indir {
		switch typ {
		case rules.DER, rules.Search:
			if offset > nbytes {
				offset = nbytes
			}
			st.search.null = s == nil
			st.search.s = s[offset:]
			st.search.sLen = uint64(nbytes - offset)
			st.search.offset = uint64(offset)
			return
		case rules.Regex:
			if s == nil || nbytes < offset {
				st.search.sLen = 0
				st.search.s = nil
				st.search.null = true
				return
			}
			var linecnt, bytecnt uint64
			if m.StrFlags()&rules.RegexLineCount != 0 {
				linecnt = uint64(m.StrRange())
				bytecnt = linecnt * 80
			} else {
				bytecnt = uint64(m.StrRange())
			}
			if bytecnt == 0 || bytecnt > uint64(nbytes-offset) {
				bytecnt = uint64(nbytes - offset)
			}
			if bytecnt > regexMax {
				bytecnt = regexMax
			}
			buf := s[offset:]
			end := int(bytecnt)
			last := end
			// Cut the region after linecnt lines.
			lines := linecnt
			p := 0
			for lines > 0 && p < end {
				c := p
				nl := bytes.IndexByte(buf[c:end], '\n')
				if nl < 0 {
					nl = bytes.IndexByte(buf[c:end], '\r')
				}
				if nl < 0 {
					break
				}
				p = c + nl
				if p < end-1 && buf[p] == '\r' && buf[p+1] == '\n' {
					p++
				}
				if p < end-1 && buf[p] == '\n' {
					p++
				}
				last = p
				lines--
				p++
			}
			if lines > 0 {
				last = end
			}
			st.search.s = buf
			st.search.null = false
			st.search.sLen = uint64(last)
			st.search.offset = uint64(offset)
			st.search.rmLen = 0
			return
		case rules.BEString16, rules.LEString16:
			if offset >= nbytes {
				break
			}
			src := int(offset)
			if typ == rules.BEString16 {
				src++
			}
			esrc := int(nbytes)
			dst := 0
			const edst = len(st.val) - 1
			for ; src < esrc; src, dst = src+2, dst+1 {
				if dst >= edst {
					break
				}
				st.val[dst] = s[src]
				if st.val[dst] == 0 {
					if typ == rules.BEString16 {
						if s[src-1] != 0 {
							st.val[dst] = ' '
						}
					} else if src+1 < esrc && s[src+1] != 0 {
						st.val[dst] = ' '
					}
				}
			}
			st.val[edst] = 0
			st.val[dst] = 0
			return
		case rules.String, rules.PString:
			if r := m.StrRange(); r != 0 && r < size {
				size = r
			}
		}
	}
	if typ == rules.Offset {
		st.val.clear()
		st.val.setQ(uint64(offset))
		return
	}
	if offset >= nbytes {
		st.val.clear()
		return
	}
	n := min(nbytes-offset, size)
	copy(st.val[:n], s[offset:offset+n])
	// libmagic pads the rest of the value with zeros.
	clear(st.val[n:])
}

// doOps is do_ops: it applies the operator of an indirect offset. It
// returns false when the offset overflows.
func doOps(m *rules.Entry, lhs, off int64) (uint32, bool) {
	const uintMax, intMin = 1<<32 - 1, -1 << 31
	if lhs >= uintMax || lhs <= intMin || off >= uintMax || off <= intMin {
		return 0, false
	}
	offset := lhs
	if off != 0 {
		switch m.InOp & rules.OpMask {
		case rules.OpAnd:
			offset = lhs & off
		case rules.OpOr:
			offset = lhs | off
		case rules.OpXor:
			offset = lhs ^ off
		case rules.OpAdd:
			offset = lhs + off
		case rules.OpMinus:
			offset = lhs - off
		case rules.OpMultiply:
			offset = lhs * off
		case rules.OpDivide:
			offset = lhs / off
		case rules.OpModulo:
			offset = lhs % off
		}
	}
	if m.InOp&rules.OpInverse != 0 {
		offset = ^offset
	}
	if offset >= uintMax {
		return 0, false
	}
	return uint32(offset), true
}

// strtoull8 is strtoull with base 8, on the C string at the start of s.
func strtoull8(s []byte) uint64 {
	s = cstr(s)
	i := 0
	for i < len(s) && isSpace(s[i]) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	var n uint64
	over := false
	for ; i < len(s) && s[i] >= '0' && s[i] <= '7'; i++ {
		if n > math.MaxUint64>>3 {
			over = true
		}
		n = n<<3 | uint64(s[i]-'0')
	}
	if over {
		return math.MaxUint64
	}
	if neg {
		return -n
	}
	return n
}

// mget is mget: it reads the value of entry idx of t, after its indirect
// offset, and runs indirect and use. It returns -1 on an error, 0 when the
// value cannot be read, and 1 when it was read.
func (st *state) mget(t *table, idx int, b, bb *buffer, o uint64, contLevel int, mode rules.Flag, text, flip bool,
	ic, nc *uint16, mv *matchVars, returnval *int, found *bool) int {
	m := &t.entries[idx]
	offset := st.offset
	s := bb.data
	nbytes := len(s)

	if *ic >= indirMax {
		st.errorf("indirect count (%d) exceeded", *ic)
		return -1
	}
	if *nc >= nameMax {
		st.errorf("name use count (%d) exceeded", *nc)
		return -1
	}

	st.mcopy(m.Type, m.Flag&rules.FlagIndir != 0, s, uint32(uint64(offset)+o), uint32(nbytes), m)

	if m.Flag&rules.FlagIndir != 0 {
		off := int64(m.InOffset)
		sgn := m.InOp&rules.OpSigned != 0
		if m.InOp&rules.OpIndirect != 0 {
			at := int64(offset) + off
			if offsetOOB(nbytes, at, 0) {
				return 0
			}
			hb := s[at:]
			need := func(n int) bool { return !offsetOOB(nbytes, at, n) }
			switch op := cvtFlip(m.InType, flip); op {
			case rules.Byte:
				if !need(1) {
					return 0
				}
				off = sext(sgn, 8, uint64(hb[0]))
			case rules.Short, rules.LEShort:
				if !need(2) {
					return 0
				}
				off = sext(sgn, 16, uint64(le.Uint16(hb)))
			case rules.BEShort:
				if !need(2) {
					return 0
				}
				off = sext(sgn, 16, uint64(be16(hb)))
			case rules.Long, rules.LELong, rules.LEID3:
				if !need(4) {
					return 0
				}
				off = sext(sgn, 32, uint64(le.Uint32(hb)))
			case rules.BELong, rules.BEID3:
				if !need(4) {
					return 0
				}
				off = sext(sgn, 32, uint64(be32(hb)))
			case rules.MELong:
				if !need(4) {
					return 0
				}
				off = sext(sgn, 32, uint64(me32(hb)))
			case rules.BEQuad:
				if !need(8) {
					return 0
				}
				off = sext(sgn, 64, be64(hb))
			case rules.LEQuad:
				if !need(8) {
					return 0
				}
				off = sext(sgn, 64, le.Uint64(hb))
			case rules.Octal:
				if offsetOOB(nbytes, int64(offset), int(m.Vallen)) {
					return 0
				}
				off = sext(sgn, 64, strtoull8(st.val[:]))
			default:
				return 0
			}
		}
		var lhs int64
		need := func(n int) bool { return !offsetOOB(nbytes, int64(offset), n) }
		switch inType := cvtFlip(m.InType, flip); inType {
		case rules.Byte:
			if !need(1) {
				return 0
			}
			lhs = sext(sgn, 8, uint64(st.val.b()))
		case rules.BEShort:
			if !need(2) {
				return 0
			}
			lhs = sext(sgn, 16, uint64(be16(st.val[:])))
		case rules.LEShort, rules.Short:
			if !need(2) {
				return 0
			}
			lhs = sext(sgn, 16, uint64(st.val.h()))
		case rules.BELong, rules.BEID3:
			if !need(4) {
				return 0
			}
			v := be32(st.val[:])
			if inType == rules.BEID3 {
				v = cvtID3(v)
			}
			lhs = sext(sgn, 32, uint64(v))
		case rules.LELong, rules.LEID3:
			if !need(4) {
				return 0
			}
			v := st.val.l()
			if inType == rules.LEID3 {
				v = cvtID3(v)
			}
			lhs = sext(sgn, 32, uint64(v))
		case rules.MELong:
			if !need(4) {
				return 0
			}
			lhs = sext(sgn, 32, uint64(me32(st.val[:])))
		case rules.Long:
			if !need(4) {
				return 0
			}
			lhs = sext(sgn, 32, uint64(st.val.l()))
		case rules.LEQuad:
			if !need(8) {
				return 0
			}
			lhs = sext(sgn, 64, st.val.q())
		case rules.BEQuad:
			if !need(8) {
				return 0
			}
			lhs = sext(sgn, 64, be64(st.val[:]))
		case rules.Octal:
			if !need(int(m.Vallen)) {
				return 0
			}
			lhs = sext(sgn, 64, strtoull8(st.val[:]))
		default:
			return 0
		}
		var ok bool
		if offset, ok = doOps(m, lhs, off); !ok {
			return 0
		}

		if m.Flag&rules.FlagIndirOffAdd != 0 {
			if contLevel == 0 {
				return 0
			}
			offset += uint32(st.li[contLevel-1].off)
			if offset == 0 {
				return 0
			}
		}
		st.mcopy(m.Type, false, s, offset, uint32(nbytes), m)
		st.offset = offset
	}

	// Check that there is enough data for the type.
	need := func(n int) bool { return !offsetOOB(nbytes, int64(offset), n) }
	switch m.Type {
	case rules.Byte:
		if !need(1) {
			return 0
		}
	case rules.Short, rules.BEShort, rules.LEShort:
		if !need(2) {
			return 0
		}
	case rules.Long, rules.BELong, rules.LELong, rules.MELong, rules.Date, rules.BEDate, rules.LEDate,
		rules.MEDate, rules.LDate, rules.BELDate, rules.LELDate, rules.MELDate, rules.Float,
		rules.BEFloat, rules.LEFloat:
		if !need(4) {
			return 0
		}
	case rules.Double, rules.BEDouble, rules.LEDouble:
		if !need(8) {
			return 0
		}
	case rules.LEGUID, rules.BEGUID, rules.GUID:
		if !need(16) {
			return 0
		}
	case rules.String, rules.PString, rules.Search, rules.Octal:
		if !need(int(m.Vallen)) {
			return 0
		}
	case rules.Regex:
		if uint64(nbytes) < uint64(offset) {
			return 0
		}
	case rules.Indirect:
		if m.StrFlags()&rules.IndirectRelative != 0 {
			offset += uint32(o)
		}
		if offset == 0 || uint64(nbytes) < uint64(offset) {
			return 0
		}
		*ic++
		sbb := &buffer{data: s[offset:], file: b.file}
		mark := len(st.out)
		rv := -1
		set := st.e.sets[0]
		rv = st.match(set, 0, len(set.entries), sbb, 0, rules.FlagBinTest, text, false, ic, nc, mv, nil, nil)
		// The output of the indirect match goes after the description of
		// this entry.
		inner := string(st.out[mark:])
		st.out = st.out[:mark]
		if st.err != nil {
			return -1
		}
		if rv == 1 {
			if st.flags&NoDesc == 0 {
				st.printFormat(m, m.DescString(), uint64(offset))
			}
			st.printf("%s", inner)
		}
		return rv
	case rules.Use:
		if uint64(nbytes) < uint64(offset) {
			return 0
		}
		name := cstr(m.Value[:])
		if len(name) > 0 && name[0] == '^' {
			name = name[1:]
			flip = !flip
		}
		ref, ok := st.e.names[string(name)]
		if !ok {
			st.errorf("cannot find entry `%s'", name)
			return -1
		}
		saved := st.li
		st.li = append([]levelInfo(nil), saved...)

		oneedSep := mv.needSep
		if m.Flag&rules.FlagNoSpace != 0 {
			mv.needSep = false
		}
		nfound := false
		*nc++
		eoffset := st.eoffset
		rv := st.match(st.e.sets[1], ref.start, ref.n, b, uint64(offset)+o, mode, text, flip, ic, nc, mv, returnval, &nfound)
		if nfound {
			st.val.setQ(1)
		} else {
			st.val.setQ(0)
		}
		*nc--
		*found = *found || nfound
		st.li = saved
		if rv != 1 {
			mv.needSep = oneedSep
		}
		st.offset = offset
		st.eoffset = eoffset
		if rv != 0 {
			return rv
		}
		if *found {
			return 1
		}
		return 0
	case rules.Name:
		if st.flags&NoDesc != 0 {
			return 1
		}
		st.printf("%s", m.DescString())
		return 1
	}
	if !st.convert(m, flip) {
		return 0
	}
	return 1
}

// cvtNum is DO_CVT of softmagic.c: the mask operation of m on v, an integer
// of bits bits. It returns false on a division by zero or an overflow.
func cvtNum(m *rules.Entry, v uint64, bits int) (uint64, bool) {
	mask := m.Mask
	signed := m.Flag&rules.FlagUnsigned == 0
	trunc := func(x uint64) uint64 {
		if bits == 64 {
			return x
		}
		return x & (1<<bits - 1)
	}
	ext := func(x uint64) int64 { return sext(true, bits, x) }
	if mask != 0 {
		switch m.MaskOp & rules.OpMask {
		case rules.OpAnd:
			v &= mask
		case rules.OpOr:
			v |= mask
		case rules.OpXor:
			v ^= mask
		case rules.OpAdd:
			v += mask
		case rules.OpMinus:
			v -= mask
		case rules.OpMultiply:
			v *= mask
		case rules.OpDivide, rules.OpModulo:
			mt := trunc(mask)
			if mt == 0 {
				return 0, false
			}
			if signed {
				a, d := ext(trunc(v)), ext(mt)
				if bits >= 32 && d == -1 && a == sext(true, bits, 1<<(bits-1)) {
					return 0, false
				}
				if m.MaskOp&rules.OpMask == rules.OpDivide {
					v = uint64(a / d)
				} else {
					v = uint64(a % d)
				}
			} else if m.MaskOp&rules.OpMask == rules.OpDivide {
				v = trunc(v) / mt
			} else {
				v = trunc(v) % mt
			}
		}
	}
	if m.MaskOp&rules.OpInverse != 0 {
		v = ^v
	}
	return trunc(v), true
}

// cvtFloat is DO_CVT2: the mask operation of m on a float.
func cvtFloat(m *rules.Entry, v float64, single bool) (float64, bool) {
	mask := m.Mask
	if mask == 0 {
		return v, true
	}
	var x float64
	if single {
		x = float64(float32(int64(mask)))
	} else {
		x = float64(int64(mask))
	}
	switch m.MaskOp & rules.OpMask {
	case rules.OpAdd:
		v += x
	case rules.OpMinus:
		v -= x
	case rules.OpMultiply:
		v *= x
	case rules.OpDivide:
		if x == 0 {
			return 0, false
		}
		v /= x
	}
	if single {
		v = float64(float32(v))
	}
	return v, true
}

// convert is mconvert: it puts the value in the byte order of the machine,
// and applies the mask.
func (st *state) convert(m *rules.Entry, flip bool) bool {
	p := &st.val
	num := func(bits int, v uint64, set func(uint64)) bool {
		r, ok := cvtNum(m, v, bits)
		if !ok {
			st.magErr("zerodivide/overflow/underflow in mconvert()")
			return false
		}
		set(r)
		return true
	}
	set8 := func(x uint64) { p.setB(uint8(x)) }
	set16 := func(x uint64) { p.setH(uint16(x)) }
	set32 := func(x uint64) { p.setL(uint32(x)) }
	set64 := func(x uint64) { p.setQ(x) }
	flt := func(single bool) bool {
		var v float64
		if single {
			v = float64(p.f())
		} else {
			v = p.d()
		}
		r, ok := cvtFloat(m, v, single)
		if !ok {
			st.magErr("zerodivide/overflow/underflow in mconvert()")
			return false
		}
		if single {
			p.setF(float32(r))
		} else {
			p.setD(r)
		}
		return true
	}
	switch cvtFlip(m.Type, flip) {
	case rules.Byte:
		return num(8, uint64(p.b()), set8)
	case rules.Short, rules.MSDOSDate, rules.MSDOSTime:
		return num(16, uint64(p.h()), set16)
	case rules.Long, rules.Date, rules.LDate:
		return num(32, uint64(p.l()), set32)
	case rules.Quad, rules.QDate, rules.QLDate, rules.QWDate, rules.Offset:
		return num(64, p.q(), set64)
	case rules.String, rules.BEString16, rules.LEString16, rules.Octal:
		p[len(p)-1] = 0
		return true
	case rules.PString:
		sz := pstringLengthSize(m)
		if sz == 0 {
			return false
		}
		l, ok := pstringLength(m, p[:])
		if !ok {
			return false
		}
		max := uint64(len(p) - sz)
		if l >= max {
			l = max
		}
		copy(p[:l], p[sz:uint64(sz)+l])
		p[l] = 0
		return true
	case rules.BEShort, rules.BEMSDOSDate, rules.BEMSDOSTime:
		return num(16, uint64(be16(p[:])), set16)
	case rules.BELong, rules.BEDate, rules.BELDate:
		return num(32, uint64(be32(p[:])), set32)
	case rules.BEQuad, rules.BEQDate, rules.BEQLDate, rules.BEQWDate:
		return num(64, be64(p[:]), set64)
	case rules.LEShort, rules.LEMSDOSDate, rules.LEMSDOSTime:
		return num(16, uint64(p.h()), set16)
	case rules.LELong, rules.LEDate, rules.LELDate:
		return num(32, uint64(p.l()), set32)
	case rules.LEQuad, rules.LEQDate, rules.LEQLDate, rules.LEQWDate:
		return num(64, p.q(), set64)
	case rules.MELong, rules.MEDate, rules.MELDate:
		return num(32, uint64(me32(p[:])), set32)
	case rules.Float:
		return flt(true)
	case rules.BEFloat:
		p.setL(be32(p[:]))
		return flt(true)
	case rules.LEFloat:
		return flt(true)
	case rules.Double:
		return flt(false)
	case rules.BEDouble:
		p.setQ(be64(p[:]))
		return flt(false)
	case rules.LEDouble:
		return flt(false)
	case rules.Regex, rules.Search, rules.Default, rules.Clear, rules.Name, rules.Use, rules.DER,
		rules.GUID, rules.LEGUID, rules.BEGUID:
		return true
	}
	st.magErr("invalid type %d in mconvert()", m.Type)
	return false
}

// pstringLengthSize is file_pstring_length_size: the size of the length of
// a Pascal string, or 0 when the rule is bad.
func pstringLengthSize(m *rules.Entry) int {
	switch m.StrFlags() & (rules.PString1LE | rules.PString2LE | rules.PString2BE | rules.PString4LE | rules.PString4BE) {
	case rules.PString1LE:
		return 1
	case rules.PString2LE, rules.PString2BE:
		return 2
	case rules.PString4LE, rules.PString4BE:
		return 4
	}
	return 0
}

// pstringLength is file_pstring_get_length: the length of the Pascal
// string at the start of s.
func pstringLength(m *rules.Entry, s []byte) (uint64, bool) {
	var l uint64
	switch m.StrFlags() & (rules.PString1LE | rules.PString2LE | rules.PString2BE | rules.PString4LE | rules.PString4BE) {
	case rules.PString1LE:
		l = uint64(s[0])
	case rules.PString2LE:
		l = uint64(le.Uint16(s))
	case rules.PString2BE:
		l = uint64(be16(s))
	case rules.PString4LE:
		l = uint64(le.Uint32(s))
	case rules.PString4BE:
		l = uint64(be32(s))
	default:
		return 0, false
	}
	if m.StrFlags()&rules.PStringLengthIncludesItself != 0 {
		l -= uint64(pstringLengthSize(m))
	}
	return l, true
}

// strncmp is file_strncmp: it compares the len bytes of a, the value of a
// rule, with b, the data, under the string flags. It reads b as far as
// maxlen when the flags compact white space. A read past b gives 0.
func strncmp(a, b []byte, length, maxlen int, flags uint32) uint64 {
	ws := flags & (rules.StrCompactWhitespace | rules.StrCompactOptionalWhitespace)
	eb := length
	if ws != 0 {
		eb = maxlen
	}
	var v int
	ai, bi := 0, 0
	if flags == 0 {
		for ; length > 0; length-- {
			v = int(at(b, bi)) - int(at(a, ai))
			bi++
			ai++
			if v != 0 {
				break
			}
		}
		return uint64(int64(v))
	}
	for length > 0 {
		if bi >= eb {
			v = 1
			break
		}
		ca := at(a, ai)
		switch {
		case flags&rules.StrIgnoreLowercase != 0 && isLower(ca):
			v = int(toLower(at(b, bi))) - int(ca)
			bi++
			ai++
		case flags&rules.StrIgnoreUppercase != 0 && isUpper(ca):
			v = int(toUpper(at(b, bi))) - int(ca)
			bi++
			ai++
		case flags&rules.StrCompactWhitespace != 0 && isSpace(ca):
			ai++
			if isSpace(at(b, bi)) {
				bi++
				if !isSpace(at(a, ai)) {
					for bi < eb && isSpace(at(b, bi)) {
						bi++
					}
				}
			} else {
				v = 1
			}
		case flags&rules.StrCompactOptionalWhitespace != 0 && isSpace(ca):
			ai++
			for bi < eb && isSpace(at(b, bi)) {
				bi++
			}
		default:
			v = int(at(b, bi)) - int(ca)
			bi++
			ai++
		}
		if v != 0 {
			break
		}
		length--
	}
	if length == 0 && v == 0 && flags&rules.StrFullWord != 0 {
		if c := at(b, bi); c != 0 && !isSpace(c) {
			v = 1
		}
	}
	return uint64(int64(v))
}

// signExtend is file_signextend: v as the type of m reads it.
func signExtend(m *rules.Entry, v uint64) uint64 {
	if m.Flag&rules.FlagUnsigned != 0 {
		return v
	}
	switch m.Type {
	case rules.Byte:
		return uint64(int64(int8(v)))
	case rules.Short, rules.BEShort, rules.LEShort:
		return uint64(int64(int16(v)))
	case rules.Date, rules.BEDate, rules.LEDate, rules.MEDate, rules.LDate, rules.BELDate,
		rules.LELDate, rules.MELDate, rules.Long, rules.BELong, rules.LELong, rules.MELong,
		rules.Float, rules.BEFloat, rules.LEFloat, rules.MSDOSDate, rules.BEMSDOSDate,
		rules.LEMSDOSDate, rules.MSDOSTime, rules.BEMSDOSTime, rules.LEMSDOSTime:
		return uint64(int64(int32(v)))
	case rules.String, rules.PString, rules.BEString16, rules.LEString16, rules.Regex,
		rules.Search, rules.Default, rules.Indirect, rules.Name, rules.Use, rules.Clear,
		rules.DER, rules.GUID, rules.LEGUID, rules.BEGUID, rules.Octal:
		return v
	case rules.Quad, rules.BEQuad, rules.LEQuad, rules.QDate, rules.QLDate, rules.QWDate,
		rules.BEQDate, rules.BEQLDate, rules.BEQWDate, rules.LEQDate, rules.LEQLDate,
		rules.LEQWDate, rules.Double, rules.BEDouble, rules.LEDouble, rules.Offset,
		rules.BEVarint, rules.LEVarint:
		return v
	}
	return math.MaxUint64
}

// check is magiccheck: whether the value that mget read matches entry idx
// of t. It returns -1 on an error.
func (st *state) check(t *table, idx int) int {
	m := &t.entries[idx]
	l := entryQ(&m.Value)
	var v uint64
	p := &st.val

	switch m.Type {
	case rules.Byte:
		v = uint64(p.b())
	case rules.Short, rules.BEShort, rules.LEShort, rules.MSDOSDate, rules.LEMSDOSDate,
		rules.BEMSDOSDate, rules.MSDOSTime, rules.LEMSDOSTime, rules.BEMSDOSTime:
		v = uint64(p.h())
	case rules.Long, rules.BELong, rules.LELong, rules.MELong, rules.Date, rules.BEDate,
		rules.LEDate, rules.MEDate, rules.LDate, rules.BELDate, rules.LELDate, rules.MELDate:
		v = uint64(p.l())
	case rules.Quad, rules.LEQuad, rules.BEQuad, rules.QDate, rules.BEQDate, rules.LEQDate,
		rules.QLDate, rules.BEQLDate, rules.LEQLDate, rules.QWDate, rules.BEQWDate,
		rules.LEQWDate, rules.Offset:
		v = p.q()
	case rules.Float, rules.BEFloat, rules.LEFloat:
		fl := float64(math.Float32frombits(le.Uint32(m.Value[:])))
		return floatRel(st, m, float64(p.f()), fl)
	case rules.Double, rules.BEDouble, rules.LEDouble:
		dl := math.Float64frombits(le.Uint64(m.Value[:]))
		return floatRel(st, m, p.d(), dl)
	case rules.Default, rules.Clear:
		l, v = 0, 0
	case rules.String, rules.PString, rules.Octal:
		l = 0
		v = strncmp(m.Value[:], p[:], int(m.Vallen), len(p), m.StrFlags())
	case rules.BEString16, rules.LEString16:
		l = 0
		// file_strncmp16 ignores the flags.
		v = strncmp(m.Value[:], p[:], int(m.Vallen), len(p), 0)
	case rules.Search:
		if st.search.null {
			return 0
		}
		slen := min(int(m.Vallen), len(m.Value))
		l, v = 0, 0
		s := st.search.s
		sLen := st.search.sLen
		if slen > 0 && m.StrFlags() == 0 {
			idx := uint64(m.StrRange()) + uint64(slen)
			if m.StrRange() == 0 || sLen < idx {
				idx = sLen
			}
			found := bytes.Index(s[:idx], m.Value[:slen])
			if found < 0 {
				v = 1
				break
			}
			st.search.offset += uint64(found)
			st.search.rmLen = sLen - uint64(found)
			break
		}
		for idx := uint64(0); m.StrRange() == 0 || idx < uint64(m.StrRange()); idx++ {
			if uint64(slen)+idx > sLen {
				v = 1
				break
			}
			v = strncmp(m.Value[:], s[idx:], slen, int(sLen-idx), m.StrFlags())
			if v == 0 {
				st.search.offset += idx
				st.search.rmLen = sLen - idx
				break
			}
		}
	case rules.Regex:
		if st.search.null {
			return 0
		}
		rx, err := t.regex(idx)
		if err != nil {
			st.err = err
			return -1
		}
		l = 0
		var text []byte
		if n := st.search.sLen; n != 0 {
			// libmagic copies the region and puts a NUL on its last byte.
			text = cstr(st.search.s[:n-1])
		}
		so, eo, ok := rx.find(text)
		if !ok {
			v = 1
			break
		}
		st.search.s = st.search.s[so:]
		st.search.offset += uint64(so)
		st.search.rmLen = uint64(eo - so)
		v = 0
	case rules.Use:
		if p.q() != 0 {
			return 1
		}
		return 0
	case rules.Name, rules.Indirect:
		return 1
	case rules.DER:
		r := st.derCmp(m)
		if r == -1 {
			return 0
		}
		return r
	case rules.BEGUID, rules.LEGUID, rules.GUID:
		l = 0
		v = uint64(int64(bytes.Compare(m.Value[:16], p[:16])))
	default:
		st.magErr("invalid type %d in magiccheck()", m.Type)
		return -1
	}

	v = signExtend(m, v)
	matched := false
	switch m.Reln {
	case 'x':
		matched = true
	case '!':
		matched = v != l
	case '=':
		matched = v == l
	case '>':
		if m.Flag&rules.FlagUnsigned != 0 {
			matched = v > l
		} else {
			matched = int64(v) > int64(l)
		}
	case '<':
		if m.Flag&rules.FlagUnsigned != 0 {
			matched = v < l
		} else {
			matched = int64(v) < int64(l)
		}
	case '&':
		matched = v&l == l
	case '^':
		matched = v&l != l
	default:
		st.magErr("cannot happen: invalid relation `%c'", m.Reln)
		return -1
	}
	if matched {
		return 1
	}
	return 0
}

// floatRel is the relation of a float or a double, with the rules of IEEE
// for a NaN.
func floatRel(st *state, m *rules.Entry, v, l float64) int {
	unordered := math.IsNaN(v) || math.IsNaN(l)
	var matched bool
	switch m.Reln {
	case 'x':
		matched = true
	case '!':
		matched = unordered || v != l
	case '=':
		matched = !unordered && v == l
	case '>':
		matched = !unordered && v > l
	case '<':
		matched = !unordered && v < l
	default:
		st.magErr("cannot happen with float: invalid relation `%c'", m.Reln)
		return -1
	}
	if matched {
		return 1
	}
	return 0
}

// moffset is moffset: the offset after the value of m, which the next level
// of continuation reads relative to. It returns -1 or 0 when the offset is
// out of the nbytes of the buffer, and 1 when it set *op.
func (st *state) moffset(m *rules.Entry, nbytes int, offset uint64, op *int32) int {
	var o int32
	p := &st.val
	switch m.Type {
	case rules.Byte:
		o = int32(st.offset + 1)
	case rules.Short, rules.BEShort, rules.LEShort, rules.MSDOSDate, rules.LEMSDOSDate,
		rules.BEMSDOSDate, rules.MSDOSTime, rules.LEMSDOSTime, rules.BEMSDOSTime:
		o = int32(st.offset + 2)
	case rules.Long, rules.BELong, rules.LELong, rules.MELong, rules.Date, rules.BEDate,
		rules.LEDate, rules.MEDate, rules.LDate, rules.BELDate, rules.LELDate, rules.MELDate,
		rules.Float, rules.BEFloat, rules.LEFloat:
		o = int32(st.offset + 4)
	case rules.Quad, rules.BEQuad, rules.LEQuad, rules.QDate, rules.BEQDate, rules.LEQDate,
		rules.QLDate, rules.BEQLDate, rules.LEQLDate, rules.Double, rules.BEDouble, rules.LEDouble:
		o = int32(st.offset + 8)
	case rules.String, rules.PString, rules.BEString16, rules.LEString16, rules.Octal:
		if m.Reln == '=' || m.Reln == '!' {
			o = int32(st.offset + uint32(m.Vallen))
		} else {
			if m.Value[0] == 0 {
				s := p.str()
				if i := bytes.IndexAny(s, "\r\n"); i >= 0 {
					p[i] = 0
				}
			}
			o = int32(st.offset + uint32(p.strlen()))
			if m.Type == rules.PString {
				l := pstringLengthSize(m)
				if l == 0 {
					return -1
				}
				o += int32(l)
			}
		}
	case rules.Regex:
		vlen := st.search.rmLen
		if m.StrFlags()&rules.RegexOffsetStart != 0 {
			vlen = 0
		}
		o = int32(st.search.offset + vlen - offset)
	case rules.Search:
		vlen := uint64(m.Vallen)
		if m.StrFlags()&rules.RegexOffsetStart != 0 {
			vlen = 0
		}
		o = int32(st.search.offset + vlen - offset)
	case rules.Clear, rules.Default, rules.Indirect, rules.Offset, rules.Use:
		o = int32(st.offset)
	case rules.DER:
		o = st.derOffs(m, nbytes)
		if o == -1 || uint64(uint32(o)) > uint64(nbytes) {
			*op = 0
			return 0
		}
	case rules.BEGUID, rules.LEGUID, rules.GUID:
		o = int32(st.offset + 16)
	default:
		o = 0
	}
	if uint64(int64(o)) > uint64(nbytes) {
		return -1
	}
	*op = o
	return 1
}

// annotate is handle_annotation: it prints the Apple type, the extension or
// the MIME type of m when the flags ask for one. It returns 1 when it
// printed, and 0 when there was nothing to print.
func (st *state) annotate(m *rules.Entry, firstline bool) int {
	if st.flags&Apple != 0 && m.Apple[0] != 0 {
		if !firstline && st.flags&Continue == 0 {
			return 1
		}
		st.printSep(firstline)
		st.printf("%s", cstr(m.Apple[:8]))
		return 1
	}
	if st.flags&Extension != 0 && m.Ext[0] != 0 {
		if !firstline && st.flags&Continue == 0 {
			return 1
		}
		st.printSep(firstline)
		st.printf("%s", cstr(m.Ext[:]))
		return 1
	}
	if st.flags&MimeType != 0 && m.MIME[0] != 0 {
		if !firstline && st.flags&Continue == 0 {
			return 1
		}
		st.printSep(firstline)
		mime := cstr(m.MIME[:])
		if x, ok := st.varexpand(mime, 1024); ok {
			mime = x
		}
		st.printf("%s", mime)
		return 1
	}
	return 0
}

// printSep is print_sep: a separator before every result but the first.
func (st *state) printSep(firstline bool) {
	if !firstline {
		st.printf(separator)
	}
}

// varexpand is varexpand of softmagic.c: it replaces each ${x?a:b} in str
// with a when the file is executable, and b when it is not. It fails when
// the result does not fit size bytes.
func (st *state) varexpand(str []byte, size int) ([]byte, bool) {
	var out []byte
	for {
		i := bytes.Index(str, []byte("${"))
		if i < 0 {
			break
		}
		out = append(out, str[:i]...)
		p := str[i+2:]
		if len(p) < 2 || p[1] != '?' {
			return nil, false
		}
		t := p[2:]
		et := bytes.IndexByte(t, ':')
		if et < 0 {
			return nil, false
		}
		e := t[et+1:]
		ee := bytes.IndexByte(e, '}')
		if ee < 0 {
			return nil, false
		}
		if p[0] != 'x' {
			return nil, false
		}
		if st.mode&0o111 != 0 {
			out = append(out, t[:et]...)
		} else {
			out = append(out, e[:ee]...)
		}
		if len(out) >= size {
			return nil, false
		}
		str = e[ee+1:]
	}
	out = append(out, str...)
	if len(out) >= size {
		return nil, false
	}
	return out, true
}

// printFormat prints the description of m with a number, as the
// description of an indirect entry does.
func (st *state) printFormat(m *rules.Entry, desc string, v uint64) {
	// A description with no format prints as it is. Phase 3 brings the
	// formats of C (docs/PLAN.md).
	st.printf("%s", fmtC(desc, strconv.FormatUint(v, 10)))
}
