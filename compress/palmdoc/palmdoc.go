// Package palmdoc decodes the compression of PalmDOC, which the text records
// of PalmDOC and MOBI books use. The header of such a book gives it as
// compression type 2.
//
// It is not a format of libmagic, so it does not register with
// github.com/xo/magic/compress (D61). The code follows the description of the
// format on the PalmDOC page of the MobileRead wiki.
package palmdoc

import (
	"errors"
	"fmt"
)

// The errors of Decode. Decode wraps them with the position of the fault.
var (
	// ErrCorrupt reports data that PalmDOC does not allow, such as a copy from
	// before the start of the record.
	ErrCorrupt = errors.New("palmdoc: corrupt data")
	// ErrLimit reports a record that decodes to more bytes than the limit.
	ErrLimit = errors.New("palmdoc: data over the limit")
)

// Decode appends the decoded bytes of the record src to dst, and returns the
// extended slice. It appends at most limit bytes. When the record is longer,
// it appends the first limit bytes and returns an error that wraps ErrLimit.
// When the record is damaged, it returns the bytes that it decoded before the
// fault and an error that wraps ErrCorrupt.
//
// Each byte of src is one of four codes:
//
//   - 0x00 and 0x09 to 0x7F: the byte itself.
//   - 0x01 to 0x08: that number of bytes, which follow the code.
//   - 0x80 to 0xBF: with the next byte, a copy of 3 to 10 bytes from 1 to
//     2047 bytes back in the decoded record.
//   - 0xC0 to 0xFF: a space, then the byte XOR 0x80.
//
// A record decodes by itself. A copy can reach back to the start of the
// record, never into the bytes of dst before it. A MOBI text record can end
// with trailing entries, which the extra data flags of the MOBI header name.
// The caller removes them before it calls Decode.
func Decode(dst, src []byte, limit int) ([]byte, error) {
	start := len(dst)
	limit = max(limit, 0)
	for i := 0; i < len(src); {
		c := src[i]
		at := i
		i++
		switch {
		case c >= 0x01 && c <= 0x08:
			n := int(c)
			if n > len(src)-i {
				return dst, fmt.Errorf("%w: %d bytes at byte %d, past the end of the record", ErrCorrupt, n, at)
			}
			room := limit - (len(dst) - start)
			if n > room {
				return append(dst, src[i:i+room]...), fmt.Errorf("%w: %d bytes", ErrLimit, limit)
			}
			dst = append(dst, src[i:i+n]...)
			i += n
		case c < 0x80:
			if len(dst)-start >= limit {
				return dst, fmt.Errorf("%w: %d bytes", ErrLimit, limit)
			}
			dst = append(dst, c)
		case c >= 0xC0:
			room := limit - (len(dst) - start)
			if room < 2 {
				if room == 1 {
					dst = append(dst, ' ')
				}
				return dst, fmt.Errorf("%w: %d bytes", ErrLimit, limit)
			}
			dst = append(dst, ' ', c^0x80)
		default:
			if i >= len(src) {
				return dst, fmt.Errorf("%w: copy at byte %d, past the end of the record", ErrCorrupt, at)
			}
			v := int(c)<<8 | int(src[i])
			i++
			dist, n := (v&0x3FFF)>>3, (v&7)+3
			if dist == 0 || dist > len(dst)-start {
				return dst, fmt.Errorf("%w: copy at byte %d from %d bytes back, and the record has %d", ErrCorrupt, at, dist, len(dst)-start)
			}
			room := limit - (len(dst) - start)
			over := n > room
			n = min(n, room)
			// The copy can overlap the bytes that it writes, so it goes one
			// byte at a time.
			for range n {
				dst = append(dst, dst[len(dst)-dist])
			}
			if over {
				return dst, fmt.Errorf("%w: %d bytes", ErrLimit, limit)
			}
		}
	}
	return dst, nil
}
