//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates the extractor of src/signatures/fat.rs of binwalk,
// src/extractors/tsk.rs. binwalk extracts FAT with tsk_recover of The
// Sleuth Kit, and extractFAT writes the files that tsk_recover writes. The
// reader copies the behavior of fatfs.cpp, fatxxfs.cpp, fatxxfs_dent.c,
// fatxxfs_meta.cpp and fatfs_meta.cpp of The Sleuth Kit, not their code.

package disk

import (
	"encoding/binary"
	"unicode/utf8"

	"github.com/xo/magic/walk"
)

// extractFAT is the extractor of "fat": tsk_recover on the image that the
// match carves.
func extractFAT(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := parseFAT(data, off+fatMagicOffset)
	if err != nil || m.Offset != off || m.Size < 0 || m.Size > len(data)-off {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	fs, ok := openFAT(data[off : off+m.Size])
	if !ok || !recoverTSK(fs, out) {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true}
}

// The attributes of a folder entry.
const (
	fatAttrVolume = 0x08
	fatAttrDir    = 0x10
	fatAttrLFN    = 0x0F
	fatAttrAll    = 0x3F
)

// fatNameMax is FATFS_MAXNAMLEN_UTF8, the room for a long name.
const fatNameMax = 1024

// fatFS is a FAT12, FAT16 or FAT32 file system, as FATFS_INFO.
type fatFS struct {
	img            []byte
	ssize          uint64 // the size of a sector
	csize          uint64 // the sectors of a cluster
	numroot        uint64
	firstfatsect   uint64
	firstdatasect  uint64
	firstclustsect uint64
	clustcnt       uint64
	lastclust      uint64
	bits           int // 12, 16 or 32
	mask           uint64
	rootsect       uint64
	lastBlock      uint64
	android        bool // TSK_FATFS_SUBTYPE_ANDROID_1
}

// openFAT is fatfs_open and fatxxfs_open, with the type detected. The
// boot sector is at the start, where binwalk found its magic.
func openFAT(img []byte) (*fatFS, bool) {
	if len(img) < 512 {
		return nil, false
	}
	le := binary.LittleEndian
	b := img[:512]
	fs := &fatFS{img: img, ssize: uint64(le.Uint16(b[11:])), csize: uint64(b[13])}
	switch fs.ssize {
	case 512, 1024, 2048, 4096:
	default:
		return nil, false
	}
	switch fs.csize {
	case 1, 2, 4, 8, 16, 32, 64, 128:
	default:
		return nil, false
	}
	numfat := uint64(b[16])
	if numfat == 0 || numfat > 8 {
		return nil, false
	}
	fs.numroot = uint64(le.Uint16(b[17:]))
	sectors := uint64(le.Uint16(b[19:]))
	if sectors == 0 {
		sectors = uint64(le.Uint32(b[32:]))
	}
	perfat := uint64(le.Uint16(b[22:]))
	if perfat == 0 {
		perfat = uint64(le.Uint32(b[36:]))
	}
	if perfat == 0 {
		return nil, false
	}
	fs.firstfatsect = uint64(le.Uint16(b[14:]))
	if fs.firstfatsect == 0 || fs.firstfatsect > sectors {
		return nil, false
	}
	fs.firstdatasect = fs.firstfatsect + perfat*numfat
	fs.firstclustsect = fs.firstdatasect + (fs.numroot*32+fs.ssize-1)/fs.ssize
	// The Sleuth Kit subtracts without a check, and the count wraps.
	fs.clustcnt = (sectors - fs.firstclustsect) / fs.csize
	fs.lastclust = 1 + fs.clustcnt
	switch {
	case fs.clustcnt < 4085:
		fs.bits, fs.mask = 12, 0xFFF
	case fs.clustcnt < 65525:
		fs.bits, fs.mask = 16, 0xFFFF
	default:
		fs.bits, fs.mask = 32, 0x0FFFFFFF
	}
	if fs.bits == 32 && fs.numroot != 0 || fs.bits != 32 && fs.numroot == 0 {
		return nil, false
	}
	fs.rootsect = fs.firstdatasect
	if fs.bits == 32 {
		fs.rootsect = fs.clustSect(uint64(le.Uint32(b[44:])))
	}
	fs.lastBlock = sectors - 1
	if n := uint64(len(img)) / fs.ssize; n < sectors {
		fs.lastBlock = n - 1
	}
	// fatxxfs_open opens the root folder to choose the subtype. A root
	// with no more names than the four virtual ones of a FAT with two
	// tables is read again as the FAT of Android, which marks a deleted
	// entry with two NULs.
	if names, ok := fs.dir(fs.root()); ok && len(names)+int(numfat)+2 <= 4 {
		fs.android = true
		if again, ok := fs.dir(fs.root()); !ok || len(again) <= len(names) {
			fs.android = false
		}
	}
	return fs, true
}

// clustSect is FATFS_CLUST_2_SECT.
func (fs *fatFS) clustSect(c uint64) uint64 {
	return fs.firstclustsect + ((c&fs.mask)-2)*fs.csize
}

// sectClust is FATFS_SECT_2_CLUST.
func (fs *fatFS) sectClust(s uint64) uint64 {
	return 2 + (s-fs.firstclustsect)/fs.csize
}

// isEOF is FATFS_ISEOF.
func (fs *fatFS) isEOF(v uint64) bool {
	return v >= 0x0FFFFFF8&fs.mask && v <= 0x0FFFFFFF
}

// sector reads the sector s.
func (fs *fatFS) sector(s uint64) ([]byte, bool) {
	if s > fs.lastBlock || (s+1)*fs.ssize > uint64(len(fs.img)) {
		return nil, false
	}
	return fs.img[s*fs.ssize : (s+1)*fs.ssize], true
}

// next is fatfs_getFAT: the entry of the table for the cluster c. The
// Sleuth Kit reads the table in pieces of 4096 bytes, so an entry fails
// when the piece from its sector passes the end of the image. An entry
// past the clusters reads as 0.
func (fs *fatFS) next(c uint64) (uint64, bool) {
	if c > fs.lastclust {
		// The Sleuth Kit ignores the cluster after the last when the
		// clusters do not end at the last sector, and leaves the value as
		// it was.
		if c == fs.lastclust+1 && fs.firstclustsect+fs.csize*fs.clustcnt-1 != fs.lastBlock {
			return 0, true
		}
		return 0, false
	}
	var off uint64
	switch fs.bits {
	case 12:
		if c&0xF000 != 0 {
			return 0, false
		}
		off = c + c>>1
	case 16:
		off = c << 1
	default:
		off = c << 2
	}
	sect := fs.firstfatsect + off/fs.ssize
	if sect*fs.ssize+4096 > uint64(len(fs.img)) {
		return 0, false
	}
	p := fs.firstfatsect*fs.ssize + off
	if p+uint64(max(fs.bits/8, 2)) > uint64(len(fs.img)) {
		return 0, false
	}
	le := binary.LittleEndian
	var v uint64
	switch fs.bits {
	case 12:
		v = uint64(le.Uint16(fs.img[p:]))
		if c&1 != 0 {
			v >>= 4
		}
		v &= 0xFFF
	case 16:
		v = uint64(le.Uint16(fs.img[p:]))
	default:
		v = uint64(le.Uint32(fs.img[p:])) & 0x0FFFFFFF
	}
	if v > fs.lastclust && v < 0x0FFFFFF7&fs.mask {
		v = 0
	}
	return v, true
}

// sectAlloc is fatfs_is_sectalloc: 1 when the sector is allocated, 0 when
// not, and -1 on an error.
func (fs *fatFS) sectAlloc(s uint64) int {
	if s < fs.firstclustsect {
		return 1
	}
	if s <= fs.lastBlock && s >= fs.firstclustsect+fs.csize*fs.clustcnt {
		return 0
	}
	v, ok := fs.next(fs.sectClust(s))
	switch {
	case !ok:
		return -1
	case v == 0:
		return 0
	}
	return 1
}

// fatRef is the metadata of a file or a folder: its first cluster, its
// size, and whether it is the root folder of FAT12 or FAT16.
type fatRef struct {
	clust uint64
	size  uint64
	root  bool
}

// sectors is fatfs_make_data_runs for an allocated file: the sectors of the
// file, from its chain of clusters, up to its size rounded to a cluster.
// It is false when the load fails. A loop in the chain gives no sector.
func (fs *fatFS) sectors(r fatRef) ([]uint64, bool) {
	if r.root {
		var out []uint64
		for s := fs.rootsect; s < fs.rootsect+fs.firstclustsect-fs.firstdatasect; s++ {
			out = append(out, s)
		}
		return out, true
	}
	c := r.clust
	if c > fs.lastclust && !fs.isEOF(c) {
		return nil, false
	}
	cbytes := fs.csize * fs.ssize
	remain := int64((r.size + cbytes - 1) / cbytes * cbytes)
	var out []uint64
	seen := map[uint64]bool{}
	for c&fs.mask > 0 && remain > 0 && !fs.isEOF(c) {
		base := fs.clustSect(c)
		if base+fs.csize-1 > fs.lastBlock {
			return nil, false
		}
		for i := range fs.csize {
			out = append(out, base+i)
		}
		remain -= int64(cbytes)
		if remain > 0 {
			n, ok := fs.next(c)
			if !ok {
				return nil, false
			}
			c = n
			if seen[c] {
				return nil, true
			}
			seen[c] = true
		}
	}
	return out, true
}

// dirSize is the size that fatxxfs_dinode_copy gives an allocated folder:
// the clusters of its chain.
func (fs *fatFS) dirSize(c uint64) uint64 {
	seen := map[uint64]bool{}
	n := uint64(0)
	for c != 0 && !fs.isEOF(c) {
		if seen[c] {
			break
		}
		seen[c] = true
		n++
		next, ok := fs.next(c)
		if !ok {
			break
		}
		c = next
	}
	return n * fs.csize * fs.ssize
}

// read is tsk_fs_file_walk with no flags: the sectors of the file up to
// its size. It stops at a sector that cannot be read.
func (fs *fatFS) read(r fatRef, emit func([]byte) bool) bool {
	secs, ok := fs.sectors(r)
	if !ok {
		return true
	}
	left := r.size
	for _, s := range secs {
		if left == 0 {
			break
		}
		b, ok := fs.sector(s)
		if !ok {
			return true
		}
		n := min(left, fs.ssize)
		left -= n
		if !emit(b[:n]) {
			return false
		}
	}
	return true
}

// root is FATFS_ROOTINO.
func (fs *fatFS) root() uint64 { return 2 }

// addr is the address of the entry at the sector s and the index i, as
// FATFS_SECT_2_INODE.
func (fs *fatFS) addr(s, i uint64) uint64 {
	return (s-fs.firstdatasect)*(fs.ssize/32) + 3 + i
}

// entry reads the entry at the address a.
func (fs *fatFS) entry(a uint64) ([]byte, uint64, bool) {
	per := fs.ssize / 32
	s := (a-3)/per + fs.firstdatasect
	b, ok := fs.sector(s)
	if !ok {
		return nil, 0, false
	}
	i := (a - 3) % per
	return b[32*i : 32*i+32], s, true
}

// ref is the metadata of the root folder, as fatfs_make_root, or of the
// entry at a, as fatxxfs_inode_lookup and fatxxfs_dinode_copy.
func (fs *fatFS) ref(a uint64) (fatRef, tskType, bool) {
	if a == fs.root() {
		if fs.bits == 32 {
			c := fs.sectClust(fs.rootsect)
			return fatRef{clust: c, size: fs.dirSize(c)}, tskDir, true
		}
		return fatRef{root: true, size: (fs.firstclustsect - fs.firstdatasect) * fs.ssize}, tskDir, true
	}
	if a < 3 {
		return fatRef{}, 0, false
	}
	e, s, ok := fs.entry(a)
	if !ok {
		return fatRef{}, 0, false
	}
	alloc := fs.sectAlloc(s)
	if alloc == -1 || !fs.isEntry(e, alloc == 1) {
		return fatRef{}, 0, false
	}
	le := binary.LittleEndian
	if e[11]&fatAttrLFN == fatAttrLFN {
		return fatRef{}, tskReg, true
	}
	c := uint64(le.Uint16(e[26:])) | uint64(le.Uint16(e[20:]))<<16
	r := fatRef{clust: c & fs.mask, size: uint64(le.Uint32(e[28:]))}
	if e[11]&fatAttrDir == 0 {
		return r, tskReg, true
	}
	if alloc == 1 && !fs.deleted(e) {
		r.size = fs.dirSize(c)
	} else if v, ok := fs.next(c); ok && v != 0 {
		r.size = 0
	} else {
		r.size = fs.csize * fs.ssize
	}
	return r, tskDir, true
}

// meta is the look up of tskFS.
func (fs *fatFS) meta(a uint64) *tskMeta {
	r, typ, ok := fs.ref(a)
	if !ok {
		return nil
	}
	return &tskMeta{
		typ:  typ,
		size: int64(r.size),
		read: func(emit func([]byte) bool) bool { return fs.read(r, emit) },
	}
}

// deleted is FATXXFS_IS_DELETED.
func (fs *fatFS) deleted(e []byte) bool {
	if fs.android {
		return e[0] == 0 && e[1] == 0
	}
	return e[0] == 0xE5
}

// fatName83 is FATXXFS_IS_83_NAME.
func fatName83(c byte) bool {
	return !(c < 0x20 || c == 0x22 || c >= 0x2A && c <= 0x2C || c == 0x2E || c == 0x2F ||
		c >= 0x3A && c <= 0x3F || c >= 0x5B && c <= 0x5D || c == 0x7C)
}

// is83 is is_83_name.
func is83(e []byte) bool {
	if e[0] != 0x05 && e[0] != '.' && !fatName83(e[0]) || e[0] == 0x20 {
		return false
	}
	if e[1] == '.' {
		if e[0] != '.' {
			return false
		}
	} else if !fatName83(e[1]) {
		return false
	}
	for _, c := range e[2:11] {
		if !fatName83(c) {
			return false
		}
	}
	if e[11]&fatAttrVolume != fatAttrVolume {
		for i := 1; i < 7; i++ {
			if e[i] == 0x20 && e[i+1] != 0x20 {
				return false
			}
		}
		if e[9] == 0x20 && e[10] != 0x20 {
			return false
		}
	}
	return true
}

// fatTime is FATFS_ISTIME.
func fatTime(x uint16) bool {
	return x&0x1F <= 30 && x>>5&0x3F <= 59 && x>>11 <= 23
}

// fatDate is FATFS_ISDATE.
func fatDate(x uint16) bool {
	d, m := x&0x1F, x>>5&0xF
	return d >= 1 && d <= 31 && m >= 1 && m <= 12
}

// isEntry is fatxxfs_is_dentry. basic limits the tests to the essential
// data.
func (fs *fatFS) isEntry(e []byte, basic bool) bool {
	if e[11]&fatAttrLFN == fatAttrLFN {
		return e[0] <= 0x4F || e[0] == 0xE5
	}
	le := binary.LittleEndian
	ctime, cdate, adate := le.Uint16(e[14:]), le.Uint16(e[16:]), le.Uint16(e[18:])
	wtime, wdate := le.Uint16(e[22:]), le.Uint16(e[24:])
	if !basic {
		switch {
		case e[12]&^0x18 != 0, e[11]&^fatAttrAll != 0:
			return false
		case e[11]&fatAttrVolume != 0 && e[11]&(fatAttrDir|0x01|0x20) != 0:
			return false
		case ctime != 0 && !fatTime(ctime), wtime != 0 && !fatTime(wtime),
			cdate != 0 && !fatDate(cdate), e[13] > 200,
			adate != 0 && !fatDate(adate), wdate != 0 && !fatDate(wdate):
			return false
		}
	}
	c := uint64(le.Uint16(e[26:])) | uint64(le.Uint16(e[20:]))<<16
	size := uint64(le.Uint32(e[28:]))
	switch {
	case c > fs.lastclust && !fs.isEOF(c):
		return false
	case size > fs.clustcnt*fs.csize*fs.ssize:
		return false
	case size > 0 && c == 0:
		return false
	case !fs.android && !is83(e):
		return false
	case ctime == 0 && wtime == 0 && cdate == 0 && adate == 0 && wdate == 0 && c == 0 && size == 0:
		return false
	}
	return true
}

// dir is fatfs_dir_open_meta: the folder is read whole, with its slack,
// then parsed.
func (fs *fatFS) dir(a uint64) ([]tskName, bool) {
	r, typ, ok := fs.ref(a)
	if !ok || typ != tskDir {
		return nil, false
	}
	if r.size == 0 {
		return nil, true
	}
	secs, ok := fs.sectors(r)
	if !ok || uint64(len(secs))*fs.ssize < r.size {
		return nil, false
	}
	for _, s := range secs {
		if _, ok := fs.sector(s); !ok {
			return nil, false
		}
	}
	return fs.parse(secs), true
}

// parse is fatxxfs_dent_parse_buf.
func (fs *fatFS) parse(secs []uint64) []tskName {
	var names []tskName
	var lfn [fatNameMax]byte
	start := fatNameMax - 1
	var chk byte
	seen, invalid := 0, 0
	corrupt := false
	for _, s := range secs {
		alloc := fs.sectAlloc(s)
		if alloc == -1 {
			continue
		}
		b, _ := fs.sector(s)
		for i := range fs.ssize / 32 {
			e := b[32*i : 32*i+32]
			seen++
			if !fs.isEntry(e, !corrupt && alloc == 1) {
				invalid++
				if seen == 4 && invalid == 4 {
					corrupt = true
				}
				continue
			}
			attr := e[11]
			var n tskName
			switch {
			case attr&fatAttrLFN == fatAttrLFN:
				if e[0]&0x40 != 0 && e[0] != 0xE5 || e[13] != chk {
					chk = e[13]
					start = fatNameMax - 1
				}
				for _, part := range [][]byte{e[28:32], e[14:26], e[1:11]} {
					for j := len(part) - 1; j >= 0; j-- {
						if start > 0 {
							lfn[start] = part[j]
							start--
						}
					}
				}
				continue
			case attr&fatAttrVolume == fatAttrVolume:
				var name []byte
				for _, c := range e[:11] {
					if c >= 0x20 && c != 0xFF {
						name = append(name, c)
					} else {
						name = append(name, '^')
					}
				}
				n.name = string(name) + " (Volume Label Entry)"
			default:
				short := fs.shortName(e)
				if start != fatNameMax-1 {
					long, ok := fatUTF16(lfn[start+1:])
					if !ok {
						continue
					}
					start = fatNameMax - 1
					n.name = long
				} else {
					n.name = short
				}
			}
			n.typ = tskReg
			if attr&fatAttrDir != 0 {
				n.typ = tskDir
			}
			n.addr = fs.addr(s, i)
			n.alloc = alloc == 1 && !fs.deleted(e)
			names = append(names, n)
		}
	}
	return names
}

// shortName is the 8.3 name of an entry, as fatxxfs_dent_parse_buf makes
// it: spaces, NULs and 0xFF are skipped, the case flags apply, and bytes
// past ASCII become "^".
func (fs *fatFS) shortName(e []byte) string {
	var name []byte
	for j, c := range e[:8] {
		if c == 0 || c == 0xFF || c == 0x20 {
			continue
		}
		switch {
		case j == 0 && c == 0xE5:
			c = '_'
		case e[12]&0x08 != 0 && c >= 'A' && c <= 'Z':
			c += 32
		}
		name = append(name, c)
	}
	for j, c := range e[8:11] {
		if c == 0 || c == 0xFF || c == 0x20 {
			continue
		}
		if j == 0 {
			name = append(name, '.')
		}
		if e[12]&0x10 != 0 && c >= 'A' && c <= 'Z' {
			c += 32
		}
		name = append(name, c)
	}
	for j, c := range name {
		if c > 0x7E {
			name[j] = '^'
		}
	}
	return string(name)
}

// fatUTF16 is tsk_UTF16toUTF8 with lenient conversion, on the bytes of a
// long name, up to the first NUL. An unpaired surrogate becomes "^", and a
// high surrogate that takes the next unit drops it. It is false when the
// name ends with a high surrogate, or does not fit in fatNameMax bytes.
func fatUTF16(b []byte) (string, bool) {
	b = b[:len(b)&^1]
	var out []byte
	for i := 0; i < len(b); i += 2 {
		r := rune(binary.LittleEndian.Uint16(b[i:]))
		switch {
		case r >= 0xD800 && r <= 0xDBFF:
			if i+2 >= len(b) {
				return "", false
			}
			i += 2
			if r2 := rune(binary.LittleEndian.Uint16(b[i:])); r2 >= 0xDC00 && r2 <= 0xDFFF {
				r = (r-0xD800)<<10 + (r2 - 0xDC00) + 0x10000
			} else {
				r = '^'
			}
		case r >= 0xDC00 && r <= 0xDFFF:
			r = '^'
		}
		out = utf8.AppendRune(out, r)
		if len(out) > fatNameMax {
			return "", false
		}
	}
	for i, c := range out {
		if c == 0 {
			out = out[:i]
			break
		}
	}
	return string(out), true
}
