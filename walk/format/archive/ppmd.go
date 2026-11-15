//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// This file follows Ppmd.h, Ppmd7.h, Ppmd7.c and Ppmd7Dec.c of 7-Zip, by
// Igor Pavlov, which are in the public domain. They come from PPMd var.H
// (2001) of Dmitry Shkarin. The code copies the behavior of 7-Zip, not its
// code.

package archive

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// szIDPPMd is the identifier of the method PPMd of 7-Zip.
const szIDPPMd = "\x03\x04\x01"

// The constants of Ppmd.h, Ppmd7.h and Ppmd7.c.
const (
	ppmdMinOrder   = 2
	ppmdMaxOrder   = 64
	ppmdMinMem     = 1 << 11
	ppmd7zMaxMem   = 0xFFFFFFFF - 12*3
	ppmdUnitSize   = 12
	ppmdMaxFreq    = 124
	ppmdNumIndexes = 38
	ppmdPeriodBits = 7
	ppmdBinScale   = 1 << 14
	ppmdTopValue   = 1 << 24
	ppmdStateSize  = 6
)

// ppmdMaxMem is the most memory that the walker gives to the model of
// PPMd. 7-Zip accepts up to ppmd7zMaxMem, near 4 GiB. The walker fails on
// a folder that needs more than 256 MiB, so that a damaged header cannot
// make it take gigabytes.
const ppmdMaxMem = 256 << 20

var (
	ppmdExpEscape  = [16]byte{25, 14, 9, 7, 5, 5, 4, 4, 4, 3, 3, 3, 2, 2, 2, 2}
	ppmdInitBinEsc = [8]uint16{0x3CDD, 0x1F3F, 0x59BF, 0x48F3, 0x64A1, 0x5ABC, 0x6632, 0x6051}
)

var errPPMd = errors.New("archive: PPMd data is not valid")

// szDecodePPMd is the method PPMd, variant H with the range coder of 7z,
// as the decoder of PpmdDecoder.cpp gives it. The properties are the order
// of the model and the size of its memory.
func szDecodePPMd(c *szCoder, in [][]byte, size int, w io.Writer) error {
	if len(in) != 1 || len(c.props) != 5 {
		return errSevenZip
	}
	order := uint32(c.props[0])
	mem := binary.LittleEndian.Uint32(c.props[1:])
	if order < ppmdMinOrder || order > ppmdMaxOrder || mem < ppmdMinMem || mem > ppmd7zMaxMem {
		return errSevenZip
	}
	if mem > ppmdMaxMem {
		return fmt.Errorf("archive: PPMd model of %d bytes is over the limit of %d bytes: %w", mem, ppmdMaxMem, errPPMd)
	}
	return ppmdDecode(order, mem, in[0], size, w)
}

// ppmdDecode decodes size bytes of the data src to w, with a model of the
// order and the memory size mem. It fails as 7-Zip does: when the data
// ends before size bytes, when the data has an end mark or an error, and
// when the range coder does not end at zero.
func ppmdDecode(order, mem uint32, src []byte, size int, w io.Writer) error {
	p := newPPMd7(mem, order)
	if !p.rc.init(src) {
		return errPPMd
	}
	p.restartModel()
	buf := make([]byte, 0, min(size, 32<<10))
	for n := 0; n < size; n++ {
		sym := p.decodeSymbol()
		if sym < 0 || p.bad || p.rc.bad || p.rc.pos > len(src) {
			return errPPMd
		}
		buf = append(buf, byte(sym))
		if len(buf) == cap(buf) {
			if _, err := w.Write(buf); err != nil {
				return fmt.Errorf("archive: writing PPMd data: %w", err)
			}
			buf = buf[:0]
		}
	}
	if len(buf) > 0 {
		if _, err := w.Write(buf); err != nil {
			return fmt.Errorf("archive: writing PPMd data: %w", err)
		}
	}
	if p.rc.pos > len(src) || p.rc.code != 0 {
		return errPPMd
	}
	return nil
}

// ppmdRange is the range decoder of 7z, CPpmd7z_RangeDec. A read past the
// end of the data gives zero and moves pos past the end, as the reader of
// 7-Zip sets Extra.
type ppmdRange struct {
	rng, code uint32
	src       []byte
	pos       int
	bad       bool // set when the range becomes too small to divide
}

// next reads a byte.
func (r *ppmdRange) next() uint32 {
	var b byte
	if r.pos < len(r.src) {
		b = r.src[r.pos]
	}
	r.pos++
	return uint32(b)
}

