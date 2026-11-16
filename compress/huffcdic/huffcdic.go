// Package huffcdic decodes the Huffman compression of Mobipocket, which MOBI
// books give as compression type 17480 in their header. The book holds the
// code in one HUFF record and the phrases in one or more CDIC records. The
// MOBI header names the first of these records and their number. Each code
// of a text record stands for a phrase, and a phrase can hold codes of other
// phrases.
//
// It is not a format of libmagic, so it does not register with
// github.com/xo/magic/compress (D61).
package huffcdic

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

// The errors of New and Decode. They wrap ErrCorrupt with the place of the
// fault.
var (
	// ErrCorrupt reports records that the format does not allow, such as a
	// code that names no phrase, or a phrase that holds itself.
	ErrCorrupt = errors.New("huffcdic: corrupt data")
	// ErrLimit reports a record that decodes to more bytes than the limit.
	ErrLimit = errors.New("huffcdic: data over the limit")
)

// maxDepth is the most phrases that can hold one another. A book from
// Mobipocket nests a few. The limit keeps the work of a hostile book in
// proportion to the limit of Decode.
const maxDepth = 64

// code is an entry of the cache table of the HUFF record, for one value of
// the first 8 bits of a code.
type code struct {
	// bits is the length of the code, or the shortest length when term is
	// false.
	bits int
	// term is true when bits is the length of each code that starts with
	// these 8 bits.
	term bool
	// max is the highest code of that length, aligned to the left of 32 bits,
	// plus the index of its first phrase. It holds when term is true.
	max uint64
}

// phrase is an entry of the CDIC records.
type phrase struct {
	// data is the phrase, decoded when done is true, and compressed when it
	// is false.
	data []byte
	done bool
	// busy is true while Decode decodes the phrase, so that a phrase that
	// holds itself is an error and not an endless loop.
	busy bool
}

// Decoder decodes the text records of one book. It is safe for concurrent
// use. It decodes each phrase the first time that a record uses it and keeps
// the result, so the calls of Decode run one at a time.
type Decoder struct {
	cache [256]code
	// min and max are the lowest and highest codes of each length, from 1 to
	// 32, aligned to the left of 32 bits. max includes the index of the first
	// phrase of the length, as code.max does.
	min, max [33]uint64

	mu      sync.Mutex
	phrases []phrase
}

// New reads the HUFF record huff and the CDIC records cdics, in the order of
// the book. It copies the records, so the caller can reuse them.
//
// The HUFF record starts with "HUFF", the length of its header, the offset
// of the cache table and the offset of the base table, each in 4 bytes, big
// endian. The cache table has 256 entries of 4 bytes, one for each value of
// the first 8 bits of a code: the length of the code in bits 0 to 4, a flag
// in bit 7 that is set when the length is final, and the highest code in
// bits 8 to 31. The base table has the lowest and the highest code of each
// length from 1 to 32, in 4 bytes each.
//
// A CDIC record starts with "CDIC", the length of its header, the number of
// phrases of the book and the number of bits of an index, each in 4 bytes,
// big endian. It holds 1<<bits phrases, or the phrases that are left when
// fewer are left. Their offsets follow the header, in 2 bytes each, from
// the end of the header. Each phrase starts with its length in bits 0 to 14
// of 2 bytes, and bit 15 is set when the phrase is not compressed.
func New(huff []byte, cdics [][]byte) (*Decoder, error) {
	if len(huff) < 16 || string(huff[:4]) != "HUFF" {
		return nil, fmt.Errorf("%w: the HUFF record has no header", ErrCorrupt)
	}
	d := new(Decoder)
	cache, base := int64(be32(huff[8:])), int64(be32(huff[12:]))
	if cache+256*4 > int64(len(huff)) || base+64*4 > int64(len(huff)) {
		return nil, fmt.Errorf("%w: a table of the HUFF record is past its end", ErrCorrupt)
	}
	for i := range d.cache {
		v := be32(huff[cache+int64(i)*4:])
		c := code{bits: int(v & 0x1F), term: v&0x80 != 0}
		if c.bits == 0 || !c.term && c.bits <= 8 {
			return nil, fmt.Errorf("%w: entry %d of the cache table has %d bits", ErrCorrupt, i, c.bits)
		}
		c.max = (uint64(v>>8)+1)<<(32-c.bits) - 1
		d.cache[i] = c
	}
	for n := 1; n <= 32; n++ {
		p := base + int64(n-1)*8
		d.min[n] = uint64(be32(huff[p:])) << (32 - n)
		d.max[n] = (uint64(be32(huff[p+4:]))+1)<<(32-n) - 1
	}
	total := int64(-1)
	for i, rec := range cdics {
		if len(rec) < 16 || string(rec[:4]) != "CDIC" {
			return nil, fmt.Errorf("%w: CDIC record %d has no header", ErrCorrupt, i)
		}
		if total < 0 {
			total = int64(be32(rec[8:]))
		}
		bits := be32(rec[12:])
		if bits > 31 {
			return nil, fmt.Errorf("%w: CDIC record %d has %d bits", ErrCorrupt, i, bits)
		}
		n := min(int64(1)<<bits, total-int64(len(d.phrases)))
		if 16+2*n > int64(len(rec)) {
			return nil, fmt.Errorf("%w: the offsets of CDIC record %d are past its end", ErrCorrupt, i)
		}
		rec = bytes.Clone(rec)
		for j := range int(n) {
			p := 16 + int(be16(rec[16+2*j:]))
			if p+2 > len(rec) {
				return nil, fmt.Errorf("%w: phrase %d of CDIC record %d is past its end", ErrCorrupt, j, i)
			}
			v := be16(rec[p:])
			end := p + 2 + int(v&0x7FFF)
			if end > len(rec) {
				return nil, fmt.Errorf("%w: phrase %d of CDIC record %d is past its end", ErrCorrupt, j, i)
			}
			d.phrases = append(d.phrases, phrase{data: rec[p+2 : end : end], done: v&0x8000 != 0})
		}
	}
	return d, nil
}

