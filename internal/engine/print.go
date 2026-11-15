// Copyright (c) Ian F. Darwin 1986-1995.
// Software written by Ian F. Darwin and others;
// maintained 1995-present by Christos Zoulas and others.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice immediately at the beginning of the file, without modification,
//    this list of conditions, and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED. IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE FOR
// ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
// Copyright (c) Christos Zoulas 2003.
// All Rights Reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice immediately at the beginning of the file, without modification,
//    this list of conditions, and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED. IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE FOR
// ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
// Copyright (c) 2008 Christos Zoulas
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE NETBSD FOUNDATION, INC. AND CONTRIBUTORS
// ``AS IS'' AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED
// TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR
// PURPOSE ARE DISCLAIMED.  IN NO EVENT SHALL THE FOUNDATION OR CONTRIBUTORS
// BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
// CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
// SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
// INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
// CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
// ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
// POSSIBILITY OF SUCH DAMAGE.
//
// This file translates src/softmagic.c, src/print.c, src/funcs.c, src/cdf_time.c of file 5.48 to Go (D29).

package engine

import (
	"bytes"
	"fmt"
	"time"

	"github.com/xo/magic/internal/rules"
)

// This file is mprint of softmagic.c and the helpers of print.c and
// funcs.c: it prints the description of a rule with the value it read.

// format is F of softmagic.c: the description as a format, when its
// conversions take the same argument as def. Otherwise libmagic records an
// error and prints with def.
func (st *state) format(desc []byte, def string, line int) []byte {
	if bytes.IndexByte(desc, '%') < 0 {
		return desc
	}
	f, ok := fmtcheck(string(desc), def)
	if !ok {
		st.magErr("%s, %d: format `%s' does not match with `%s'", "softmagic.c", line, desc, def)
	}
	return []byte(f)
}

// printable is file_printable: the C string at the start of s, at most
// slen bytes, with each byte that is not printable as an octal escape, in
// a buffer of bufsiz bytes.
func (st *state) printableC(s []byte, slen, bufsiz int) []byte {
	if slen < len(s) {
		s = s[:slen]
	}
	out := make([]byte, 0, len(s))
	eptr := bufsiz - 1
	for _, c := range s {
		if len(out) >= eptr || c == 0 {
			break
		}
		if st.flags&Raw != 0 || isPrint(c) {
			out = append(out, c)
			continue
		}
		if len(out) >= eptr-3 {
			break
		}
		out = append(out, '\\', '0'+(c>>6)&7, '0'+(c>>3)&7, '0'+c&7)
	}
	return out
}

// strtrim is file_strtrim: s without the white space at both ends.
func strtrim(s []byte) []byte {
	s = cstr(s)
	return bytes.TrimFunc(s, func(r rune) bool { return r < 0x80 && isSpace(byte(r)) })
}