// init is Ppmd7z_RangeDec_Init.
func (r *ppmdRange) init(src []byte) bool {
	r.src = src
	r.rng = 0xFFFFFFFF
	if r.next() != 0 {
		return false
	}
	for range 4 {
		r.code = r.code<<8 | r.next()
	}
	return r.code < 0xFFFFFFFF && r.pos <= len(src)
}

// normalize is the normalization of Ppmd7Dec.c, of two bytes at most.
func (r *ppmdRange) normalize() {
	if r.rng < ppmdTopValue {
		r.code = r.code<<8 | r.next()
		r.rng <<= 8
		if r.rng < ppmdTopValue {
			r.code = r.code<<8 | r.next()
			r.rng <<= 8
		}
	}
}

// threshold is RC_GetThreshold. Data that 7-Zip writes never makes the
// range less than total, and other data sets bad.
func (r *ppmdRange) threshold(total uint32) uint32 {
	r.rng /= total
	if r.rng == 0 {
		r.rng, r.bad = 1, true
	}
	return r.code / r.rng
}

// decode is Ppmd7z_RD_Decode.
func (r *ppmdRange) decode(start, size uint32) {
	r.code -= start * r.rng
	r.rng *= size
	r.normalize()
}

// ppmdSee is CPpmd_See, the context of an escape.
type ppmdSee struct {
	summ  uint16
	shift byte
	count byte
}

// update is Ppmd_See_UPDATE.
func (s *ppmdSee) update() {
	if s.shift < ppmdPeriodBits {
		s.count--
		if s.count == 0 {
			s.summ <<= 1
			s.count = byte(3 << s.shift)
			s.shift++
		}
	}
}

// ppmd7 is CPpmd7, the model of PPMd variant H.
//
// The model lives in mem, as the model of 7-Zip lives in one block of
// memory, and a reference is an offset in mem, with 0 for none. A context
// has 12 bytes: the number of states (16 bits), the sum of the frequencies
// (16 bits), the reference of the states, and the reference of the suffix.
// A state has 6 bytes: the symbol, the frequency, and the reference of the
// successor. A context of one state holds it from its byte 2, over the sum
// and the reference of the states.
type ppmd7 struct {
	mem   []byte
	size  uint32
	align uint32

	text, unitsStart, loUnit, hiUnit uint32
	glueCount                        uint32
	freeList                         [ppmdNumIndexes]uint32

	minContext, maxContext, foundState uint32
	orderFall, initEsc, prevSuccess    uint32
	maxOrder, hiBitsFlag               uint32
	runLength, initRL                  int32

	indx2Units [ppmdNumIndexes]byte
	units2Indx [128]byte
	ns2Indx    [256]byte
	ns2BSIndx  [256]byte
	see        [25][16]ppmdSee
	dummySee   ppmdSee
	binSumm    [128][64]uint16

	rc ppmdRange

	// bad is set when the model is not consistent, which a correct model
	// never is. It stops the decoder before an index goes out of range.
	bad bool
	ps  [256]uint32
}

// newPPMd7 is Ppmd7_Construct and Ppmd7_Alloc. The memory has room for
// the head of the list of free blocks after its end, as 7-Zip allocates
// it.
func newPPMd7(size, order uint32) *ppmd7 {
	p := &ppmd7{size: size, maxOrder: order, align: 4 - size&3}
	p.mem = make([]byte, p.align+size+ppmdUnitSize)
	k := 0
	for i := range ppmdNumIndexes {
		step := 4
		if i < 12 {
			step = i>>2 + 1
		}
		for ; step > 0; step-- {
			p.units2Indx[k] = byte(i)
			k++
		}
		p.indx2Units[i] = byte(k)
	}
	p.ns2BSIndx[0] = 0 << 1
	p.ns2BSIndx[1] = 1 << 1
	for i := 2; i < 11; i++ {
		p.ns2BSIndx[i] = 2 << 1
	}
	for i := 11; i < 256; i++ {
		p.ns2BSIndx[i] = 3 << 1
	}
	for i := range 3 {
		p.ns2Indx[i] = byte(i)
	}
	m, step := 3, 1
	for i := 3; i < 256; i++ {
		p.ns2Indx[i] = byte(m)
		step--
		if step == 0 {
			m++
			step = m - 2
		}
	}
	p.dummySee = ppmdSee{shift: ppmdPeriodBits, count: 64}
	return p
}

