// Package adc decodes ADC, the Apple Data Compression of older DMG files. A
// DMG file names a chunk of ADC with the type 0x80000004 in its table of
// blocks.
//
// ADC is an LZ77 encoding of bytes with three codes:
//
//   - 1LLLLLLL: a literal of 1 to 128 bytes, which follow the code.
//   - 00MMMMDD DDDDDDDD: a match of 3 to 18 bytes, from 1 to 1024 bytes back.
//   - 01MMMMMM DDDDDDDD DDDDDDDD: a match of 4 to 67 bytes, from 1 to 65536
//     bytes back.
//
// D holds the distance minus 1. A chunk has no header and no code of the
// end. It ends with its bytes, and the table of blocks gives its length and
// the length of its output.
//
// It is not a format of libmagic, so the package does not register with
// github.com/xo/magic/compress (D63). The code is written from knowledge of
// the format. 7z and dmg2img extract the same disk as the decoder from the
// DMG file of its tests.
package adc

import (
	"errors"
	"fmt"
)

// The errors of Decode. Decode wraps them with the place of the fault.
var (
	// ErrCorrupt reports data that ADC does not allow, such as a match from
	// before the start of the chunk, or a code that the chunk cuts.
	ErrCorrupt = errors.New("adc: corrupt data")
	// ErrLimit reports a chunk that decodes to more bytes than the limit.
	ErrLimit = errors.New("adc: data over the limit")
)

// Decode appends the decoded bytes of the chunk src to dst, and returns the
// extended slice. It appends at most limit bytes. When the chunk is longer,
// it appends the first limit bytes and returns an error that wraps ErrLimit.
// When the chunk is damaged, it returns the bytes that it decoded before the
// fault and an error that wraps ErrCorrupt.
//
// A chunk decodes by itself. A match can reach back to the start of the
// chunk, never into the bytes of dst before it.
func Decode(dst, src []byte, limit int) ([]byte, error) {
	start := len(dst)
	limit = max(limit, 0)
	for i := 0; i < len(src); {
		c := src[i]
		at := i
		room := limit - (len(dst) - start)
		if c&0x80 != 0 {
			n := int(c&0x7F) + 1
			i++
			if n > len(src)-i {
				return dst, fmt.Errorf("%w: a literal of %d bytes at byte %d, past the end of the chunk", ErrCorrupt, n, at)
			}
			if n > room {
				return append(dst, src[i:i+room]...), fmt.Errorf("%w: %d bytes", ErrLimit, limit)
			}
			dst = append(dst, src[i:i+n]...)
			i += n
			continue
		}
		var n, dist int
		if c&0x40 != 0 {
			if len(src)-i < 3 {
				return dst, fmt.Errorf("%w: a match at byte %d, past the end of the chunk", ErrCorrupt, at)
			}
			n = int(c&0x3F) + 4
			dist = (int(src[i+1])<<8 | int(src[i+2])) + 1
			i += 3
		} else {
			if len(src)-i < 2 {
				return dst, fmt.Errorf("%w: a match at byte %d, past the end of the chunk", ErrCorrupt, at)
			}
			n = int(c>>2) + 3
			dist = (int(c&3)<<8 | int(src[i+1])) + 1
			i += 2
		}
		if dist > len(dst)-start {
			return dst, fmt.Errorf("%w: a match at byte %d from %d bytes back, and the chunk has %d", ErrCorrupt, at, dist, len(dst)-start)
		}
		over := n > room
		n = min(n, room)
		// The copy can overlap the bytes that it appends, so it goes by at
		// most dist bytes at a time.
		for n > 0 {
			k := min(n, dist)
			from := len(dst) - dist
			dst = append(dst, dst[from:from+k]...)
			n -= k
		}
		if over {
			return dst, fmt.Errorf("%w: %d bytes", ErrLimit, limit)
		}
	}
	return dst, nil
}
