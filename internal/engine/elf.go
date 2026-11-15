package engine

import (
	"encoding/binary"
	"io"
)

// This file is readelf.c: file_tryelf reads the program headers, the
// section headers and the notes of an ELF file. It describes what the
// rules cannot: how the program is linked, its interpreter, the system and
// the build ID in its notes, and whether it is stripped. Its walk of the
// program headers also sets the bits of the mode that ${x?a:b} reads, so
// it changes the MIME type too.

const (
	elfPhnumMax  = 2048
	elfShnumMax  = 32768
	elfNotesMax  = 256
	elfShsizeMax = 128 * 1024 * 1024
	elfNBufSize  = 2024
	sizeUnknown  = -1

	etRel  = 1
	etExec = 2
	etDyn  = 3
	etCore = 4

	ptDynamic = 2
	ptInterp  = 3
	ptNote    = 4

	shtSymtab   = 2
	shtNote     = 7
	shtSunwCap  = 0x6ffffff5
	dtNeeded    = 1
	dtFlags1    = 0x6ffffffb
	df1PIE      = 0x08000000
	emSparc     = 2
	em386       = 3
	emSparc32P  = 18
	emSparcV9   = 43
	emIA64      = 50
	emAMD64     = 62
	caSunwNull  = 0
	caSunwHW1   = 1
	caSunwSF1   = 2
	sf1FPKnown  = 0x01
	sf1FPUsed   = 0x02
	sf1Mask     = 0x03
	ntPrpsinfo  = 3
	ntAuxv      = 6
	ntGNUVer    = 1
	ntGNUBuild  = 3
	ntGoBuild   = 4
	ntNetBSDVer = 1
	ntNetBSDEmu = 2
	ntNetBSDPax = 3
	ntNetBSDArc = 5
	ntNetBSDCM  = 6
	ntAndroidV  = 1
	ntAndroidMT = 4
	ntNetBSDPI  = 1

	atLinuxUID      = 11
	atLinuxEUID     = 12
	atLinuxGID      = 13
	atLinuxEGID     = 14
	atLinuxPlatform = 15
	atLinuxExecfn   = 31
)

// The flags of readelf.c.
const (
	flagsCoreStyle      = 0x0003
	flagsDidCore        = 0x0004
	flagsDidOSNote      = 0x0008
	flagsDidBuildID     = 0x0010
	flagsDidCoreStyle   = 0x0020
	flagsDidNetBSDPax   = 0x0040
	flagsDidNetBSDMarch = 0x0080
	flagsDidNetBSDCM    = 0x0100
	flagsDidNetBSDEmu   = 0x0200
	flagsDidNetBSDUnk   = 0x0400
	flagsDidMemtag      = 0x0800
	flagsIsCore         = 0x1000
	flagsDidAuxv        = 0x2000
)

const (
	osStyleSVR4 = iota
	osStyleFreeBSD
	osStyleNetBSD
)

var osStyleNames = [...]string{"SVR4", "FreeBSD", "NetBSD"}

// elfFile is what the functions of readelf.c share: the file, its class
// and its byte order.
type elfFile struct {
	st    *state
	r     io.ReaderAt
	class byte
	bo    binary.ByteOrder
	fsize int64
}

// elfPrintf is elf_printf: it prints nothing for a MIME output.
func (ef *elfFile) printf(format string, args ...carg) int {
	if ef.st.flags&Mime != 0 {
		return 1
	}
	return ef.st.put(cprintf([]byte(format), args...))
}

// pread is pread(2): the number of bytes read, or -1 for an offset that
// off_t cannot hold.
func (ef *elfFile) pread(p []byte, off int64) int {
	if off < 0 {
		return -1
	}
	n, _ := ef.r.ReadAt(p, off)
	return n
}

func (ef *elfFile) u16(b []byte) uint16 { return ef.bo.Uint16(b) }
func (ef *elfFile) u32(b []byte) uint32 { return ef.bo.Uint32(b) }
func (ef *elfFile) u64(b []byte) uint64 { return ef.bo.Uint64(b) }

// word reads a field that is 32 bits in a 32-bit file and 64 bits in a
// 64-bit one.
func (ef *elfFile) word(b []byte) uint64 {
	if ef.class == 1 {
		return uint64(ef.u32(b))
	}
	return ef.u64(b)
}

// phdr is a program header.
type phdr struct {
	typ                          uint32
	offset, vaddr, filesz, align uint64
}

func (ef *elfFile) phSize() int {
	if ef.class == 1 {
		return 32
	}
	return 56
}

func (ef *elfFile) parsePhdr(b []byte) phdr {
	var p phdr
	p.typ = ef.u32(b)
	if ef.class == 1 {
		p.offset, p.vaddr, p.filesz = uint64(ef.u32(b[4:])), uint64(ef.u32(b[8:])), uint64(ef.u32(b[16:]))
		p.align = uint64(ef.u32(b[28:]))
		if le.Uint32(b[28:]) == 0 {
			p.align = 4
		}
		if le.Uint32(b[8:]) == 0 {
			p.vaddr = 4
		}
	} else {
		p.offset, p.vaddr, p.filesz = ef.u64(b[8:]), ef.u64(b[16:]), ef.u64(b[32:])
		p.align = ef.u64(b[48:])
		if le.Uint64(b[48:]) == 0 {
			p.align = 4
		}
		if le.Uint64(b[16:]) == 0 {
			p.vaddr = 4
		}
	}
	return p
}