func (p *ppmd7) u16(o uint32) uint32 { return uint32(binary.LittleEndian.Uint16(p.mem[o:])) }
func (p *ppmd7) u32(o uint32) uint32 { return binary.LittleEndian.Uint32(p.mem[o:]) }
func (p *ppmd7) setU16(o, v uint32)  { binary.LittleEndian.PutUint16(p.mem[o:], uint16(v)) }
func (p *ppmd7) setU32(o, v uint32)  { binary.LittleEndian.PutUint32(p.mem[o:], v) }

func (p *ppmd7) numStats(c uint32) uint32  { return p.u16(c) }
func (p *ppmd7) setNumStats(c, v uint32)   { p.setU16(c, v) }
func (p *ppmd7) summFreq(c uint32) uint32  { return p.u16(c + 2) }
func (p *ppmd7) setSummFreq(c, v uint32)   { p.setU16(c+2, v) }
func (p *ppmd7) stats(c uint32) uint32     { return p.u32(c + 4) }
func (p *ppmd7) setStats(c, v uint32)      { p.setU32(c+4, v) }
func (p *ppmd7) suffix(c uint32) uint32    { return p.u32(c + 8) }
func (p *ppmd7) setSuffix(c, v uint32)     { p.setU32(c+8, v) }
func oneState(c uint32) uint32             { return c + 2 }
func (p *ppmd7) symbol(s uint32) uint32    { return uint32(p.mem[s]) }
func (p *ppmd7) freq(s uint32) uint32      { return uint32(p.mem[s+1]) }
func (p *ppmd7) setFreq(s, v uint32)       { p.mem[s+1] = byte(v) }
func (p *ppmd7) successor(s uint32) uint32 { return p.u32(s + 2) }
func (p *ppmd7) setSuccessor(s, v uint32)  { p.setU32(s+2, v) }
func (p *ppmd7) copyState(dst, src uint32) {
	copy(p.mem[dst:dst+ppmdStateSize], p.mem[src:src+ppmdStateSize])
}
func (p *ppmd7) i2u(i uint32) uint32            { return uint32(p.indx2Units[i]) }
func (p *ppmd7) u2i(nu uint32) uint32           { return uint32(p.units2Indx[nu-1]) }
func (p *ppmd7) loadState(s uint32) [6]byte     { return [6]byte(p.mem[s : s+ppmdStateSize]) }
func (p *ppmd7) storeState(s uint32, t [6]byte) { copy(p.mem[s:s+ppmdStateSize], t[:]) }

// swapStates swaps the states a and b.
func (p *ppmd7) swapStates(a, b uint32) {
	t := p.loadState(a)
	p.copyState(a, b)
	p.storeState(b, t)
}

