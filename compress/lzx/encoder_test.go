package lzx

import (
	"encoding/binary"
	"slices"
)

// encoder is a small LZX compressor for the tests. It writes verbatim,
// aligned offset and uncompressed blocks, matches with the three recent
// offsets, runs of the pretree, E8 translation, and resets.
type encoder struct {
	windowSize int
	// interval is the number of frames from one reset to the next, or 0 for
	// no reset.
	interval int
	// e8size is the translation size of E8, or 0 for no translation.
	e8size int32
	// blockSize is the most bytes of output in a block. kinds are the types
	// of the blocks, in turn.
	blockSize int
	kinds     []int

	slots       int
	main, lens  []uint8
	r           [3]uint32
	w           bitWriter
	frames      [][]byte
	out, length int
	cut         int
	kind        int
}

// token is a literal, when n is 0, or a match of n bytes from off back.
type token struct {
	lit byte
	n   int
	off uint32
}

// encode compresses data, and returns the compressed bytes of each frame.
func (e *encoder) encode(data []byte) [][]byte {
	e.slots = map[int]int{1 << 15: 30, 1 << 16: 32, 1 << 17: 34, 1 << 18: 36, 1 << 19: 38, 1 << 20: 42, 1 << 21: 50}[e.windowSize]
	e.frames = nil
	group := len(data)
	if e.interval > 0 {
		group = e.interval * FrameSize
	}
	for start := 0; start < len(data) || start == 0; start += group {
		e.group(data[start:min(start+group, len(data))])
		if len(data) == 0 {
			break
		}
	}
	return e.frames
}

// group compresses the frames from one reset to the next. The position of
// E8 translation starts at 0 with the group.
func (e *encoder) group(g []byte) {
	e.main = make([]uint8, 256+8*e.slots)
	e.lens = make([]uint8, 249)
	e.r = [3]uint32{1, 1, 1}
	e.w = bitWriter{}
	e.out, e.length, e.cut = 0, len(g), 0
	g = slices.Clone(g)
	if e.e8size != 0 {
		e.w.write(1, 1)
		e.w.write(uint32(e.e8size), 32)
		for f := 0; f < len(g); f += FrameSize {
			frame := g[f:min(f+FrameSize, len(g))]
			if len(frame) > 10 {
				translate(frame, int32(f), e.e8size)
			}
		}
	} else {
		e.w.write(0, 1)
	}
	tokens := e.tokens(g)
	for i, n := 0, 0; i < len(tokens); n++ {
		j, size := i, 0
		for j < len(tokens) && size < e.blockSize {
			size += max(tokens[j].n, 1)
			j++
		}
		e.kind = e.kinds[n%len(e.kinds)]
		if e.kind == uncompressed {
			e.raw(size, g)
		} else {
			e.block(tokens[i:j], size)
		}
		i = j
	}
	if len(g) == 0 {
		e.w.flush()
		e.frames = append(e.frames, e.w.out)
	}
}

// translate does E8 translation for the encoder: it changes the relative
// offset after each byte 0xE8 to an absolute one, as Decoder.translate
// expects.
func translate(f []byte, pos, size int32) {
	for i := 0; i < len(f)-10; {
		if f[i] != 0xE8 {
			i++
			continue
		}
		cur := pos + int32(i)
		rel := int32(binary.LittleEndian.Uint32(f[i+1:]))
		if rel >= -cur && rel < size {
			abs := rel + cur
			if rel >= size-cur {
				abs = rel - size
			}
			binary.LittleEndian.PutUint32(f[i+1:], uint32(abs))
		}
		i += 5
	}
}

// tokens finds matches with a hash of 3 bytes. A match does not pass the
// end of a frame, and reaches no further back than the window allows.
func (e *encoder) tokens(g []byte) []token {
	var out []token
	head := map[uint32][]int{}
	r := [3]uint32{1, 1, 1}
	for i := 0; i < len(g); {
		end := min((i/FrameSize+1)*FrameSize, len(g))
		limit := min(257, end-i)
		var best token
		try := func(off uint32, minLen int) {
			if off == 0 || int(off) > i || int(off) > e.windowSize-3 {
				return
			}
			n := 0
			for n < limit && g[i+n] == g[i+n-int(off)] {
				n++
			}
			if n >= minLen && n > best.n {
				best = token{n: n, off: off}
			}
		}
		for _, off := range r {
			try(off, 2)
		}
		var h uint32
		if i+3 <= len(g) {
			h = uint32(g[i])<<16 | uint32(g[i+1])<<8 | uint32(g[i+2])
			c := head[h]
			for k := len(c) - 1; k >= max(0, len(c)-32); k-- {
				try(uint32(i-c[k]), 3)
			}
		}
		if best.n == 0 {
			best = token{lit: g[i]}
		} else {
			switch best.off {
			case r[0]:
			case r[1]:
				r[0], r[1] = r[1], r[0]
			case r[2]:
				r[0], r[2] = r[2], r[0]
			default:
				r = [3]uint32{best.off, r[0], r[1]}
			}
		}
		for k := range max(best.n, 1) {
			if i+k+3 <= len(g) {
				hk := uint32(g[i+k])<<16 | uint32(g[i+k+1])<<8 | uint32(g[i+k+2])
				head[hk] = append(head[hk], i+k)
			}
		}
		out = append(out, best)
		i += max(best.n, 1)
	}
	return out
}