// tryELF is file_tryelf. It returns 1 for an ELF file, 0 for another file,
// and -1 on an error.
func (st *state) tryELF(b *buffer) int {
	if st.flags&(Apple|Extension) != 0 {
		return 0
	}
	buf := b.data
	if len(buf) < 4 || buf[0] != 0x7f || (buf[1] != 'E' && buf[1] != 'O') || buf[2] != 'L' || buf[3] != 'F' {
		return 0
	}
	ef := &elfFile{st: st, r: b.file.r, class: buf[4], fsize: sizeUnknown}
	if b.file.regular || b.file.size != 0 {
		ef.fsize = b.file.size
	}
	var hdrSize int
	switch ef.class {
	case 1:
		hdrSize = 52
	case 2:
		hdrSize = 64
	default:
		if ef.printf(", unknown class %d", cint(uint64(ef.class))) == -1 {
			return -1
		}
		return 0
	}
	if len(buf) <= hdrSize {
		return 0
	}
	// The data is in the byte order of the machine, little-endian, unless
	// EI_DATA says otherwise.
	ef.bo = binary.LittleEndian
	if buf[5] != 1 {
		ef.bo = binary.BigEndian
	}
	typ := ef.u16(buf[16:])
	notecount := elfNotesMax
	flags := 0
	var phoff, shoff uint64
	var phentsize, phnum, shentsize, shnum, shstrndx uint16
	if ef.class == 1 {
		phoff, shoff = uint64(ef.u32(buf[28:])), uint64(ef.u32(buf[32:]))
		phentsize, phnum = ef.u16(buf[42:]), ef.u16(buf[44:])
		shentsize, shnum, shstrndx = ef.u16(buf[46:]), ef.u16(buf[48:]), ef.u16(buf[50:])
	} else {
		phoff, shoff = ef.u64(buf[32:]), ef.u64(buf[40:])
		phentsize, phnum = ef.u16(buf[54:]), ef.u16(buf[56:])
		shentsize, shnum, shstrndx = ef.u16(buf[58:]), ef.u16(buf[60:]), ef.u16(buf[62:])
	}
	machine := int(ef.u16(buf[18:]))
	toomany := func(name string, n uint16) int {
		if ef.printf(", too many %s (%u)", cstringS(name), cint(uint64(n))) == -1 {
			return -1
		}
		return 1
	}
	switch typ {
	case etCore:
		if phnum > elfPhnumMax {
			return toomany("program headers", phnum)
		}
		flags |= flagsIsCore
		if ef.phnCore(int64(phoff), int(phnum), int(phentsize), &flags, &notecount) == -1 {
			return -1
		}
	case etExec, etDyn, etRel:
		if typ != etRel {
			if phnum > elfPhnumMax {
				return toomany("program", phnum)
			}
			if shnum > elfShnumMax {
				return toomany("section", shnum)
			}
			if ef.phnExec(int64(phoff), int(phnum), int(phentsize), int(shnum), &flags, &notecount) == -1 {
				return -1
			}
		}
		if shnum > elfShnumMax {
			return toomany("section headers", shnum)
		}
		if ef.shn(int64(shoff), int(shnum), int(shentsize), machine, int(shstrndx), &flags, &notecount) == -1 {
			return -1
		}
	}
	if notecount == 0 {
		return toomany("notes", elfNotesMax)
	}
	return 1
}

// phnCore is dophn_core: the notes of a core file.
func (ef *elfFile) phnCore(off int64, num, size int, flags, notecount *int) int {
	if ef.st.flags&Mime != 0 {
		return 0
	}
	if num == 0 {
		if ef.printf(", no program header") == -1 {
			return -1
		}
		return 0
	}
	if size != ef.phSize() {
		if ef.printf(", corrupted program header size") == -1 {
			return -1
		}
		return 0
	}
	phOff, phNum := off, num
	ph := make([]byte, size)
	nbuf := make([]byte, elfNBufSize)
	for ; num > 0; num-- {
		if ef.pread(ph, off) < size {
			if ef.printf(", can't read elf program headers at %jd", cint(uint64(off))) == -1 {
				return -1
			}
			return 0
		}
		off += int64(size)
		p := ef.parsePhdr(ph)
		if ef.fsize != sizeUnknown && int64(p.offset) > ef.fsize {
			continue
		}
		if p.typ != ptNote {
			continue
		}
		bufsize := ef.pread(nbuf[:min(p.filesz, elfNBufSize)], int64(p.offset))
		if bufsize == -1 {
			if ef.printf(" can't read note section at %jd", cint(p.offset)) == -1 {
				return -1
			}
			return 0
		}
		for offset := 0; offset < bufsize; {
			offset = ef.note(nbuf[:bufsize], offset, 4, flags, notecount, phOff, phNum)
			if offset == 0 {
				break
			}
		}
	}
	return 0
}

