package engine

import (
	"fmt"
	"strings"

	"github.com/xo/magic/internal/rules"
)

// This file is der.c: the der type, which reads the tags of ASN.1 DER.

const derBad = ^uint32(0)

// derTags are the names of the universal tags, as der__tag of der.c.
var derTags = [...]string{
	"eoc", "bool", "int", "bit_str", "octet_str",
	"null", "obj_id", "obj_desc", "ext", "real",
	"enum", "embed", "utf8_str", "rel_oid", "time",
	"res2", "seq", "set", "num_str", "prt_str",
	"t61_str", "vid_str", "ia5_str", "utc_time", "gen_time",
	"gr_str", "vis_str", "gen_str", "univ_str", "char_str",
	"bmp_str", "date", "tod", "datetime", "duration",
	"oid-iri", "rel-oid-iri",
}

// derGetTag is gettag.
func derGetTag(c []byte, null bool, p *uint64, l uint64) uint32 {
	if null || *p >= l {
		return derBad
	}
	tag := uint32(c[*p] & 0x1f)
	*p++
	if tag != 0x1f {
		return tag
	}
	if *p >= l {
		return derBad
	}
	for c[*p] >= 0x80 {
		tag = tag*128 + uint32(c[*p]) - 0x80
		*p++
		if *p >= l {
			return derBad
		}
	}
	return tag
}

// derGetLength is getlength.
func derGetLength(c []byte, p *uint64, l uint64) uint32 {
	if *p >= l {
		return derBad
	}
	oneByte := c[*p]&0x80 == 0
	digits := uint64(c[*p] & 0x7f)
	*p++
	if *p+digits >= l {
		return derBad
	}
	if oneByte {
		return uint32(digits)
	}
	var n uint64
	for range digits {
		n = n<<8 | uint64(c[*p])
		*p++
	}
	if n > uint64(^uint32(0))-*p || *p+n > l {
		return derBad
	}
	return uint32(n)
}

// derOffs is der_offs: the offset after the DER value at the search.
func (st *state) derOffs(m *rules.Entry, nbytes int) int32 {
	b := st.search.s
	l := st.search.sLen
	if l == 0 {
		l = uint64(nbytes)
	}
	// libmagic can read past the search here. The port stops at the end
	// of the buffer.
	l = min(l, uint64(len(b)))
	var offs uint64
	if derGetTag(b, st.search.null, &offs, l) == derBad {
		return -1
	}
	tlen := derGetLength(b, &offs, l)
	if tlen == derBad {
		return -1
	}
	offs += uint64(st.offset + uint32(m.Offset))
	if m.ContLevel != 0 {
		if offs+uint64(tlen) > uint64(nbytes) {
			return -1
		}
		st.li[m.ContLevel-1].off = int32(offs + uint64(tlen))
	}
	return int32(offs)
}

// derData is der_data: the value of a DER item as text, in a buffer of 128
// bytes. buf holds what was there before, which a value of length 0 that
// is not a string leaves.
func derData(buf string, tag uint32, d []byte) string {
	const blen = 128
	switch tag {
	case 0x13, 0x0c, 0x16: // prt_str, utf8_str, ia5_str
		s := string(cstr(d))
		if len(s) > blen-1 {
			s = s[:blen-1]
		}
		return s
	case 0x17: // utc_time
		if len(d) >= 12 {
			return fmt.Sprintf("20%c%c-%c%c-%c%c %c%c:%c%c:%c%c GMT",
				d[0], d[1], d[2], d[3], d[4], d[5], d[6], d[7], d[8], d[9], d[10], d[11])
		}
	}
	if len(d) == 0 {
		return buf
	}
	var sb strings.Builder
	for i, c := range d {
		if i<<1 < blen-2 {
			fmt.Fprintf(&sb, "%.2x", c)
		}
	}
	return sb.String()
}

// derCmp is der_cmp: whether the DER item at the search has the tag, the
// length and the value that m names. It returns -1 at the end of the data.
func (st *state) derCmp(m *rules.Entry) int {
	b := st.search.s
	l := st.search.sLen
	var offs uint64
	tag := derGetTag(b, st.search.null, &offs, l)
	if tag == derBad {
		return -1
	}
	tlen := derGetLength(b, &offs, l)
	if tlen == derBad {
		return -1
	}
	var name string
	if tag < uint32(len(derTags)) {
		name = derTags[tag]
	} else {
		name = fmt.Sprintf("%#x", tag)
	}
	s := string(cstr(m.Value[:]))
	if !strings.HasPrefix(s, name) {
		return 0
	}
	s = s[len(name):]
	for {
		switch {
		case s == "":
			return 1
		case s[0] == '=':
			s = s[1:]
			end := min(offs+uint64(tlen), uint64(len(b)))
			data := derData(name, tag, b[offs:end])
			if data != s && s != "x" {
				return 0
			}
			copy(st.val[:], data)
			st.val[min(len(data), len(st.val)-1)] = 0
			return 1
		case !isDigit(s[0]):
			return 0
		}
		n := 0
		for len(s) > 0 && isDigit(s[0]) {
			n = n*10 + int(s[0]-'0')
			s = s[1:]
		}
		if uint64(tlen) != uint64(n) {
			return 0
		}
	}
}