// Decode appends the decoded bytes of the text record src to dst, and returns
// the extended slice. It appends at most limit bytes. When the record is
// longer, it appends the first limit bytes and returns an error that wraps
// ErrLimit. When the record is damaged, it returns the bytes that it decoded
// before the fault and an error that wraps ErrCorrupt.
//
// A MOBI text record can end with trailing entries, which the extra data
// flags of the MOBI header name. The caller removes them before it calls
// Decode.
func (d *Decoder) Decode(dst, src []byte, limit int) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.decode(dst, src, len(dst)+max(limit, 0), 0)
}

// decode appends the decoded bytes of src to dst, up to the length end of
// dst. depth is the number of phrases that hold src.
func (d *Decoder) decode(dst, src []byte, end, depth int) ([]byte, error) {
	left := int64(len(src)) * 8
	for pos := int64(0); ; {
		c := peek32(src, pos)
		e := d.cache[c>>24]
		n, top := e.bits, e.max
		if !e.term {
			for c < d.min[n] {
				n++
				switch {
				case int64(n) > left:
					// The bits that pad the record hold no code.
					return dst, nil
				case n > 32:
					return dst, fmt.Errorf("%w: no code at bit %d", ErrCorrupt, pos)
				}
			}
			top = d.max[n]
		}
		// The last code ends with the data. The bits after it pad the
		// record to a byte.
		if left -= int64(n); left < 0 {
			return dst, nil
		}
		if top < c {
			return dst, fmt.Errorf("%w: code at bit %d is over the highest code", ErrCorrupt, pos)
		}
		r := (top - c) >> (32 - n)
		if r >= uint64(len(d.phrases)) {
			return dst, fmt.Errorf("%w: code at bit %d names phrase %d, and the book has %d", ErrCorrupt, pos, r, len(d.phrases))
		}
		pos += int64(n)
		p := &d.phrases[r]
		if !p.done {
			if p.busy || depth >= maxDepth {
				return dst, fmt.Errorf("%w: phrase %d holds itself, or %d phrases nest", ErrCorrupt, r, maxDepth)
			}
			p.busy = true
			data, err := d.decode(nil, p.data, end-len(dst), depth+1)
			p.busy = false
			if err != nil {
				// Over the limit, data holds the bytes up to it.
				return append(dst, data...), err
			}
			p.data, p.done = data, true
		}
		if len(p.data) > end-len(dst) {
			return append(dst, p.data[:end-len(dst)]...), ErrLimit
		}
		dst = append(dst, p.data...)
	}
}

// peek32 returns the 32 bits of b from bit pos, the first bit of b being the
// highest. The bits after the end of b are 0.
func peek32(b []byte, pos int64) uint64 {
	var v uint64
	i := pos / 8
	for k := range int64(5) {
		v <<= 8
		if i+k < int64(len(b)) {
			v |= uint64(b[i+k])
		}
	}
	return v >> (8 - pos%8) & 0xFFFFFFFF
}

func be32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }

func be16(b []byte) uint16 { return binary.BigEndian.Uint16(b) }