// phnExec is dophn_exec: how an executable is linked, and its
// interpreter. A dynamic section decides whether the program is a PIE.
func (ef *elfFile) phnExec(off int64, num, size, shnum int, flags, notecount *int) int {
	st := ef.st
	if num == 0 {
		if ef.printf(", no program header") == -1 {
			return -1
		}
		return 0
	}
	if size != ef.phSize() {
		if ef.printf(", corrupted program header size") == -1 {
			return -1
		}
		return 0
	}
	mime := st.flags&Mime != 0
	ph := make([]byte, size)
	nbuf := make([]byte, elfNBufSize)
	var interp []byte
	pie, dynamic, need := false, false, 0
	dynSize := 8
	if ef.class == 2 {
		dynSize = 16
	}
	bufsize := 0
	for ; num > 0; num-- {
		if ef.pread(ph, off) < size {
			if ef.printf(", can't read elf program headers at %jd", cint(uint64(off))) == -1 {
				return -1
			}
			return 0
		}
		off += int64(size)
		bufsize = 0
		align := uint64(4)
		p := ef.parsePhdr(ph)
		doread := false
		switch p.typ {
		case ptDynamic:
			doread = true
		case ptNote:
			if shnum != 0 {
				continue
			}
			if align = p.align; align&0x80000000 != 0 || align < 4 {
				if ef.printf(", invalid note alignment %#lx", cint(align)) == -1 {
					return -1
				}
				align = 4
			}
			doread = true
		case ptInterp:
			doread = true
		default:
			if ef.fsize != sizeUnknown && int64(p.offset) > ef.fsize {
				continue
			}
		}
		if doread {
			bufsize = ef.pread(nbuf[:min(p.filesz, elfNBufSize)], int64(p.offset))
			if bufsize == -1 {
				if ef.printf(", can't read section at %jd", cint(p.offset)) == -1 {
					return -1
				}
				return 0
			}
		}
		switch p.typ {
		case ptDynamic:
			dynamic = true
			// Let DF_1 decide whether the program is a PIE.
			st.mode &^= 0o111
			for o := 0; o+dynSize <= bufsize; o += dynSize {
				tag, val := ef.word(nbuf[o:]), ef.word(nbuf[o+dynSize/2:])
				switch tag {
				case dtFlags1:
					if val&df1PIE != 0 {
						pie = true
						st.mode |= 0o111
					} else {
						st.mode &^= 0o111
					}
				case dtNeeded:
					need++
				}
			}
		case ptInterp:
			need++
			if mime {
				continue
			}
			if bufsize > 0 && nbuf[0] != 0 {
				nbuf[bufsize-1] = 0
				interp = append(interp[:0], cstr(nbuf[:bufsize])...)
			} else {
				interp = []byte("*empty*")
			}
			if len(interp) > elfNBufSize-1 {
				interp = interp[:elfNBufSize-1]
			}
		case ptNote:
			if mime {
				return 0
			}
			for offset := 0; offset < bufsize; {
				offset = ef.note(nbuf[:bufsize], offset, int(align), flags, notecount, 0, 0)
				if offset == 0 {
					break
				}
			}
		default:
			if mime {
				continue
			}
		}
	}
	if mime {
		return 0
	}
	str := "statically"
	if dynamic {
		str = "dynamically"
		if pie && need == 0 {
			str = "static-pie"
		}
	}
	if ef.printf(", %s linked", cstringS(str)) == -1 {
		return -1
	}
	if len(interp) > 0 {
		if ef.printf(", interpreter %s", cstring(st.printableC(interp, elfNBufSize, elfNBufSize))) == -1 {
			return -1
		}
	}
	return 0
}

// nameEquals is NAMEEQUALS: the name of a note is v with its NUL.
func nameEquals(nbuf []byte, noff int, namesz uint32, v string) bool {
	if int(namesz) != len(v)+1 || noff+len(v)+1 > len(nbuf) {
		return false
	}
	return string(nbuf[noff:noff+len(v)]) == v && nbuf[noff+len(v)] == 0
}

