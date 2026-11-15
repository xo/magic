//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Package lz4 decodes blocks of LZ4.
package lz4

import "errors"

// errLZ4 is the error of data that is not valid LZ4.
var errLZ4 = errors.New("lz4: the data is not valid")

// Block decodes one block of LZ4 with no frame, of at most limit bytes,
// as squashfs and the LZ4 frame hold it.
func Block(src []byte, limit int) ([]byte, error) {
	var out []byte
	for i := 0; i < len(src); {
		tok := src[i]
		i++
		n := int(tok >> 4)
		if n == 15 {
			for {
				if i >= len(src) {
					return nil, errLZ4
				}
				b := src[i]
				i++
				n += int(b)
				if b != 255 {
					break
				}
			}
		}
		if i+n > len(src) || len(out)+n > limit {
			return nil, errLZ4
		}
		out = append(out, src[i:i+n]...)
		i += n
		if i == len(src) {
			return out, nil // the last sequence has no match
		}
		if i+2 > len(src) {
			return nil, errLZ4
		}
		dist := int(src[i]) | int(src[i+1])<<8
		i += 2
		ml := int(tok & 15)
		if ml == 15 {
			for {
				if i >= len(src) {
					return nil, errLZ4
				}
				b := src[i]
				i++
				ml += int(b)
				if b != 255 {
					break
				}
			}
		}
		ml += 4
		if dist == 0 || dist > len(out) || len(out)+ml > limit {
			return nil, errLZ4
		}
		// The match can overlap the bytes that it writes.
		base := len(out) - dist
		for k := range ml {
			out = append(out, out[base+k])
		}
	}
	return out, nil
}