// done counts n bytes of output, and ends the frame at its end.
func (e *encoder) done(n int) {
	e.out += n
	if e.out%FrameSize == 0 || e.out == e.length {
		e.w.flush()
		if len(e.w.out)%2 == 1 {
			e.w.out = append(e.w.out, 0)
		}
		e.frames = append(e.frames, e.w.out[e.cut:])
		e.cut = len(e.w.out)
	}
}

// raw writes an uncompressed block of the next size bytes of g.
func (e *encoder) raw(size int, g []byte) {
	e.w.write(uncompressed, 3)
	e.w.write(uint32(size), 24)
	e.w.write(0, 16-e.w.n)
	for _, r := range e.r {
		e.w.out = binary.LittleEndian.AppendUint32(e.w.out, r)
	}
	for i, k := e.out, 0; k < size; k++ {
		e.w.out = append(e.w.out, g[i+k])
		e.done(1)
	}
	if size%2 == 1 && e.out%FrameSize != 0 && e.out != e.length {
		e.w.out = append(e.w.out, 0)
	}
}

// sym is a token as the symbols of a block.
type sym struct {
	main, length int
	// footer holds bits bits of the offset. In an aligned offset block, the
	// low 3 bits of the offset go with the aligned code when aligned is
	// true.
	footer  uint32
	bits    uint
	aligned bool
	n       int
}

// block writes a verbatim or aligned offset block.
func (e *encoder) block(tokens []token, size int) {
	syms := make([]sym, 0, len(tokens))
	mainFreq := make([]int, len(e.main))
	lenFreq := make([]int, len(e.lens))
	alnFreq := make([]int, 8)
	for _, t := range tokens {
		if t.n == 0 {
			syms = append(syms, sym{main: int(t.lit), length: -1, n: 1})
			mainFreq[t.lit]++
			continue
		}
		s := sym{length: -1, n: t.n}
		var slot int
		switch t.off {
		case e.r[0]:
		case e.r[1]:
			slot = 1
			e.r[0], e.r[1] = e.r[1], e.r[0]
		case e.r[2]:
			slot = 2
			e.r[0], e.r[2] = e.r[2], e.r[0]
		default:
			v := t.off + 2
			slot = 3
			for slot+1 < e.slots && base[slot+1] <= v {
				slot++
			}
			s.footer, s.bits = v-base[slot], uint(footer[slot])
			if e.kind == alignedBlock && s.bits >= 3 {
				s.aligned = true
				alnFreq[s.footer&7]++
			}
			e.r = [3]uint32{t.off, e.r[0], e.r[1]}
		}
		head := min(t.n-2, 7)
		s.main = 256 + slot*8 + head
		mainFreq[s.main]++
		if head == 7 {
			s.length = t.n - 9
			lenFreq[s.length]++
		}
		syms = append(syms, s)
	}
	mainLens := huffman(mainFreq, 16)
	lenLens := huffman(lenFreq, 16)
	e.w.write(uint32(e.kind), 3)
	e.w.write(uint32(size), 24)
	var alnLens []uint8
	if e.kind == alignedBlock {
		alnLens = huffman(alnFreq, 7)
		if slices.Max(alnLens) == 0 {
			alnLens = []uint8{3, 3, 3, 3, 3, 3, 3, 3}
		}
		for _, l := range alnLens {
			e.w.write(uint32(l), 3)
		}
	}
	e.lengths(e.main, mainLens, 0, 256)
	e.lengths(e.main, mainLens, 256, len(e.main))
	e.lengths(e.lens, lenLens, 0, len(e.lens))
	mainCodes, lenCodes, alnCodes := canonical(mainLens), canonical(lenLens), canonical(alnLens)
	for _, s := range syms {
		e.w.write(mainCodes[s.main], uint(mainLens[s.main]))
		if s.length >= 0 {
			e.w.write(lenCodes[s.length], uint(lenLens[s.length]))
		}
		if s.aligned {
			e.w.write(s.footer>>3, s.bits-3)
			e.w.write(alnCodes[s.footer&7], uint(alnLens[s.footer&7]))
		} else {
			e.w.write(s.footer, s.bits)
		}
		e.done(s.n)
	}
}