// note is donote: it describes the note at offset of nbuf, and returns the
// offset of the next one, or 0 to stop.
func (ef *elfFile) note(nbuf []byte, offset, align int, flags, notecount *int, phOff int64, phNum int) int {
	if *notecount == 0 {
		return 0
	}
	*notecount--
	size := len(nbuf)
	ealign := func(a int) int { return (a + align - 1) / align * align }
	if 12+offset > size {
		return 12 + offset
	}
	namesz := ef.u32(nbuf[offset:])
	descsz := ef.u32(nbuf[offset+4:])
	typ := ef.u32(nbuf[offset+8:])
	offset += 12
	if namesz == 0 && descsz == 0 {
		return max(offset, size)
	}
	if namesz&0x80000000 != 0 {
		ef.printf(", bad note name size %#lx", cint(uint64(namesz)))
		return 0
	}
	if descsz&0x80000000 != 0 {
		ef.printf(", bad note description size %#lx", cint(uint64(descsz)))
		return 0
	}
	noff := offset
	doff := ealign(offset + int(namesz))
	if offset+int(namesz) > size {
		return doff
	}
	offset = ealign(doff + int(descsz))
	if doff+int(descsz) > size {
		return max(offset, size)
	}
	if *flags&flagsDidOSNote == 0 {
		if ef.osNote(nbuf, typ, namesz, descsz, noff, doff, flags) != 0 {
			return offset
		}
	}
	if *flags&flagsDidBuildID == 0 {
		if ef.bidNote(nbuf, typ, namesz, descsz, noff, doff, flags) != 0 {
			return offset
		}
	}
	if *flags&flagsDidNetBSDPax == 0 {
		if ef.bitsNote(nbuf, typ, namesz, descsz, noff, doff, flags, "PaX", ntNetBSDPax, flagsDidNetBSDPax,
			", PaX: ", []string{"+mprotect", "-mprotect", "+segvguard", "-segvguard", "+ASLR", "-ASLR"}) != 0 {
			return offset
		}
	}
	if *flags&flagsDidMemtag == 0 {
		if ef.bitsNote(nbuf, typ, namesz, descsz, noff, doff, flags, "Android", ntAndroidMT, flagsDidMemtag,
			", Android Memtag: ", []string{"none", "async", "sync", "heap", "stack"}) != 0 {
			return offset
		}
	}
	if *flags&flagsDidCore == 0 {
		if ef.coreNote(nbuf, typ, namesz, descsz, noff, doff, flags, size) != 0 {
			return offset
		}
	}
	if *flags&flagsDidAuxv == 0 {
		if ef.auxvNote(nbuf, typ, descsz, doff, flags, phOff, phNum) != 0 {
			return offset
		}
	}
	if nameEquals(nbuf, noff, namesz, "NetBSD") {
		if descsz > 100 {
			descsz = 100
		}
		var flag int
		var tag string
		switch typ {
		case ntNetBSDVer:
			return offset
		case ntNetBSDArc:
			flag, tag = flagsDidNetBSDMarch, "compiled for"
		case ntNetBSDCM:
			flag, tag = flagsDidNetBSDCM, "compiler model"
		case ntNetBSDEmu:
			flag, tag = flagsDidNetBSDEmu, "emulation:"
		default:
			if *flags&flagsDidNetBSDUnk != 0 {
				return offset
			}
			*flags |= flagsDidNetBSDUnk
			ef.printf(", note=%u", cint(uint64(typ)))
			return offset
		}
		if *flags&flag != 0 {
			return offset
		}
		*flags |= flag
		ef.printf(", %s: %s", cstringS(tag), cstring(copystr(nbuf[doff:], int(descsz), 256)))
		return offset
	}
	return offset
}

// copystr is file_copystr: width bytes of s, at most blen-1, as a C string.
func copystr(s []byte, width, blen int) []byte {
	if width >= blen {
		width = blen - 1
	}
	width = min(width, len(s))
	return cstr(s[:width])
}

// osNote is do_os_note.
func (ef *elfFile) osNote(nbuf []byte, typ, namesz, descsz uint32, noff, doff int, flags *int) int {
	eq := func(v string) bool { return nameEquals(nbuf, noff, namesz, v) }
	switch {
	case eq("SuSE") && typ == ntGNUVer && descsz == 2:
		*flags |= flagsDidOSNote
		if ef.printf(", for SuSE %d.%d", cint(uint64(nbuf[doff])), cint(uint64(nbuf[doff+1]))) == -1 {
			return -1
		}
		return 1
	case eq("GNU") && typ == ntGNUVer && descsz == 16:
		*flags |= flagsDidOSNote
		if ef.printf(", for GNU/") == -1 {
			return -1
		}
		os := "<unknown>"
		switch ef.u32(nbuf[doff:]) {
		case 0:
			os = "Linux"
		case 1:
			os = "Hurd"
		case 2:
			os = "Solaris"
		case 3:
			os = "kFreeBSD"
		case 4:
			os = "kNetBSD"
		}
		if ef.printf("%s", cstringS(os)) == -1 {
			return -1
		}
		if ef.printf(" %d.%d.%d", cint(uint64(ef.u32(nbuf[doff+4:]))), cint(uint64(ef.u32(nbuf[doff+8:]))),
			cint(uint64(ef.u32(nbuf[doff+12:])))) == -1 {
			return -1
		}
		return 1
	case eq("NetBSD") && typ == ntNetBSDVer && descsz == 4:
		*flags |= flagsDidOSNote
		if ef.netbsdVersion(ef.u32(nbuf[doff:])) == -1 {
			return -1
		}
		return 1
	case eq("FreeBSD") && typ == 1 && descsz == 4:
		*flags |= flagsDidOSNote
		if ef.freebsdVersion(ef.u32(nbuf[doff:])) == -1 {
			return -1
		}
		return 1
	case eq("OpenBSD") && typ == 1 && descsz == 4:
		*flags |= flagsDidOSNote
		if ef.printf(", for OpenBSD") == -1 {
			return -1
		}
		return 1
	case eq("DragonFly") && typ == 1 && descsz == 4:
		*flags |= flagsDidOSNote
		if ef.printf(", for DragonFly") == -1 {
			return -1
		}
		d := ef.u32(nbuf[doff:])
		if ef.printf(" %d.%d.%d", cint(uint64(d/100000)), cint(uint64(d/10000%10)), cint(uint64(d%10000))) == -1 {
			return -1
		}
		return 1
	case eq("Android") && typ == ntAndroidV && descsz >= 4:
		*flags |= flagsDidOSNote
		if ef.printf(", for Android %d", cint(uint64(ef.u32(nbuf[doff:])))) == -1 {
			return -1
		}
		if descsz >= 4+64+64 {
			if ef.printf(", built by NDK %.64s (%.64s)", cstring(nbuf[doff+4:]), cstring(nbuf[doff+4+64:])) == -1 {
				return -1
			}
		}
	}
	return 0
}