// mprint is mprint of softmagic.c.
func (st *state) mprint(m *rules.Entry) int {
	desc := cstr(m.Desc[:])
	if x, ok := st.varexpand(desc, 512); ok {
		desc = x
	}
	p := &st.val
	unsigned := m.Flag&rules.FlagUnsigned != 0

	// printer is the PRINTER macro. line is the line of softmagic.c that
	// calls it, which an error names.
	printer := func(v uint64, quad bool, line int) int {
		v = signExtend(m, v)
		if bytes.IndexByte(desc, '%') >= 0 && checkFmtString(desc) {
			s := fmt.Sprint(int64(v))
			if unsigned {
				s = fmt.Sprint(v)
			}
			return st.cprintf(st.format(desc, "%s", line), cstringS(s))
		}
		def := "%d"
		if unsigned {
			def = "%u"
		}
		if quad {
			def = "%ll" + def[1:]
		}
		return st.cprintf(st.format(desc, def, line), cint(v))
	}

	switch m.Type {
	case rules.Byte:
		v := uint64(p.b())
		if unsigned {
			return printer(v, false, 649)
		}
		return printer(uint64(int64(int8(v))), false, 649)
	case rules.Short, rules.BEShort, rules.LEShort:
		v := uint64(p.h())
		if unsigned {
			return printer(v, false, 654)
		}
		return printer(uint64(int64(int16(v))), false, 654)
	case rules.Long, rules.BELong, rules.LELong, rules.MELong:
		v := uint64(p.l())
		if unsigned {
			return printer(v, false, 660)
		}
		return printer(uint64(int64(int32(v))), false, 660)
	case rules.Quad, rules.BEQuad, rules.LEQuad, rules.Offset:
		return printer(p.q(), true, 666)
	case rules.String, rules.PString, rules.BEString16, rules.LEString16:
		if m.Reln == '=' || m.Reln == '!' {
			return st.cprintf(st.format(desc, "%s", 673), cstring(st.printableC(m.Value[:], len(m.Value), 512)))
		}
		if m.Value[0] == 0 {
			if i := bytes.IndexAny(p.str(), "\r\n"); i >= 0 {
				p[i] = 0
			}
		}
		str := p[:]
		if m.StrFlags()&rules.StrTrim != 0 {
			str = strtrim(str)
		}
		if st.cprintf(st.format(desc, "%s", 689), cstring(st.printableC(str, len(str), 512))) == -1 {
			return -1
		}
		if m.Type == rules.PString && pstringLengthSize(m) == 0 {
			return -1
		}
	case rules.Date, rules.BEDate, rules.LEDate, rules.MEDate:
		return st.cprintf(st.format(desc, "%s", 706), cstringS(fmtDateTime(uint64(p.l()), 0)))
	case rules.LDate, rules.BELDate, rules.LELDate, rules.MELDate:
		return st.cprintf(st.format(desc, "%s", 715), cstringS(fmtDateTime(uint64(p.l()), timeLocal)))
	case rules.QDate, rules.BEQDate, rules.LEQDate:
		return st.cprintf(st.format(desc, "%s", 724), cstringS(fmtDateTime(p.q(), 0)))
	case rules.QLDate, rules.BEQLDate, rules.LEQLDate:
		return st.cprintf(st.format(desc, "%s", 732), cstringS(fmtDateTime(p.q(), timeLocal)))
	case rules.QWDate, rules.BEQWDate, rules.LEQWDate:
		return st.cprintf(st.format(desc, "%s", 740), cstringS(fmtDateTime(p.q(), timeWindows)))
	case rules.Float, rules.BEFloat, rules.LEFloat:
		v := float64(p.f())
		if bytes.IndexByte(desc, '%') >= 0 && checkFmtString(desc) {
			return st.cprintf(st.format(desc, "%s", 755), cstring(cprintf([]byte("%g"), cfloat(v))))
		}
		return st.cprintf(st.format(desc, "%g", 759), cfloat(v))
	case rules.Double, rules.BEDouble, rules.LEDouble:
		v := p.d()
		if bytes.IndexByte(desc, '%') >= 0 && checkFmtString(desc) {
			return st.cprintf(st.format(desc, "%s", 774), cstring(cprintf([]byte("%g"), cfloat(v))))
		}
		return st.cprintf(st.format(desc, "%g", 778), cfloat(v))
	case rules.Search, rules.Regex:
		if st.search.null {
			break
		}
		n := min(st.search.rmLen, uint64(len(st.search.s)))
		cp := cstr(st.search.s[:n])
		if m.StrFlags()&rules.StrTrim != 0 {
			cp = strtrim(cp)
		}
		return st.cprintf(st.format(desc, "%s", 800), cstring(st.printableC(cp, int(st.search.rmLen), 512)))
	case rules.Default, rules.Clear:
		return st.printf("%s", cstr(m.Desc[:]))
	case rules.Indirect, rules.Use, rules.Name:
	case rules.DER:
		return st.cprintf(st.format(desc, "%s", 820), cstring(st.printableC(p[:], len(p), 512)))
	case rules.GUID, rules.LEGUID:
		return st.cprintf(st.format(desc, "%s", 828), cstringS(guidString(p[:16], false)))
	case rules.BEGUID:
		return st.cprintf(st.format(desc, "%s", 832), cstringS(guidString(p[:16], true)))
	case rules.MSDOSDate, rules.BEMSDOSDate, rules.LEMSDOSDate:
		return st.cprintf(st.format(desc, "%s", 839), cstringS(fmtDOSDate(p.h())))
	case rules.MSDOSTime, rules.BEMSDOSTime, rules.LEMSDOSTime:
		return st.cprintf(st.format(desc, "%s", 846), cstringS(fmtDOSTime(p.h())))
	case rules.Octal:
		return st.cprintf(st.format(desc, "%s", 852), cstringS(fmtNum(cstr(m.Value[:]), 8)))
	default:
		st.magErr("invalid m->type (%d) in mprint()", m.Type)
		return -1
	}
	return 0
}

// The flags of file_fmtdatetime.
const (
	timeLocal = 1 << iota
	timeWindows
)

// maxCtime is MAX_CTIME: the last second of the year 9999 that asctime
// prints.
const maxCtime = 0x3afff487cf