// b2u is 1 for true and 0 for false, as a comparison of C gives.
func b2u(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// findState gives the state of the symbol sym in the context c of more
// than one state, or 0 when c has none.
func (p *ppmd7) findState(c, sym uint32) uint32 {
	s := p.stats(c)
	for range p.numStats(c) {
		if p.symbol(s) == sym {
			return s
		}
		s += ppmdStateSize
	}
	p.bad = true
	return 0
}

// insertNode is InsertNode: it puts the block node of the index indx on
// its free list.
func (p *ppmd7) insertNode(node, indx uint32) {
	p.setU32(node, p.freeList[indx])
	p.freeList[indx] = node
}

// removeNode is RemoveNode: it takes a block from the free list indx.
func (p *ppmd7) removeNode(indx uint32) uint32 {
	node := p.freeList[indx]
	p.freeList[indx] = p.u32(node)
	return node
}

// splitBlock is SplitBlock: it frees the units of the block at ptr past
// the units of newIndx.
func (p *ppmd7) splitBlock(ptr, oldIndx, newIndx uint32) {
	nu := p.i2u(oldIndx) - p.i2u(newIndx)
	ptr += p.i2u(newIndx) * ppmdUnitSize
	i := p.u2i(nu)
	if p.i2u(i) != nu {
		i--
		k := p.i2u(i)
		p.insertNode(ptr+k*ppmdUnitSize, nu-k-1)
	}
	p.insertNode(ptr, i)
}

// glueFreeBlocks is GlueFreeBlocks: it joins the free blocks that are next
// to each other, and puts them on the free lists again. A node is a free
// block: a stamp of 0 (16 bits), its number of units (16 bits), and the
// next and the previous node of a list with a head after the memory. A
// block in use never has a stamp of 0, because its first 16 bits are the
// number of states of a context, or the symbol and the frequency of a
// state.
func (p *ppmd7) glueFreeBlocks() {
	head := p.align + p.size
	n := head
	p.glueCount = 255
	for i := range uint32(ppmdNumIndexes) {
		nu := p.i2u(i)
		next := p.freeList[i]
		p.freeList[i] = 0
		for next != 0 {
			node := next
			p.setU32(node+4, n)
			p.setU32(n+8, node)
			n = node
			next = p.u32(node)
			p.setU16(node, 0)
			p.setU16(node+2, nu)
		}
	}
	p.setU16(head, 1)
	p.setU32(head+4, n)
	p.setU32(n+8, head)
	if p.loUnit != p.hiUnit {
		p.setU16(p.loUnit, 1)
	}
	for n != head {
		node := n
		nu := p.u16(node + 2)
		for {
			node2 := node + nu*ppmdUnitSize
			nu += p.u16(node2 + 2)
			if p.u16(node2) != 0 || nu >= 0x10000 {
				break
			}
			prev, next := p.u32(node2+8), p.u32(node2+4)
			p.setU32(prev+4, next)
			p.setU32(next+8, prev)
			p.setU16(node+2, nu)
		}
		n = p.u32(node + 4)
	}
	for n = p.u32(head + 4); n != head; {
		node := n
		next := p.u32(node + 4)
		nu := p.u16(node + 2)
		for ; nu > 128; nu, node = nu-128, node+128*ppmdUnitSize {
			p.insertNode(node, ppmdNumIndexes-1)
		}
		i := p.u2i(nu)
		if p.i2u(i) != nu {
			i--
			k := p.i2u(i)
			p.insertNode(node+k*ppmdUnitSize, nu-k-1)
		}
		p.insertNode(node, i)
		n = next
	}
}

// allocUnitsRare is AllocUnitsRare: it allocates the units of indx when
// its free list is empty and the gap between loUnit and hiUnit is too
// small. It gives 0 when the memory is full.
func (p *ppmd7) allocUnitsRare(indx uint32) uint32 {
	if p.glueCount == 0 {
		p.glueFreeBlocks()
		if p.freeList[indx] != 0 {
			return p.removeNode(indx)
		}
	}
	i := indx
	for {
		i++
		if i == ppmdNumIndexes {
			numBytes := p.i2u(indx) * ppmdUnitSize
			p.glueCount--
			if p.unitsStart-p.text > numBytes {
				p.unitsStart -= numBytes
				return p.unitsStart
			}
			return 0
		}
		if p.freeList[i] != 0 {
			break
		}
	}
	block := p.removeNode(i)
	p.splitBlock(block, i, indx)
	return block
}

// allocUnits is AllocUnits.
func (p *ppmd7) allocUnits(indx uint32) uint32 {
	if p.freeList[indx] != 0 {
		return p.removeNode(indx)
	}
	numBytes := p.i2u(indx) * ppmdUnitSize
	if p.hiUnit-p.loUnit >= numBytes {
		lo := p.loUnit
		p.loUnit += numBytes
		return lo
	}
	return p.allocUnitsRare(indx)
}

// shrinkUnits is ShrinkUnits.
func (p *ppmd7) shrinkUnits(old, oldNU, newNU uint32) uint32 {
	i0, i1 := p.u2i(oldNU), p.u2i(newNU)
	if i0 == i1 {
		return old
	}
	if p.freeList[i1] != 0 {
		ptr := p.removeNode(i1)
		copy(p.mem[ptr:ptr+newNU*ppmdUnitSize], p.mem[old:old+newNU*ppmdUnitSize])
		p.insertNode(old, i0)
		return ptr
	}
	p.splitBlock(old, i0, i1)
	return old
}

// restartModel is RestartModel and Ppmd7_Init: it empties the memory and
// makes the context of order 0, with each of the 256 symbols.
func (p *ppmd7) restartModel() {
	clear(p.freeList[:])
	p.text = p.align
	p.hiUnit = p.text + p.size
	p.loUnit = p.hiUnit - p.size/8/ppmdUnitSize*7*ppmdUnitSize
	p.unitsStart = p.loUnit
	p.glueCount = 0

	p.orderFall = p.maxOrder
	p.initRL = -int32(min(p.maxOrder, 12)) - 1
	p.runLength = p.initRL
	p.prevSuccess = 0

	p.hiUnit -= ppmdUnitSize
	mc := p.hiUnit
	p.minContext, p.maxContext = mc, mc
	p.setSuffix(mc, 0)
	p.setNumStats(mc, 256)
	p.setSummFreq(mc, 256+1)
	p.foundState = p.loUnit
	p.setStats(mc, p.loUnit)
	for i := range uint32(256) {
		s := p.loUnit + i*ppmdStateSize
		p.mem[s] = byte(i)
		p.mem[s+1] = 1
		p.setSuccessor(s, 0)
	}
	p.loUnit += 256 / 2 * ppmdUnitSize

	for i := range 128 {
		for k := range 8 {
			v := uint16(ppmdBinScale - uint32(ppmdInitBinEsc[k])/uint32(i+2))
			for m := 0; m < 64; m += 8 {
				p.binSumm[i][k+m] = v
			}
		}
	}
	for i := range 25 {
		for k := range 16 {
			p.see[i][k] = ppmdSee{summ: uint16((5*i + 10) << (ppmdPeriodBits - 4)), shift: ppmdPeriodBits - 4, count: 4}
		}
	}
}

// createSuccessors is CreateSuccessors: it makes the contexts of the
// symbol of foundState, from minContext down to the context whose
// successor is not the text that upBranch points to. It gives 0 when the
// memory is full.
func (p *ppmd7) createSuccessors(skip bool) uint32 {
	c := p.minContext
	upBranch := p.successor(p.foundState)
	sym := p.symbol(p.foundState)
	var ps [ppmdMaxOrder]uint32
	n := 0
	if !skip {
		ps[n] = p.foundState
		n++
	}
	for p.suffix(c) != 0 {
		c = p.suffix(c)
		var s uint32
		if p.numStats(c) != 1 {
			if s = p.findState(c, sym); s == 0 {
				return 0
			}
		} else {
			s = oneState(c)
		}
		if succ := p.successor(s); succ != upBranch {
			c = succ
			if n == 0 {
				return c
			}
			break
		}
		if n == len(ps) {
			p.bad = true
			return 0
		}
		ps[n] = s
		n++
	}
	if upBranch == 0 || upBranch >= p.unitsStart {
		p.bad = true
		return 0
	}
	upSym := uint32(p.mem[upBranch])
	var upFreq uint32
	if p.numStats(c) == 1 {
		upFreq = p.freq(oneState(c))
	} else {
		s := p.findState(c, upSym)
		if s == 0 {
			return 0
		}
		cf := p.freq(s) - 1
		s0 := p.summFreq(c) - p.numStats(c) - cf
		switch {
		case 2*cf <= s0:
			upFreq = 1 + b2u(5*cf > s0)
		case s0 == 0:
			p.bad = true
			return 0
		default:
			upFreq = 1 + (2*cf+3*s0-1)/(2*s0)
		}
	}
	for n > 0 {
		var c1 uint32
		switch {
		case p.hiUnit != p.loUnit:
			p.hiUnit -= ppmdUnitSize
			c1 = p.hiUnit
		case p.freeList[0] != 0:
			c1 = p.removeNode(0)
		default:
			if c1 = p.allocUnitsRare(0); c1 == 0 {
				return 0
			}
		}
		p.setNumStats(c1, 1)
		p.mem[c1+2] = byte(upSym)
		p.mem[c1+3] = byte(upFreq)
		p.setU32(c1+4, upBranch+1)
		p.setSuffix(c1, c)
		n--
		p.setSuccessor(ps[n], c1)
		c = c1
	}
	return c
}

// updateModel is UpdateModel: it adds the symbol of foundState to the
// contexts from maxContext down to minContext, where the decoder found
// it.
func (p *ppmd7) updateModel() {
	fs := p.foundState
	fSym := p.symbol(fs)
	fSuccessor := p.successor(fs)
	if p.freq(fs) < ppmdMaxFreq/4 && p.suffix(p.minContext) != 0 {
		c := p.suffix(p.minContext)
		if p.numStats(c) == 1 {
			s := oneState(c)
			if p.freq(s) < 32 {
				p.mem[s+1]++
			}
		} else {
			s := p.stats(c)
			if p.symbol(s) != fSym {
				if s = p.findState(c, fSym); s == 0 {
					return
				}
				if p.freq(s) >= p.freq(s-ppmdStateSize) {
					p.swapStates(s, s-ppmdStateSize)
					s -= ppmdStateSize
				}
			}
			if p.freq(s) < ppmdMaxFreq-9 {
				p.mem[s+1] += 2
				p.setSummFreq(c, p.summFreq(c)+2)
			}
		}
	}

	if p.orderFall == 0 {
		c := p.createSuccessors(true)
		p.minContext, p.maxContext = c, c
		if c == 0 {
			p.restartModel()
			return
		}
		p.setSuccessor(p.foundState, c)
		return
	}

	p.mem[p.text] = byte(fSym)
	p.text++
	successor := p.text
	if p.text >= p.unitsStart {
		p.restartModel()
		return
	}

	if fSuccessor != 0 {
		if fSuccessor <= successor {
			cs := p.createSuccessors(false)
			if cs == 0 {
				p.restartModel()
				return
			}
			fSuccessor = cs
		}
		p.orderFall--
		if p.orderFall == 0 {
			successor = fSuccessor
			if p.maxContext != p.minContext {
				p.text--
			}
		}
	} else {
		p.setSuccessor(p.foundState, successor)
		fSuccessor = p.minContext
	}

	ns := p.numStats(p.minContext)
	s0 := p.summFreq(p.minContext) - ns - (p.freq(p.foundState) - 1)

	for c := p.maxContext; c != p.minContext; c = p.suffix(c) {
		if c == 0 {
			p.bad = true
			return
		}
		ns1 := p.numStats(c)
		if ns1 != 1 {
			if ns1&1 == 0 {
				// The states fill their units, so they take one more.
				oldNU := ns1 >> 1
				i := p.u2i(oldNU)
				if i != p.u2i(oldNU+1) {
					ptr := p.allocUnits(i + 1)
					if ptr == 0 {
						p.restartModel()
						return
					}
					old := p.stats(c)
					copy(p.mem[ptr:ptr+oldNU*ppmdUnitSize], p.mem[old:old+oldNU*ppmdUnitSize])
					p.insertNode(old, i)
					p.setStats(c, ptr)
				}
			}
			sf := p.summFreq(c)
			sf += b2u(2*ns1 < ns) + 2*(b2u(4*ns1 <= ns)&b2u(sf <= 8*ns1))
			p.setSummFreq(c, sf)
		} else {
			s := p.allocUnits(0)
			if s == 0 {
				p.restartModel()
				return
			}
			p.copyState(s, oneState(c))
			p.setStats(c, s)
			f := p.freq(s)
			if f < ppmdMaxFreq/4-1 {
				f <<= 1
			} else {
				f = ppmdMaxFreq - 4
			}
			p.setFreq(s, f)
			p.setSummFreq(c, f+p.initEsc+b2u(ns > 3))
		}
		cf := 2 * p.freq(p.foundState) * (p.summFreq(c) + 6)
		sf := s0 + p.summFreq(c)
		if cf < 6*sf {
			cf = 1 + b2u(cf > sf) + b2u(cf >= 4*sf)
			p.setSummFreq(c, p.summFreq(c)+3)
		} else {
			cf = 4 + b2u(cf >= 9*sf) + b2u(cf >= 12*sf) + b2u(cf >= 15*sf)
			p.setSummFreq(c, p.summFreq(c)+cf)
		}
		s := p.stats(c) + ns1*ppmdStateSize
		p.setSuccessor(s, successor)
		p.mem[s] = byte(fSym)
		p.mem[s+1] = byte(cf)
		p.setNumStats(c, ns1+1)
	}
	p.maxContext, p.minContext = fSuccessor, fSuccessor
}

// rescale is Rescale: it halves the frequencies of minContext, and drops
// the states whose frequency becomes 0.
func (p *ppmd7) rescale() {
	mc := p.minContext
	stats := p.stats(mc)
	s := p.foundState
	// The found state moves to the front.
	t := p.loadState(s)
	for ; s != stats; s -= ppmdStateSize {
		p.copyState(s, s-ppmdStateSize)
	}
	p.storeState(s, t)

	escFreq := p.summFreq(mc) - p.freq(s)
	adder := b2u(p.orderFall != 0)
	f := (p.freq(s) + 4 + adder) >> 1
	p.setFreq(s, f)
	sumFreq := f

	i := p.numStats(mc) - 1
	for ; i > 0; i-- {
		s += ppmdStateSize
		escFreq -= p.freq(s)
		f := (p.freq(s) + adder) >> 1
		p.setFreq(s, f)
		sumFreq += f
		if f > p.freq(s-ppmdStateSize) {
			s1 := s
			t := p.loadState(s1)
			for {
				p.copyState(s1, s1-ppmdStateSize)
				s1 -= ppmdStateSize
				if s1 == stats || uint32(t[1]) <= p.freq(s1-ppmdStateSize) {
					break
				}
			}
			p.storeState(s1, t)
		}
	}

	if p.freq(s) == 0 {
		numStats := p.numStats(mc)
		for {
			i++
			s -= ppmdStateSize
			if p.freq(s) != 0 {
				break
			}
		}
		escFreq += i
		p.setNumStats(mc, numStats-i)
		if p.numStats(mc) == 1 {
			t := p.loadState(stats)
			for {
				t[1] -= t[1] >> 1
				escFreq >>= 1
				if escFreq <= 1 {
					break
				}
			}
			p.insertNode(stats, p.u2i((numStats+1)>>1))
			p.foundState = oneState(mc)
			p.storeState(p.foundState, t)
			return
		}
		n0 := (numStats + 1) >> 1
		n1 := (p.numStats(mc) + 1) >> 1
		if n0 != n1 {
			p.setStats(mc, p.shrinkUnits(stats, n0, n1))
		}
	}
	p.setSummFreq(mc, sumFreq+escFreq-escFreq>>1)
	p.foundState = p.stats(mc)
}

// makeEscFreq is Ppmd7_MakeEscFreq: it gives the context of the escape of
// minContext, where numMasked symbols are masked, and the frequency of the
// escape.
func (p *ppmd7) makeEscFreq(numMasked uint32) (*ppmdSee, uint32) {
	mc := p.minContext
	numStats := p.numStats(mc)
	if numStats == 256 {
		return &p.dummySee, 1
	}
	nonMasked := numStats - numMasked
	j := b2u(nonMasked < p.numStats(p.suffix(mc))-numStats) +
		2*b2u(p.summFreq(mc) < 11*numStats) +
		4*b2u(numMasked > nonMasked) +
		p.hiBitsFlag
	see := &p.see[p.ns2Indx[nonMasked-1]][j]
	r := uint32(see.summ >> see.shift)
	see.summ -= uint16(r)
	return see, r + b2u(r == 0)
}

// nextContext is NextContext.
func (p *ppmd7) nextContext() {
	c := p.successor(p.foundState)
	if p.orderFall == 0 && c > p.text {
		p.minContext, p.maxContext = c, c
	} else {
		p.updateModel()
	}
}

// update1 is Ppmd7_Update1: the decoder found a symbol after the first in
// minContext.
func (p *ppmd7) update1() {
	s := p.foundState
	p.setFreq(s, p.freq(s)+4)
	p.setSummFreq(p.minContext, p.summFreq(p.minContext)+4)
	if p.freq(s) > p.freq(s-ppmdStateSize) {
		p.swapStates(s, s-ppmdStateSize)
		s -= ppmdStateSize
		p.foundState = s
		if p.freq(s) > ppmdMaxFreq {
			p.rescale()
		}
	}
	p.nextContext()
}

// update1First is Ppmd7_Update1_0: the decoder found the first symbol of
// minContext.
func (p *ppmd7) update1First() {
	p.prevSuccess = b2u(2*p.freq(p.foundState) > p.summFreq(p.minContext))
	p.runLength += int32(p.prevSuccess)
	p.setSummFreq(p.minContext, p.summFreq(p.minContext)+4)
	f := p.freq(p.foundState) + 4
	p.setFreq(p.foundState, f)
	if f > ppmdMaxFreq {
		p.rescale()
	}
	p.nextContext()
}

// updateBin is Ppmd7_UpdateBin: the decoder found the symbol of a context
// of one state.
func (p *ppmd7) updateBin() {
	f := p.freq(p.foundState)
	p.setFreq(p.foundState, f+b2u(f < 128))
	p.prevSuccess = 1
	p.runLength++
	p.nextContext()
}

// update2 is Ppmd7_Update2: the decoder found a symbol after an escape.
func (p *ppmd7) update2() {
	p.setSummFreq(p.minContext, p.summFreq(p.minContext)+4)
	f := p.freq(p.foundState) + 4
	p.setFreq(p.foundState, f)
	if f > ppmdMaxFreq {
		p.rescale()
	}
	p.runLength = p.initRL
	p.updateModel()
}

// hiBitsFlag3 is PPMD7_HiBitsFlag_3: 8 for a symbol of 0x40 or more.
func hiBitsFlag3(sym uint32) uint32 { return ((sym + 0xC0) >> 5) & (1 << 3) }

// hiBitsFlag4 is PPMD7_HiBitsFlag_4: 16 for a symbol of 0x40 or more.
func hiBitsFlag4(sym uint32) uint32 { return ((sym + 0xC0) >> 4) & (1 << 4) }

// decodeSymbol is Ppmd7z_DecodeSymbol. It gives the symbol, -1 for the
// end mark, or -2 for an error of the data.
func (p *ppmd7) decodeSymbol() int {
	var charMask [256]bool // true for a symbol that a context masks
	rc := &p.rc
	mc := p.minContext
	if p.numStats(mc) != 1 {
		s := p.stats(mc)
		summ := p.summFreq(mc)
		if summ == 0 {
			p.bad = true
			return -2
		}
		count := rc.threshold(summ)
		hiCnt := p.freq(s)
		if count < hiCnt {
			rc.decode(0, hiCnt)
			p.foundState = s
			sym := p.symbol(s)
			p.update1First()
			return int(sym)
		}
		p.prevSuccess = 0
		for i := p.numStats(mc) - 1; i > 0; i-- {
			s += ppmdStateSize
			hiCnt += p.freq(s)
			if hiCnt > count {
				rc.decode(hiCnt-p.freq(s), p.freq(s))
				p.foundState = s
				sym := p.symbol(s)
				p.update1()
				return int(sym)
			}
		}
		if count >= summ {
			return -2
		}
		p.hiBitsFlag = hiBitsFlag3(p.symbol(p.foundState))
		rc.decode(hiCnt, summ-hiCnt)
		for i, s := uint32(0), p.stats(mc); i < p.numStats(mc); i, s = i+1, s+ppmdStateSize {
			charMask[p.mem[s]] = true
		}
	} else {
		one := oneState(mc)
		f := p.freq(one)
		if f == 0 || f > 128 || p.suffix(mc) == 0 {
			p.bad = true
			return -2
		}
		prob := &p.binSumm[f-1][p.prevSuccess+
			uint32((p.runLength>>26)&0x20)+
			uint32(p.ns2BSIndx[p.numStats(p.suffix(mc))-1])+
			hiBitsFlag4(p.symbol(one))+
			p.setHiBitsFlag(hiBitsFlag3(p.symbol(p.foundState)))]
		size0 := (rc.rng >> 14) * uint32(*prob)
		if rc.code < size0 {
			rc.rng = size0
			rc.normalize()
			*prob = *prob + 1<<ppmdPeriodBits - ppmdGetMean(*prob)
			p.foundState = one
			sym := p.symbol(one)
			p.updateBin()
			return int(sym)
		}
		rc.code -= size0
		rc.rng -= size0
		rc.normalize()
		*prob -= ppmdGetMean(*prob)
		p.initEsc = uint32(ppmdExpEscape[*prob>>10])
		charMask[p.symbol(one)] = true
		p.prevSuccess = 0
	}
	for {
		numMasked := p.numStats(p.minContext)
		for {
			p.orderFall++
			if p.suffix(p.minContext) == 0 {
				return -1
			}
			p.minContext = p.suffix(p.minContext)
			if p.numStats(p.minContext) != numMasked {
				break
			}
		}
		mc := p.minContext
		numStats := p.numStats(mc)
		if numStats < numMasked {
			p.bad = true
			return -2
		}
		hiCnt := uint32(0)
		num := numStats - numMasked
		i := uint32(0)
		s := p.stats(mc)
		for k := uint32(0); i != num; k, s = k+1, s+ppmdStateSize {
			if k == numStats {
				p.bad = true
				return -2
			}
			if !charMask[p.mem[s]] {
				hiCnt += p.freq(s)
				p.ps[i] = s
				i++
			}
		}
		see, freqSum := p.makeEscFreq(numMasked)
		freqSum += hiCnt
		count := rc.threshold(freqSum)
		if count < hiCnt {
			k := 0
			hiCnt = p.freq(p.ps[0])
			for hiCnt <= count {
				k++
				hiCnt += p.freq(p.ps[k])
			}
			s := p.ps[k]
			rc.decode(hiCnt-p.freq(s), p.freq(s))
			see.update()
			p.foundState = s
			sym := p.symbol(s)
			p.update2()
			return int(sym)
		}
		if count >= freqSum {
			return -2
		}
		rc.decode(hiCnt, freqSum-hiCnt)
		see.summ += uint16(freqSum)
		for _, s := range p.ps[:num] {
			charMask[p.mem[s]] = true
		}
	}
}

// setHiBitsFlag sets hiBitsFlag to f and gives it, as the macro
// Ppmd7_GetBinSumm does.
func (p *ppmd7) setHiBitsFlag(f uint32) uint32 {
	p.hiBitsFlag = f
	return f
}

// ppmdGetMean is PPMD_GET_MEAN.
func ppmdGetMean(prob uint16) uint16 {
	return (prob + 1<<(ppmdPeriodBits-2)) >> ppmdPeriodBits
}