// netbsdVersion is do_note_netbsd_version.
func (ef *elfFile) netbsdVersion(desc uint32) int {
	if ef.printf(", for NetBSD") == -1 {
		return -1
	}
	if desc > 100000000 {
		patch := desc / 100 % 100
		rel := desc / 10000 % 100
		minor := desc / 1000000 % 100
		major := desc / 100000000
		if ef.printf(" %u.%u", cint(uint64(major)), cint(uint64(minor))) == -1 {
			return -1
		}
		if major >= 9 {
			patch += 100 * rel
			rel = 0
		}
		if rel == 0 && patch != 0 {
			if ef.printf(".%u", cint(uint64(patch))) == -1 {
				return -1
			}
		} else if rel != 0 {
			for rel > 26 {
				if ef.printf("Z") == -1 {
					return -1
				}
				rel -= 26
			}
			if ef.printf("%c", cint(uint64('A'+rel-1))) == -1 {
				return -1
			}
		}
	}
	return 0
}

// freebsdVersion is do_note_freebsd_version.
func (ef *elfFile) freebsdVersion(desc uint32) int {
	if ef.printf(", for FreeBSD") == -1 {
		return -1
	}
	d := func(v uint32) carg { return cint(uint64(v)) }
	switch {
	case desc == 460002:
		return ef.printf(" 4.6.2")
	case desc < 460100:
		if ef.printf(" %d.%d", d(desc/100000), d(desc/10000%10)) == -1 {
			return -1
		}
		if desc/1000%10 > 0 && ef.printf(".%d", d(desc/1000%10)) == -1 {
			return -1
		}
		if desc%1000 > 0 || desc%100000 == 0 {
			return ef.printf(" (%d)", d(desc))
		}
	case desc < 500000:
		if ef.printf(" %d.%d", d(desc/100000), d(desc/10000%10+desc/1000%10)) == -1 {
			return -1
		}
		if desc/100%10 > 0 {
			return ef.printf(" (%d)", d(desc))
		} else if desc/10%10 > 0 {
			return ef.printf(".%d", d(desc/10%10))
		}
	default:
		if ef.printf(" %d.%d", d(desc/100000), d(desc/1000%100)) == -1 {
			return -1
		}
		if desc/100%10 > 0 || desc%100000/100 == 0 {
			return ef.printf(" (%d)", d(desc))
		} else if desc/10%10 > 0 {
			return ef.printf(".%d", d(desc/10%10))
		}
	}
	return 0
}

// bidNote is do_bid_note: a GNU or Go build ID.
func (ef *elfFile) bidNote(nbuf []byte, typ, namesz, descsz uint32, noff, doff int, flags *int) int {
	if nameEquals(nbuf, noff, namesz, "GNU") && typ == ntGNUBuild && descsz >= 4 && descsz <= 20 {
		*flags |= flagsDidBuildID
		btype := "unknown"
		switch descsz {
		case 8:
			btype = "xxHash"
		case 16:
			btype = "md5/uuid"
		case 20:
			btype = "sha1"
		}
		if ef.printf(", BuildID[%s]=", cstringS(btype)) == -1 {
			return -1
		}
		for i := 0; i < int(descsz); i++ {
			if ef.printf("%02x", cint(uint64(nbuf[doff+i]))) == -1 {
				return -1
			}
		}
		return 1
	}
	if namesz == 4 && noff+3 <= len(nbuf) && string(nbuf[noff:noff+3]) == "Go\x00" && typ == ntGoBuild && descsz < 128 {
		if ef.printf(", Go BuildID=%s", cstring(copystr(nbuf[doff:], int(descsz), 256))) == -1 {
			return -1
		}
		return 1
	}
	return 0
}

// bitsNote is do_pax_note and do_memtag_note: a note of one word whose bits
// have names.
func (ef *elfFile) bitsNote(nbuf []byte, typ, namesz, descsz uint32, noff, doff int, flags *int,
	name string, ntype uint32, flag int, lead string, names []string) int {
	if !nameEquals(nbuf, noff, namesz, name) || typ != ntype || descsz != 4 {
		return 0
	}
	*flags |= flag
	desc := ef.u32(nbuf[doff:])
	if desc != 0 && ef.printf("%s", cstringS(lead)) == -1 {
		return -1
	}
	did := false
	for i, n := range names {
		if desc&(1<<i) == 0 {
			continue
		}
		sep := ""
		if did {
			sep = ","
		}
		did = true
		if ef.printf("%s%s", cstringS(sep), cstringS(n)) == -1 {
			return -1
		}
	}
	return 1
}