// asctime is asctime_r of glibc, without its newline.
func asctime(t time.Time) string {
	return fmt.Sprintf("%.3s %.3s%3d %.2d:%.2d:%.2d %d",
		t.Weekday().String(), t.Month().String(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Year())
}

// fmtDateTime is file_fmtdatetime: a time in seconds since the epoch, or in
// 100 ns since 1601 for Windows.
func fmtDateTime(v uint64, flags int) string {
	const invalid = "*Invalid datetime*"
	var t int64
	if flags&timeWindows != 0 {
		var ok bool
		if t, ok = cdfTimestamp(int64(v)); !ok {
			// cdf_timestamp_to_timespec leaves the seconds as they were;
			// libmagic reads them anyway. They are 0 here.
			t = 0
		}
	} else {
		t = int64(v)
	}
	if t > maxCtime {
		return invalid
	}
	tm := time.Unix(t, 0).UTC()
	if flags&timeLocal != 0 {
		tm = time.Unix(t, 0).Local()
	}
	if y := int64(tm.Year()) - 1900; y < -1<<31 || y > 1<<31-1-1900 {
		return invalid
	}
	return asctime(tm)
}

// cdfTimestamp is cdf_timestamp_to_timespec: the seconds since the epoch of
// a time in 100 ns since 1601. It builds the date with the approximations
// of libmagic, then converts it as mktime does, in local time.
func cdfTimestamp(t int64) (int64, bool) {
	t /= 10000000
	sec := int(t % 60)
	t /= 60
	mn := int(t % 60)
	t /= 60
	hour := int(t % 24)
	t /= 24
	year := int(1601 + t/365)
	if year > 9999 {
		return 0, false
	}
	t -= int64(cdfGetDays(year)) - 1
	mday := cdfGetDay(year, int(t))
	mon := cdfGetMonth(year, int(t))
	return time.Date(year, time.Month(mon+1), mday, hour, mn, sec, 0, time.Local).Unix(), true
}

var cdfMdays = [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

func isLeap(y int) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

func cdfGetDays(year int) int {
	days := 0
	for y := 1601; y < year; y++ {
		days += 365
		if isLeap(y) {
			days++
		}
	}
	return days
}

func cdfGetDay(year, days int) int {
	for m, d := range cdfMdays {
		sub := d
		if m == 1 && isLeap(year) {
			sub++
		}
		if days < sub {
			return days
		}
		days -= sub
	}
	return days
}

func cdfGetMonth(year, days int) int {
	for m, d := range cdfMdays {
		days -= d
		if m == 1 && isLeap(year) {
			days--
		}
		if days <= 0 {
			return m
		}
	}
	return len(cdfMdays)
}

var monthAbbr = [...]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// fmtDOSDate is file_fmtdate: an MS-DOS date as strftime "%b %d %Y".
func fmtDOSDate(v uint16) string {
	mday := int(v & 0x1f)
	mon := int(v>>5&0xf) - 1
	if mon < 0 || mon > 11 {
		mon = 0
	}
	return fmt.Sprintf("%s %02d %d", monthAbbr[mon], mday, int(v>>9)+1980)
}

// fmtDOSTime is file_fmttime: an MS-DOS time as strftime "%T".
func fmtDOSTime(v uint16) string {
	return fmt.Sprintf("%02d:%02d:%02d", v>>11, v>>5&0x3f, (v&0x1f)*2)
}

// fmtNum is file_fmtnum: the number in the C string us, in base, as an
// unsigned decimal.
func fmtNum(us []byte, base int) string {
	const invalid = "*Invalid number*"
	i := 0
	for i < len(us) && isSpace(us[i]) {
		i++
	}
	neg := false
	if i < len(us) && (us[i] == '+' || us[i] == '-') {
		neg = us[i] == '-'
		i++
	}
	var v uint64
	digits := 0
	for ; i < len(us); i++ {
		d := int(us[i] - '0')
		if us[i] < '0' || d >= base {
			break
		}
		if v > (1<<64-1)/uint64(base) {
			return invalid
		}
		v = v*uint64(base) + uint64(d)
		digits++
	}
	if i < len(us) {
		return invalid
	}
	if digits == 0 {
		// strtoull reads no number, and endptr points at the start.
		if len(us) != 0 {
			return invalid
		}
	}
	if neg {
		v = -v
	}
	return fmt.Sprint(v)
}

// guidString is file_print_guid: a GUID as text. big says that its first
// three fields are big-endian.
func guidString(b []byte, big bool) string {
	d1, d2, d3 := le.Uint32(b), le.Uint16(b[4:]), le.Uint16(b[6:])
	if big {
		d1, d2, d3 = be32(b), be16(b[4:]), be16(b[6:])
	}
	return fmt.Sprintf("%08X-%04X-%04X-%02X%02X-%02X%02X%02X%02X%02X%02X",
		d1, d2, d3, b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15])
}
