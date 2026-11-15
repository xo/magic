//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates the extractor of src/signatures/ext.rs of binwalk,
// src/extractors/tsk.rs. binwalk extracts EXT with tsk_recover of The
// Sleuth Kit, and extractEXT writes the files that tsk_recover writes. The
// reader copies the behavior of ext2fs.cpp, ext2fs_dent.cpp and
// unix_misc.cpp of The Sleuth Kit, not their code.

package disk

import (
	"cmp"
	"encoding/binary"
	"slices"

	"github.com/xo/magic/walk"
)

// extractEXT is the extractor of "ext": tsk_recover on the image that the
// match carves.
func extractEXT(data []byte, off int, out *walk.Output) walk.Extraction {
	const magicOffset = 1080
	m, err := parseEXT(data, off+magicOffset)
	if err != nil || m.Offset != off || m.Size < 0 || m.Size > len(data)-off {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	fs, ok := openExt(data[off : off+m.Size])
	if !ok || !recoverTSK(fs, out) {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true}
}

// The features of the superblock that the reader uses.
const (
	extIncompatFiletype = 0x0002
	extIncompatExtents  = 0x0040
	extIncompat64Bit    = 0x0080
	extROCompatLarge    = 0x0002
)

// The flags of an inode that the reader uses.
const (
	extFlagExtents = 0x00080000
	extFlagInline  = 0x10000000
)

// The sizes of the reader, from tsk_ext2fs.h.
const (
	extEAInodeOffset = 160 // EXT2_EA_INODE_OFFSET
	extEAMagic       = 0xEA020000
	extInlineMax     = 60   // EXT2_INLINE_MAX_DATA_LEN
	extMaxPath       = 4096 // EXT2FS_MAXPATHLEN
	extMaxName       = 255  // EXT2FS_MAXNAMLEN
	extMaxDirNames   = 1000000
	extMaxInitLen    = 0x8000 // EXT2_MAX_INIT_EXTENT_LENGTH
)

// extFS is an EXT file system, as EXT2FS_INFO.
type extFS struct {
	img       []byte
	bs        uint64 // the size of a block
	lastBlock uint64
	inodes    uint64 // s_inodes_count
	ipg       uint64 // inodes per group
	inodeSize uint64
	groups    uint64
	gdOff     uint64 // the offset of the group descriptors
	gdSize    uint64
	gd64      bool // the descriptors have the high halves of ext4
	v2        bool // a folder entry has a type
	largeFile bool
}

// openExt is ext2fs_open, with the type detected.
func openExt(img []byte) (*extFS, bool) {
	if len(img) < 2048 {
		return nil, false
	}
	le := binary.LittleEndian
	sb := img[1024:2048]
	if le.Uint16(sb[56:]) != 0xEF53 {
		return nil, false
	}
	incompat := le.Uint32(sb[96:])
	fs := &extFS{
		img:       img,
		inodes:    uint64(le.Uint32(sb[0:])),
		ipg:       uint64(le.Uint32(sb[40:])),
		inodeSize: uint64(le.Uint16(sb[88:])),
		v2:        incompat&extIncompatFiletype != 0,
		largeFile: le.Uint32(sb[100:])&extROCompatLarge != 0,
	}
	if fs.inodes+1 < 10 {
		return nil, false
	}
	logBlock, logFrag := le.Uint32(sb[24:]), le.Uint32(sb[28:])
	if logBlock != logFrag || logBlock >= 32 {
		return nil, false
	}
	// The size of a block is an unsigned int in The Sleuth Kit.
	fs.bs = uint64(uint32(1024) << logBlock)
	if fs.bs == 0 || fs.bs%512 != 0 {
		return nil, false
	}
	blocks := uint64(le.Uint32(sb[4:]))
	if incompat&extIncompat64Bit != 0 {
		blocks |= uint64(le.Uint32(sb[0x150:])) << 32
	}
	fs.lastBlock = blocks - 1
	if n := uint64(len(img)) / fs.bs; n < blocks {
		fs.lastBlock = n - 1
	}
	bpg := uint64(le.Uint32(sb[32:]))
	if bpg == 0 || fs.ipg == 0 {
		return nil, false
	}
	first := uint64(le.Uint32(sb[20:]))
	fs.groups = uint64(uint32((blocks - first + bpg - 1) / bpg))
	fs.gdOff = (2048 + fs.bs - 1) / fs.bs * fs.bs
	descSize := uint64(le.Uint16(sb[254:]))
	fs.gd64 = incompat&extIncompatExtents != 0 && incompat&extIncompat64Bit != 0 && descSize >= 64
	fs.gdSize = max(descSize, 32)
	return fs, true
}

// read reads n bytes at off of the image, as tsk_fs_read.
func (fs *extFS) read(off, n uint64) ([]byte, bool) {
	end := off + n
	if end < off || end > (fs.lastBlock+1)*fs.bs || end > uint64(len(fs.img)) {
		return nil, false
	}
	return fs.img[off:end], true
}

// block reads the block addr, as tsk_fs_read_block.
func (fs *extFS) block(addr uint64) ([]byte, bool) {
	if addr > fs.lastBlock {
		return nil, false
	}
	return fs.read(addr*fs.bs, fs.bs)
}

// group is ext2fs_group_load: the inode bitmap and the inode table of the
// group g.
func (fs *extFS) group(g uint64) (ibitmap, itable uint64, ok bool) {
	if g >= fs.groups {
		return 0, 0, false
	}
	gd, ok := fs.read(fs.gdOff+g*fs.gdSize, fs.gdSize)
	if !ok {
		return 0, 0, false
	}
	le := binary.LittleEndian
	bbitmap := uint64(le.Uint32(gd[0:]))
	ibitmap = uint64(le.Uint32(gd[4:]))
	itable = uint64(le.Uint32(gd[8:]))
	if fs.gd64 {
		bbitmap |= uint64(le.Uint32(gd[0x20:])) << 32
		ibitmap |= uint64(le.Uint32(gd[0x24:])) << 32
		itable |= uint64(le.Uint32(gd[0x28:])) << 32
	}
	if bbitmap > fs.lastBlock || ibitmap > fs.lastBlock || itable > fs.lastBlock {
		return 0, 0, false
	}
	return ibitmap, itable, true
}

// extInode is an inode as ext2fs_dinode_copy reads it.
type extInode struct {
	raw     []byte // the inode, at least 160 bytes
	ea      []byte // the extended attributes in the inode, or nil
	typ     tskType
	mode    uint16
	size    uint64
	flags   uint32
	alloc   bool // the bit of the inode bitmap
	full    bool // the look up did not fail
	inline  []byte
	hasData bool // the inline data was loaded
}

// inode is ext2fs_inode_lookup. It is nil when ext2fs_dinode_load fails.
// When a later step fails, the inode has its type and size, but full is
// false, as the metadata that The Sleuth Kit keeps.
func (fs *extFS) inode(inum uint64) *extInode {
	if inum < 1 || inum > fs.inodes {
		return nil
	}
	g := (inum - 1) / fs.ipg
	ibitmap, itable, ok := fs.group(g)
	if !ok {
		return nil
	}
	rel := (inum - 1) - fs.ipg*g
	if itable >= (1<<63-1)/fs.bs {
		return nil
	}
	b, ok := fs.read(itable*fs.bs+rel*fs.inodeSize, fs.inodeSize)
	if !ok {
		return nil
	}
	in := &extInode{raw: make([]byte, max(fs.inodeSize, extEAInodeOffset))}
	copy(in.raw, b)
	if fs.inodeSize > extEAInodeOffset {
		in.ea = in.raw[extEAInodeOffset:fs.inodeSize]
	}
	le := binary.LittleEndian
	in.mode = le.Uint16(in.raw[0:])
	switch in.mode & 0xF000 {
	case 0x8000:
		in.typ = tskReg
	case 0x4000:
		in.typ = tskDir
	case 0:
		in.typ = tskUndef
	default:
		in.typ = tskOther
	}
	in.size = uint64(le.Uint32(in.raw[4:]))
	if in.typ == tskReg && fs.largeFile {
		in.size += uint64(le.Uint32(in.raw[108:])) << 32
	}
	in.flags = le.Uint32(in.raw[32:])
	switch {
	case in.flags&extFlagExtents != 0:
	case in.flags&extFlagInline != 0:
		in.loadInline()
	case in.mode&0xF000 == 0xA000 && in.size < extMaxPath && in.size < 4*15:
		// A fast link: The Sleuth Kit copies the target, then clears the
		// pointers, so its content reads as zeros.
		clear(in.raw[40:100])
	case in.mode&0xF000 == 0xA000 && in.size < extMaxPath:
		// A slow link: The Sleuth Kit reads the target from the direct
		// blocks, and the look up fails when it cannot.
		for i := uint64(0); i < 12 && i*fs.bs < in.size; i++ {
			if _, ok := fs.block(uint64(int64(int32(le.Uint32(in.raw[40+4*i:]))))); !ok {
				return in
			}
		}
	}
	ibase := g*fs.ipg + 1
	bm, ok := fs.block(ibitmap)
	if !ok || inum < ibase || inum-ibase >= fs.bs*8 {
		return in
	}
	in.alloc = bm[(inum-ibase)/8]&(1<<((inum-ibase)%8)) != 0
	in.full = true
	return in
}

// loadInline is ext4_load_attrs_inline: the data in the block pointers,
// then the rest from the attribute "system.data".
func (in *extInode) loadInline() {
	ea := in.ea
	var more []byte
	le := binary.LittleEndian
	const entrySize = 17 // sizeof(ext2fs_ea_entry), with one byte of name
	if len(ea) > 4+entrySize && le.Uint32(ea) == extEAMagic {
		i := 4
		for ea[i] != 0 || ea[i+1] != 0 {
			nlen := int(ea[i])
			if ea[i+1] == 7 && nlen == 4 && i+entrySize+4 < len(ea) && string(ea[i+16:i+20]) == "data" {
				off := int(le.Uint16(ea[i+2:]))
				size := int(le.Uint32(ea[i+8:]))
				if off < len(ea)-4 && size <= len(ea)-4-off {
					more = ea[4+off : 4+off+size]
					break
				}
			}
			i += entrySize + nlen - 1
			if i+entrySize+4 > len(ea) {
				break
			}
		}
	}
	if in.size == 0 || in.size > 1024-156 {
		return
	}
	d := make([]byte, in.size)
	n := copy(d, in.raw[40:40+min(in.size, extInlineMax)])
	if in.size > extInlineMax && len(more) > 0 {
		copy(d[n:], more)
	}
	in.inline = d
	in.hasData = true
}

// extRun is a run of blocks of a file, as TSK_FS_ATTR_RUN.
type extRun struct {
	off, addr, n uint64
	sparse       bool
}

// runs is the load of the attribute of the data, as ext2fs_load_attrs:
// a function that gives the block of each logical block, in order. It is
// false when the load fails, and The Sleuth Kit then reads nothing.
func (fs *extFS) runs(in *extInode) (func(yield func(addr uint64, sparse bool) bool), bool) {
	if in.flags&extFlagExtents != 0 {
		return fs.extentRuns(in)
	}
	return fs.blockMap(in)
}

// extentRuns is ext4_load_attrs_extents.
func (fs *extFS) extentRuns(in *extInode) (func(yield func(uint64, bool) bool), bool) {
	le := binary.LittleEndian
	h := in.raw[40:100]
	if le.Uint16(h) != 0xF30A {
		return nil, false
	}
	var runs []extRun
	entries := le.Uint16(h[2:])
	if entries != 0 {
		nodes := 0
		if entries > (60-12)/12 || !fs.extentNode(h, &runs, 0, &nodes) {
			return nil, false
		}
	}
	slices.SortStableFunc(runs, func(a, b extRun) int { return cmp.Compare(a.off, b.off) })
	nblocks := (in.size + fs.bs - 1) / fs.bs
	end := uint64(0)
	if len(runs) > 0 {
		r := runs[len(runs)-1]
		end = r.off + r.n
	}
	if end < nblocks {
		runs = append(runs, extRun{off: end, n: nblocks - end, sparse: true})
	}
	return func(yield func(uint64, bool) bool) {
		// tsk_fs_attr_add_run keeps the runs in order of their logical
		// block, and fills a gap with zeros.
		next := uint64(0)
		for _, r := range runs {
			if r.off < next {
				continue
			}
			for ; next < r.off; next++ {
				if !yield(0, true) {
					return
				}
			}
			for i := range r.n {
				if !yield(r.addr+i, r.sparse) {
					return
				}
			}
			next = r.off + r.n
		}
	}, true
}

// extentNode adds the extents of the node h, as
// ext2fs_make_data_run_extent_index, with the limit of depth of
// ext2fs_extent_tree_index_count. The node in the inode holds 4 entries.
//
// A tree of extents has no more nodes than the image has blocks. The Sleuth
// Kit has no such limit, so a hostile tree whose nodes name one block many
// times can make it read for ever. The walker fails the load there.
func (fs *extFS) extentNode(h []byte, runs *[]extRun, depth int, nodes *int) bool {
	le := binary.LittleEndian
	if *nodes++; uint64(*nodes) > fs.lastBlock+1 || depth > 32 || le.Uint16(h) != 0xF30A {
		return false
	}
	entries := int(le.Uint16(h[2:]))
	if entries > (len(h)-12)/12 {
		return false
	}
	for i := range entries {
		e := h[12+12*i:]
		if le.Uint16(h[6:]) == 0 {
			// ext2fs_make_data_run_extent. The Sleuth Kit shifts the high
			// half of the start by 16, not 32.
			r := extRun{off: uint64(le.Uint32(e[0:]))}
			if n := le.Uint16(e[4:]); n <= extMaxInitLen {
				r.n = uint64(n)
				r.addr = uint64(le.Uint16(e[6:]))<<16 | uint64(le.Uint32(e[8:]))
			} else {
				r.n = uint64(n - extMaxInitLen)
				r.sparse = true
			}
			*runs = append(*runs, r)
			continue
		}
		child, ok := fs.block(uint64(le.Uint16(e[8:]))<<16 | uint64(le.Uint32(e[4:])))
		if !ok || !fs.extentNode(child, runs, depth+1, nodes) {
			return false
		}
	}
	return true
}

// blockMap is tsk_fs_unix_make_data_run: 12 direct blocks, then a single,
// a double and a triple indirect block. A block 0 is sparse, and so is the
// tree under an indirect block 0.
func (fs *extFS) blockMap(in *extInode) (func(yield func(uint64, bool) bool), bool) {
	le := binary.LittleEndian
	ptr := func(i int) uint64 {
		// The pointers of the inode are signed in The Sleuth Kit.
		return uint64(int64(int32(le.Uint32(in.raw[40+4*i:]))))
	}
	per := fs.bs / 4
	need := (in.size + fs.bs - 1) / fs.bs
	// The checks of the load: an indirect block that the load reads must
	// be in the image. At a double or triple level, the load reads every
	// pointer of the block, past the size of the file too.
	var check func(addr uint64, level int) bool
	check = func(addr uint64, level int) bool {
		if addr > fs.lastBlock {
			return false
		}
		if addr == 0 {
			return true
		}
		b, ok := fs.block(addr)
		if !ok {
			return false
		}
		if level == 1 {
			return true
		}
		for i := range per {
			if !check(uint64(le.Uint32(b[4*i:])), level-1) {
				return false
			}
		}
		return true
	}
	covered := uint64(12)
	for level := 1; level <= 3 && need > covered; level++ {
		if !check(ptr(11+level), level) {
			return nil, false
		}
		n := per
		for range level - 1 {
			n *= per
		}
		covered += n
	}
	var walkLevel func(addr uint64, level int, yield func(uint64, bool) bool, left *uint64) bool
	walkLevel = func(addr uint64, level int, yield func(uint64, bool) bool, left *uint64) bool {
		var b []byte
		if addr != 0 {
			b, _ = fs.block(addr)
		}
		for i := uint64(0); i < per && *left > 0; i++ {
			p := uint64(0)
			if b != nil {
				p = uint64(le.Uint32(b[4*i:]))
			}
			if level == 1 {
				*left--
				if !yield(p, p == 0) {
					return false
				}
				continue
			}
			if !walkLevel(p, level-1, yield, left) {
				return false
			}
		}
		return true
	}
	return func(yield func(uint64, bool) bool) {
		left := need
		for i := 0; i < 12 && left > 0; i++ {
			left--
			if !yield(ptr(i), ptr(i) == 0) {
				return
			}
		}
		for level := 1; level <= 3 && left > 0; level++ {
			if !walkLevel(ptr(11+level), level, yield, &left) {
				return
			}
		}
	}, true
}

// readData is tsk_fs_file_walk with no flags: the content of the file, a
// block at a time, up to its size. It stops at a block past the file
// system or that cannot be read.
func (fs *extFS) readData(in *extInode, emit func([]byte) bool) bool {
	if in.flags&extFlagExtents == 0 && in.flags&extFlagInline != 0 {
		if !in.hasData {
			return true
		}
		return emit(in.inline)
	}
	blocks, ok := fs.runs(in)
	if !ok {
		return true
	}
	zero := make([]byte, fs.bs)
	left := in.size
	stopped := false
	blocks(func(addr uint64, sparse bool) bool {
		if left == 0 {
			return false
		}
		if addr > fs.lastBlock {
			return false
		}
		b := zero
		if !sparse {
			var ok bool
			if b, ok = fs.block(addr); !ok {
				return false
			}
		}
		n := min(left, fs.bs)
		left -= n
		if !emit(b[:n]) {
			stopped = true
			return false
		}
		return true
	})
	return !stopped
}

// root is EXT2FS_ROOTINO.
func (fs *extFS) root() uint64 { return 2 }

// orphans is the address of the virtual folder $OrphanFiles, which holds
// only unallocated names.
func (fs *extFS) orphans() uint64 { return fs.inodes + 1 }

// meta is the look up of tskFS.
func (fs *extFS) meta(addr uint64) *tskMeta {
	if addr == fs.orphans() {
		return &tskMeta{typ: tskDir}
	}
	in := fs.inode(addr)
	if in == nil {
		return nil
	}
	return &tskMeta{
		typ:  in.typ,
		size: int64(in.size),
		read: func(emit func([]byte) bool) bool { return fs.readData(in, emit) },
	}
}

// dir is ext2fs_dir_open_meta.
func (fs *extFS) dir(addr uint64) ([]tskName, bool) {
	if addr == fs.orphans() {
		return nil, true
	}
	if addr < 1 || addr > fs.orphans() {
		return nil, false
	}
	in := fs.inode(addr)
	if in == nil || !in.full {
		return nil, false
	}
	d := &extDir{fs: fs, del: !in.alloc, seen: map[extKey]int{}}
	if in.flags&extFlagExtents == 0 && in.flags&extFlagInline != 0 {
		if in.size == 0 {
			return nil, true
		}
		if !in.hasData {
			return nil, false
		}
		if !d.parse(in.inline) {
			return nil, false
		}
		return d.names, true
	}
	if in.size == 0 {
		return nil, false
	}
	blocks, ok := fs.runs(in)
	if !ok {
		return nil, false
	}
	// tsk_fs_file_read reads each block of the rounded size, and a read
	// past the size of the folder is short, which fails the folder.
	total := (in.size + fs.bs - 1) / fs.bs * fs.bs
	if total != in.size {
		return nil, false
	}
	left := total
	good := true
	blocks(func(a uint64, sparse bool) bool {
		if left == 0 {
			return false
		}
		left -= fs.bs
		if a > fs.lastBlock {
			good = false
			return false
		}
		if sparse {
			// A block of zeros holds no entry.
			return true
		}
		b, ok := fs.block(a)
		if !ok || !d.parse(b) {
			good = false
			return false
		}
		return true
	})
	if !good || left != 0 {
		return nil, false
	}
	return d.names, true
}

// extKey is the key of tsk_fs_dir_add, which drops a second name with the
// same address.
type extKey struct {
	addr uint64
	name string
}

// extDir is the folder that ext2fs_dir_open_meta fills.
type extDir struct {
	fs    *extFS
	del   bool // the inode of the folder is unallocated
	names []tskName
	seen  map[extKey]int
}

// add is tsk_fs_dir_add: an allocated name replaces an unallocated one with
// the same address and name, and another one is dropped.
func (d *extDir) add(n tskName) bool {
	k := extKey{n.addr, n.name}
	if i, ok := d.seen[k]; ok {
		if !d.names[i].alloc && n.alloc {
			d.names[i] = n
		}
		return true
	}
	if len(d.names) >= extMaxDirNames {
		return false
	}
	d.seen[k] = len(d.names)
	d.names = append(d.names, n)
	return true
}

// parse is ext2fs_dent_parse_block. It reads each entry by its real length,
// not its recorded length, so that it finds the deleted entries in the
// room that an entry does not use.
func (d *extDir) parse(buf []byte) bool {
	le := binary.LittleEndian
	dirsiz := func(n int) int { return (n + 8 + 3) &^ 3 }
	dellen := 0
	var minreclen int
	for idx := 0; idx <= len(buf)-dirsiz(1); idx += minreclen {
		e := buf[idx:]
		inode := uint64(le.Uint32(e[0:]))
		reclen := int(le.Uint16(e[4:]))
		namelen := int(le.Uint16(e[6:]))
		if d.fs.v2 {
			namelen = int(e[6])
		}
		minreclen = dirsiz(namelen)
		if inode > d.fs.orphans() || namelen > extMaxName || namelen == 0 ||
			reclen < minreclen || reclen%4 != 0 || idx+reclen > len(buf) {
			minreclen = 4
			if dellen > 0 {
				dellen -= 4
			}
			continue
		}
		if dellen != 0 && dellen < minreclen {
			minreclen = 4
			dellen -= 4
			continue
		}
		n := tskName{addr: inode, typ: tskUndef}
		name := e[8 : 8+namelen]
		// strncpy stops at a NUL.
		for i, c := range name {
			if c == 0 {
				name = name[:i]
				break
			}
		}
		n.name = string(name)
		if d.fs.v2 {
			switch e[7] {
			case 1:
				n.typ = tskReg
			case 2:
				n.typ = tskDir
			case 3, 4, 5, 6, 7:
				n.typ = tskOther
			}
		}
		if dellen > 0 || inode == 0 || d.del {
			if dellen > 0 {
				dellen -= minreclen
			}
		} else {
			n.alloc = true
		}
		if !d.add(n) {
			return false
		}
		if dellen <= 0 {
			if reclen-minreclen >= dirsiz(1) {
				dellen = reclen - minreclen
			} else {
				minreclen = reclen
			}
		}
	}
	return true
}