var (
	prpsoffsets32 = []int{100, 84, 44, 28, 48, 32, 8}
	prpsoffsets64 = []int{136, 120, 56, 40, 16}
)

// coreNote is do_core_note: the style of a core file and the program that
// dropped it.
func (ef *elfFile) coreNote(nbuf []byte, typ, namesz, descsz uint32, noff, doff int, flags *int, size int) int {
	name := nbuf[noff:]
	osStyle := -1
	if namesz == 4 && len(name) >= 4 && string(name[:4]) == "CORE" || nameEquals(nbuf, noff, namesz, "CORE") {
		osStyle = osStyleSVR4
	}
	if nameEquals(nbuf, noff, namesz, "FreeBSD") {
		osStyle = osStyleFreeBSD
	}
	if namesz >= 11 && len(name) >= 11 && string(name[:11]) == "NetBSD-CORE" {
		osStyle = osStyleNetBSD
	}
	if osStyle != -1 && *flags&flagsDidCoreStyle == 0 {
		if ef.printf(", %s-style", cstringS(osStyleNames[osStyle])) == -1 {
			return -1
		}
		*flags |= flagsDidCoreStyle | osStyle
	}
	switch osStyle {
	case osStyleNetBSD:
		if typ != ntNetBSDPI {
			break
		}
		var pi [160]byte
		copy(pi[:], nbuf[doff:min(doff+min(int(descsz), len(pi)), len(nbuf))])
		u := func(o int) carg { return cint(uint64(ef.u32(pi[o:]))) }
		if ef.printf(", from '%.31s', pid=%u, uid=%u, gid=%u, nlwps=%u, lwp=%u (signal %u/code %u)",
			cstring(ef.st.printableC(pi[124:156], 32, 512)), u(80), u(100), u(112), u(120), u(156), u(8), u(12)) == -1 {
			return -1
		}
		*flags |= flagsDidCore
		return 1
	case osStyleFreeBSD:
		if typ == ntPrpsinfo && *flags&flagsIsCore != 0 {
			argoff := 4 + 4 + 17
			if ef.class == 2 {
				argoff = 4 + 4 + 8 + 17
			}
			if doff+argoff+81 <= size {
				if ef.printf(", from '%.80s'", cstring(nbuf[doff+argoff:])) == -1 {
					return -1
				}
			}
			pidoff := argoff + 81 + 2
			if doff+pidoff+4 <= size {
				if ef.printf(", pid=%u", cint(uint64(ef.u32(nbuf[doff+pidoff:])))) == -1 {
					return -1
				}
			}
			*flags |= flagsDidCore
		}
	default:
		if typ != ntPrpsinfo || *flags&flagsIsCore == 0 {
			break
		}
		offs := prpsoffsets32
		if ef.class == 2 {
			offs = prpsoffsets64
		}
		isquote := func(c byte) bool { return c == '\'' || c == '"' || c == '`' }
	next:
		for i := 0; i < len(offs); i++ {
			reloffset := offs[i]
			noffset := doff + reloffset
			j := 0
			for ; j < 16; j, noffset, reloffset = j+1, noffset+1, reloffset+1 {
				if noffset >= size || reloffset >= int(descsz) {
					continue next
				}
				c := nbuf[noffset]
				if c == 0 {
					if j == 0 {
						continue next
					}
					break
				}
				if !isPrint(c) || isquote(c) {
					continue next
				}
			}
			// Try the next offsets, in case this match is in the middle of a
			// string.
			for k := i + 1; k < len(offs); k++ {
				if offs[k] >= offs[i] {
					continue
				}
				if offs[k] == offs[i]-16 && j == 16 {
					continue
				}
				adjust := true
				for no := doff + offs[k]; no < doff+offs[i]; no++ {
					adjust = adjust && isPrint(nbuf[no])
				}
				if adjust {
					i = k
				}
			}
			cname := doff + offs[i]
			cp := cname
			for cp < size && nbuf[cp] != 0 && isPrint(nbuf[cp]) {
				cp++
			}
			for cp > cname && isSpace(nbuf[cp-1]) {
				cp--
			}
			if ef.printf(", from '%s'", cstring(copystr(nbuf[cname:], cp-cname, 256))) == -1 {
				return -1
			}
			*flags |= flagsDidCore
			return 1
		}
	}
	return 0
}