// lengths writes the lengths next[from:end] with a pretree, as deltas from
// old, and keeps them in old.
func (e *encoder) lengths(old, next []uint8, from, end int) {
	type item struct {
		code  int
		extra uint32
		bits  uint
		then  int
	}
	var items []item
	freq := make([]int, 20)
	delta := func(i int) int { return (int(old[i]) - int(next[i]) + 17) % 17 }
	for i := from; i < end; {
		run := 1
		for i+run < end && next[i+run] == next[i] {
			run++
		}
		switch {
		case next[i] == 0 && run >= 20:
			run = min(run, 51)
			items = append(items, item{code: 18, extra: uint32(run - 20), bits: 5, then: -1})
		case next[i] == 0 && run >= 4:
			run = min(run, 19)
			items = append(items, item{code: 17, extra: uint32(run - 4), bits: 4, then: -1})
		case run >= 4:
			run = min(run, 5)
			items = append(items, item{code: 19, extra: uint32(run - 4), bits: 1, then: delta(i)})
			freq[delta(i)]++
		default:
			run = 1
			items = append(items, item{code: delta(i), then: -1})
		}
		freq[items[len(items)-1].code]++
		i += run
	}
	pre := huffman(freq, 15)
	codes := canonical(pre)
	for _, l := range pre {
		e.w.write(uint32(l), 4)
	}
	for _, it := range items {
		e.w.write(codes[it.code], uint(pre[it.code]))
		e.w.write(it.extra, it.bits)
		if it.then >= 0 {
			e.w.write(codes[it.then], uint(pre[it.then]))
		}
	}
	copy(old[from:end], next[from:end])
}

// huffman returns the lengths of a complete Huffman code for freq, of at most
// limit bits. A code with one symbol gets a second one, and a code with none
// has all lengths 0.
func huffman(freq []int, limit int) []uint8 {
	freq = slices.Clone(freq)
	var used []int
	for i, f := range freq {
		if f > 0 {
			used = append(used, i)
		}
	}
	lens := make([]uint8, len(freq))
	switch len(used) {
	case 0:
		return lens
	case 1:
		other := 0
		if used[0] == 0 {
			other = 1
		}
		freq[other] = 1
		used = append(used, other)
		slices.Sort(used)
	}
	for {
		type node struct{ w, parent int }
		nodes := make([]node, 0, 2*len(used))
		for _, s := range used {
			nodes = append(nodes, node{w: freq[s], parent: -1})
		}
		live := make([]int, len(nodes))
		for i := range live {
			live[i] = i
		}
		for len(live) > 1 {
			slices.SortStableFunc(live, func(a, b int) int { return nodes[a].w - nodes[b].w })
			a, b := live[0], live[1]
			nodes = append(nodes, node{w: nodes[a].w + nodes[b].w, parent: -1})
			nodes[a].parent, nodes[b].parent = len(nodes)-1, len(nodes)-1
			live = append(live[2:], len(nodes)-1)
		}
		longest := 0
		for i, s := range used {
			d := 0
			for p := nodes[i].parent; p >= 0; p = nodes[p].parent {
				d++
			}
			lens[s] = uint8(d)
			longest = max(longest, d)
		}
		if longest <= limit {
			return lens
		}
		for _, s := range used {
			freq[s] = freq[s]/2 + 1
		}
	}
}

// canonical returns the code of each symbol, in the order that tree.build
// gives them.
func canonical(lens []uint8) []uint32 {
	var count [17]uint32
	for _, l := range lens {
		if l > 0 {
			count[l]++
		}
	}
	var next [17]uint32
	var code uint32
	for l := 1; l <= 16; l++ {
		code = (code + count[l-1]) << 1
		next[l] = code
	}
	codes := make([]uint32, len(lens))
	for s, l := range lens {
		if l > 0 {
			codes[s] = next[l]
			next[l]++
		}
	}
	return codes
}

// bitWriter writes the bits of LZX: 16-bit words, little endian, from the
// highest bit of each word.
type bitWriter struct {
	out []byte
	acc uint32
	n   uint
}

// write writes the low k bits of v, the highest first.
func (w *bitWriter) write(v uint32, k uint) {
	for i := int(k) - 1; i >= 0; i-- {
		w.acc = w.acc<<1 | v>>uint(i)&1
		if w.n++; w.n == 16 {
			w.out = append(w.out, byte(w.acc), byte(w.acc>>8))
			w.acc, w.n = 0, 0
		}
	}
}

// flush pads the last word with zeros.
func (w *bitWriter) flush() {
	if w.n > 0 {
		w.write(0, 16-w.n)
	}
}
