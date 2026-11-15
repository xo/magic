package engine

import (
	"encoding/binary"
	"io"
)

// This file is the part of readelf.c that changes what the rules see.
// file_tryelf runs on a file before the rules, and its walk of the program
// headers sets the bits of the mode that ${x?a:b} reads: a dynamic section
// clears them, and the flag DF_1_PIE sets them again. So a shared library
// is application/x-sharedlib, and a PIE program is
// application/x-pie-executable.
//
// The descriptions that readelf.c prints come in phase 3 (docs/PLAN.md).

const (
	elfPhnumMax = 2048
	elfShnumMax = 32768
	elfNBufSize = 2024
	etExec      = 2
	etDyn       = 3
	ptDynamic   = 2
	ptInterp    = 3
	ptNote      = 4
	dtFlags1    = 0x6ffffffb
	df1PIE      = 0x08000000
)

// tryELF is file_tryelf, as far as it changes the mode of the file.
func (st *state) tryELF(b *buffer) {
	if st.flags&(Apple|Extension) != 0 || b.file == nil || b.file.r == nil {
		return
	}
	buf := b.data
	if len(buf) < 4 || buf[0] != 0x7f || (buf[1] != 'E' && buf[1] != 'O') || buf[2] != 'L' || buf[3] != 'F' {
		return
	}
	if len(buf) < 6 {
		return
	}
	class := buf[4]
	var hdrSize int
	switch class {
	case 1:
		hdrSize = 52
	case 2:
		hdrSize = 64
	default:
		return
	}
	if len(buf) <= hdrSize {
		return
	}
	// The data is in the byte order of the machine, little-endian, unless
	// EI_DATA says otherwise.
	var bo binary.ByteOrder = binary.LittleEndian
	if buf[5] != 1 {
		bo = binary.BigEndian
	}
	typ := bo.Uint16(buf[16:])
	if typ != etExec && typ != etDyn {
		return
	}
	var phoff uint64
	var phentsize, phnum, shnum uint16
	if class == 1 {
		phoff = uint64(bo.Uint32(buf[28:]))
		phentsize, phnum, shnum = bo.Uint16(buf[42:]), bo.Uint16(buf[44:]), bo.Uint16(buf[48:])
	} else {
		phoff = bo.Uint64(buf[32:])
		phentsize, phnum, shnum = bo.Uint16(buf[54:]), bo.Uint16(buf[56:]), bo.Uint16(buf[60:])
	}
	if phnum > elfPhnumMax || shnum > elfShnumMax {
		return
	}
	st.programHeaders(b.file.r, class, bo, phoff, int(phnum), int(phentsize), shnum)
}

// pread reads as pread does: as many bytes as there are, and false on an
// offset that a signed off_t cannot hold.
func pread(r io.ReaderAt, p []byte, off uint64) (int, bool) {
	if off > 1<<63-1 {
		return 0, false
	}
	n, _ := r.ReadAt(p, int64(off))
	return n, true
}

// programHeaders is dophn_exec, as far as it changes the mode.
func (st *state) programHeaders(r io.ReaderAt, class byte, bo binary.ByteOrder, off uint64, num, size int, shnum uint16) {
	phSize, dynSize := 32, 8
	if class == 2 {
		phSize, dynSize = 56, 16
	}
	if num == 0 || size != phSize {
		return
	}
	ph := make([]byte, phSize)
	nbuf := make([]byte, elfNBufSize)
	mime := st.flags&Mime != 0
	for ; num > 0; num-- {
		if n, ok := pread(r, ph, off); !ok || n < phSize {
			return
		}
		off += uint64(size)
		ptype := bo.Uint32(ph[0:])
		var poffset, pfilesz uint64
		if class == 1 {
			poffset, pfilesz = uint64(bo.Uint32(ph[4:])), uint64(bo.Uint32(ph[16:]))
		} else {
			poffset, pfilesz = bo.Uint64(ph[8:]), bo.Uint64(ph[32:])
		}
		switch ptype {
		case ptNote:
			if shnum != 0 {
				continue
			}
		case ptDynamic, ptInterp:
		default:
			continue
		}
		bufsize, ok := pread(r, nbuf[:min(pfilesz, elfNBufSize)], poffset)
		if !ok {
			return
		}
		switch ptype {
		case ptDynamic:
			// Let DF_1 decide whether the program is a PIE.
			st.mode &^= 0o111
			for o := 0; o+dynSize <= bufsize; o += dynSize {
				var tag, val uint64
				if class == 1 {
					tag, val = uint64(bo.Uint32(nbuf[o:])), uint64(bo.Uint32(nbuf[o+4:]))
				} else {
					tag, val = bo.Uint64(nbuf[o:]), bo.Uint64(nbuf[o+8:])
				}
				if tag == dtFlags1 {
					if val&df1PIE != 0 {
						st.mode |= 0o111
					} else {
						st.mode &^= 0o111
					}
				}
			}
		case ptNote:
			if mime {
				return
			}
		}
	}
}