// auxvNote is do_auxv_note: the auxiliary vector of a core file.
func (ef *elfFile) auxvNote(nbuf []byte, typ, descsz uint32, doff int, flags *int, phOff int64, phNum int) int {
	if *flags&(flagsIsCore|flagsDidCoreStyle) != flagsIsCore|flagsDidCoreStyle {
		return 0
	}
	if *flags&flagsCoreStyle != osStyleSVR4 || typ != ntAuxv {
		return 0
	}
	*flags |= flagsDidAuxv
	elsize := 8
	if ef.class == 2 {
		elsize = 16
	}
	nval := 0
	for off := 0; off+elsize <= int(descsz); off += elsize {
		if nval >= 50 {
			ef.st.errorf("Too many ELF Auxv elements")
			return 1
		}
		nval++
		e := nbuf[doff+off:]
		atype, aval := ef.word(e), ef.word(e[elsize/2:])
		var tag string
		isString := false
		switch atype {
		case atLinuxExecfn:
			tag, isString = "execfn", true
		case atLinuxPlatform:
			tag, isString = "platform", true
		case atLinuxUID:
			tag = "real uid"
		case atLinuxGID:
			tag = "real gid"
		case atLinuxEUID:
			tag = "effective uid"
		case atLinuxEGID:
			tag = "effective gid"
		default:
			continue
		}
		if isString {
			s, ok := ef.stringAt(phOff, phNum, aval)
			if !ok {
				continue
			}
			if ef.printf(", %s: '%s'", cstringS(tag), cstring(s)) == -1 {
				return -1
			}
		} else if ef.printf(", %s: %d", cstringS(tag), cint(aval)) == -1 {
			return -1
		}
	}
	return 1
}

// stringAt is get_string_on_virtaddr: the printable string at a virtual
// address of a core file.
func (ef *elfFile) stringAt(off int64, num int, va uint64) ([]byte, bool) {
	offset := int64(0)
	size := ef.phSize()
	ph := make([]byte, size)
	for ; num > 0; num-- {
		if ef.pread(ph, off) < size {
			ef.printf(", can't read elf program header at %jd", cint(uint64(off)))
			offset = 0
			break
		}
		off += int64(size)
		p := ef.parsePhdr(ph)
		if ef.fsize != sizeUnknown && int64(p.offset) > ef.fsize {
			continue
		}
		if va >= p.vaddr && va < p.vaddr+p.filesz {
			offset = int64(p.offset + va - p.vaddr)
			break
		}
	}
	buf := make([]byte, 256)
	n := -1
	if offset >= 0 {
		n = ef.pread(buf, offset)
	}
	if n <= 0 {
		ef.printf(", can't read elf string at %jd", cint(uint64(offset)))
		return nil, false
	}
	buf[n-1] = 0
	i := 0
	for buf[i] != 0 && isPrint(buf[i]) {
		i++
	}
	if buf[i] != 0 || i == 0 {
		return nil, false
	}
	return buf[:i], true
}

var capDescSparc = []struct {
	mask uint64
	name string
}{
	{0x0001, "MUL32"}, {0x0002, "DIV32"}, {0x0004, "FSMULD"}, {0x0008, "V8PLUS"},
	{0x0010, "POPC"}, {0x0020, "VIS"}, {0x0040, "VIS2"}, {0x0080, "ASI_BLK_INIT"},
	{0x0100, "FMAF"}, {0x4000, "FJFMAU"}, {0x8000, "IMA"},
}

var capDesc386 = []struct {
	mask uint64
	name string
}{
	{0x00000001, "FPU"}, {0x00000002, "TSC"}, {0x00000004, "CX8"}, {0x00000008, "SEP"},
	{0x00000010, "AMD_SYSC"}, {0x00000020, "CMOV"}, {0x00000040, "MMX"}, {0x00000080, "AMD_MMX"},
	{0x00000100, "AMD_3DNow"}, {0x00000200, "AMD_3DNowx"}, {0x00000400, "FXSR"}, {0x00000800, "SSE"},
	{0x00001000, "SSE2"}, {0x00002000, "PAUSE"}, {0x00004000, "SSE3"}, {0x00008000, "MON"},
	{0x00010000, "CX16"}, {0x00020000, "AHF"}, {0x00040000, "TSCP"}, {0x00080000, "AMD_SSE4A"},
	{0x00100000, "POPCNT"}, {0x00200000, "AMD_LZCNT"}, {0x00400000, "SSSE3"}, {0x00800000, "SSE4.1"},
	{0x01000000, "SSE4.2"},
}

