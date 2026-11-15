//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package filesystem

import "errors"

var errLZO = errors.New("lzo: the data is not valid")

// lzo1x decodes a block of LZO1X, as lzo1x_decompress_safe of the LZO
// library, which squashfs uses, and writes at most limit bytes. It follows
// the states of the decoder of the library: a run of literals, a match of
// one of four kinds by the size of its distance, and the literals that the
// two low bits of a match add.
func lzo1x(in []byte, limit int) ([]byte, error) {
	var out []byte
	ip := 0
	byteAt := func() (int, bool) {
		if ip >= len(in) {
			return 0, false
		}
		ip++
		return int(in[ip-1]), true
	}
	literals := func(n int) bool {
		if n < 0 || ip+n > len(in) || len(out)+n > limit {
			return false
		}
		out = append(out, in[ip:ip+n]...)
		ip += n
		return true
	}
	copyMatch := func(dist, n int) bool {
		start := len(out) - dist
		if dist <= 0 || start < 0 || len(out)+n > limit {
			return false
		}
		for k := range n {
			out = append(out, out[start+k])
		}
		return true
	}
	// long reads the length of a long run: zero bytes count 255 each, and
	// the byte that ends them adds base and its value.
	long := func(base int) (int, bool) {
		n := 0
		for {
			b, ok := byteAt()
			if !ok {
				return 0, false
			}
			if b != 0 {
				return n + base + b, true
			}
			n += 255
			if n > limit {
				return 0, false
			}
		}
	}

	var t int
	var ok bool
	state := "loop"
	if len(in) > 0 && in[0] > 17 {
		ip = 1
		t = int(in[0]) - 17
		if t < 4 {
			state = "next"
		} else {
			if !literals(t) {
				return nil, errLZO
			}
			state = "first"
		}
	}
	for {
		switch state {
		case "loop":
			if t, ok = byteAt(); !ok {
				return nil, errLZO
			}
			if t >= 16 {
				state = "match"
				continue
			}
			if t == 0 {
				if t, ok = long(15); !ok {
					return nil, errLZO
				}
			}
			if !literals(t + 3) {
				return nil, errLZO
			}
			state = "first"
		case "first":
			if t, ok = byteAt(); !ok {
				return nil, errLZO
			}
			if t >= 16 {
				state = "match"
				continue
			}
			b, ok := byteAt()
			if !ok || !copyMatch(1+0x0800+(t>>2)+(b<<2), 3) {
				return nil, errLZO
			}
			state = "done"
		case "match":
			var dist, n int
			switch {
			case t >= 64:
				b, ok := byteAt()
				if !ok {
					return nil, errLZO
				}
				dist, n = 1+((t>>2)&7)+(b<<3), (t>>5)+1
			case t >= 32:
				if t &= 31; t == 0 {
					if t, ok = long(31); !ok {
						return nil, errLZO
					}
				}
				if ip+2 > len(in) {
					return nil, errLZO
				}
				dist, n = 1+(int(in[ip])>>2)+(int(in[ip+1])<<6), t+2
				ip += 2
			case t >= 16:
				dist = (t & 8) << 11
				if t &= 7; t == 0 {
					if t, ok = long(7); !ok {
						return nil, errLZO
					}
				}
				if ip+2 > len(in) {
					return nil, errLZO
				}
				dist += (int(in[ip]) >> 2) + (int(in[ip+1]) << 6)
				ip += 2
				if dist == 0 {
					// The end of the stream.
					if ip != len(in) {
						return nil, errLZO
					}
					return out, nil
				}
				dist, n = dist+0x4000, t+2
			default:
				b, ok := byteAt()
				if !ok || !copyMatch(1+(t>>2)+(b<<2), 2) {
					return nil, errLZO
				}
				state = "done"
				continue
			}
			if !copyMatch(dist, n) {
				return nil, errLZO
			}
			state = "done"
		case "done":
			// The two low bits of the byte before the last that the match
			// read give the literals that follow it.
			if ip < 2 {
				return nil, errLZO
			}
			if t = int(in[ip-2]) & 3; t == 0 {
				state = "loop"
				continue
			}
			state = "next"
		case "next":
			if !literals(t) {
				return nil, errLZO
			}
			if t, ok = byteAt(); !ok {
				return nil, errLZO
			}
			state = "match"
		}
	}
}