// shn is doshn: the section headers, for the notes, the symbol table, the
// debug information and the capabilities of SunOS.
func (ef *elfFile) shn(off int64, num, size, mach, strtab int, flags, notecount *int) int {
	if ef.st.flags&Mime != 0 {
		return 0
	}
	if num == 0 {
		if ef.printf(", no section header") == -1 {
			return -1
		}
		return 0
	}
	shSize := 40
	if ef.class == 2 {
		shSize = 64
	}
	if size != shSize {
		if ef.printf(", corrupted section header size") == -1 {
			return -1
		}
		return 0
	}
	sh := make([]byte, shSize)
	type shdr struct {
		name, typ    uint32
		offset, size uint64
	}
	parse := func() shdr {
		if ef.class == 1 {
			return shdr{ef.u32(sh), ef.u32(sh[4:]), uint64(ef.u32(sh[16:])), uint64(ef.u32(sh[20:]))}
		}
		return shdr{ef.u32(sh), ef.u32(sh[4:]), ef.u64(sh[24:]), ef.u64(sh[32:])}
	}
	offs := off + int64(size)*int64(strtab)
	if ef.pread(sh, offs) < shSize {
		if ef.printf(", missing section headers at %jd", cint(uint64(offs))) == -1 {
			return -1
		}
		return 0
	}
	nameOff := int64(parse().offset)
	if ef.fsize != sizeUnknown && ef.fsize < nameOff {
		if ef.printf(", too large section header offset %jd", cint(uint64(nameOff))) == -1 {
			return -1
		}
		return 0
	}
	stripped, debugInfo := true, false
	nbadcap := 0
	var capHW1, capSF1 uint64
	name := make([]byte, 49)
	for ; num > 0; num-- {
		offs = nameOff + int64(parse().name)
		n := ef.pread(name, offs)
		if n == -1 {
			if ef.printf(", can't read name of elf section at %jd", cint(uint64(offs))) == -1 {
				return -1
			}
			return 0
		}
		if string(cstr(name[:n])) == ".debug_info" {
			debugInfo, stripped = true, false
		}
		if ef.pread(sh, off) < shSize {
			if ef.printf(", can't read elf section at %jd", cint(uint64(off))) == -1 {
				return -1
			}
			return 0
		}
		off += int64(size)
		s := parse()
		if s.typ == shtSymtab {
			stripped = false
		} else if ef.fsize != sizeUnknown && int64(s.offset) > ef.fsize {
			continue
		}
		switch s.typ {
		case shtNote:
			if s.size+s.offset > uint64(ef.fsize) {
				if ef.printf(", note offset/size %#jx+%#jx exceeds file size %#jx",
					cint(s.offset), cint(s.size), cint(uint64(ef.fsize))) == -1 {
					return -1
				}
				return 0
			}
			if s.size > elfShsizeMax {
				ef.st.errorf("Note section size too big (%d > %d)", s.size, elfShsizeMax)
				return -1
			}
			nbuf := make([]byte, s.size)
			if ef.pread(nbuf, int64(s.offset)) < len(nbuf) {
				if ef.printf(", can't read elf note at %jd", cint(s.offset)) == -1 {
					return -1
				}
				return 0
			}
			for noff := 0; noff < len(nbuf); {
				noff = ef.note(nbuf, noff, 4, flags, notecount, 0, 0)
				if noff == 0 {
					break
				}
			}
		case shtSunwCap:
			switch mach {
			case emSparc, emSparcV9, emIA64, em386, emAMD64:
			default:
				continue
			}
			if nbadcap > 5 {
				continue
			}
			csize := 8
			if ef.class == 2 {
				csize = 16
			}
			cbuf := make([]byte, csize)
			pos := int64(s.offset)
		caps:
			for coff := csize; uint64(coff) <= s.size; coff += csize {
				if ef.pread(cbuf, pos) != csize {
					ef.st.errorf("error reading")
					return -1
				}
				pos += int64(csize)
				if cbuf[0] == 'A' {
					break
				}
				tag, val := ef.word(cbuf), ef.word(cbuf[csize/2:])
				switch tag {
				case caSunwNull:
				case caSunwHW1:
					capHW1 |= val
				case caSunwSF1:
					capSF1 |= val
				default:
					if ef.printf(", with unknown capability %#llx = %#llx", cint(tag), cint(val)) == -1 {
						return -1
					}
					nbadcap++
					if nbadcap > 3 {
						break caps
					}
				}
			}
		}
	}
	if debugInfo && ef.printf(", with debug_info") == -1 {
		return -1
	}
	not := "not "
	if stripped {
		not = ""
	}
	if ef.printf(", %sstripped", cstringS(not)) == -1 {
		return -1
	}
	if capHW1 != 0 {
		var cdp []struct {
			mask uint64
			name string
		}
		switch mach {
		case emSparc, emSparc32P, emSparcV9:
			cdp = capDescSparc
		case em386, emIA64, emAMD64:
			cdp = capDesc386
		}
		if ef.printf(", uses") == -1 {
			return -1
		}
		if cdp != nil {
			for _, c := range cdp {
				if capHW1&c.mask != 0 {
					if ef.printf(" %s", cstringS(c.name)) == -1 {
						return -1
					}
					capHW1 &^= c.mask
				}
			}
			if capHW1 != 0 && ef.printf(" unknown hardware capability %#llx", cint(capHW1)) == -1 {
				return -1
			}
		} else if ef.printf(" hardware capability %#llx", cint(capHW1)) == -1 {
			return -1
		}
	}
	if capSF1 != 0 {
		if capSF1&sf1FPUsed != 0 {
			s := ", not known to use frame pointer"
			if capSF1&sf1FPKnown != 0 {
				s = ", uses frame pointer"
			}
			if ef.printf("%s", cstringS(s)) == -1 {
				return -1
			}
		}
		capSF1 &^= sf1Mask
		if capSF1 != 0 && ef.printf(", with unknown software capability %#llx", cint(capSF1)) == -1 {
			return -1
		}
	}
	return 0
}
