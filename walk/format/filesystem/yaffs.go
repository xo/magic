//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/yaffs.rs and src/structures/yaffs.rs
// of binwalk. binwalk extracts YAFFS2 with unyaffs, from
// src/extractors/yaffs2.rs, and extractYAFFS writes the files that unyaffs
// 0.9.7 writes.

package filesystem

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// YAFFS is the signature "yaffs" of binwalk.
var YAFFS = &walk.Signature{
	Name: "yaffs",
	// The first object is a folder or a file in the root folder, in either
	// byte order, with the unused checksum of its name.
	Magic: [][]byte{
		[]byte("\x03\x00\x00\x00\x01\x00\x00\x00\xFF\xFF"),
		[]byte("\x00\x00\x00\x03\x00\x00\x00\x01\xFF\xFF"),
		[]byte("\x01\x00\x00\x00\x01\x00\x00\x00\xFF\xFF"),
		[]byte("\x00\x00\x00\x01\x00\x00\x00\x01\xFF\xFF"),
	},
	Description: "YAFFSv2 filesystem",
	Parse:       parseYAFFS,
	Extract:     extractYAFFS,
}

// yaffsMinObjects is MIN_NUMBER_OF_OBJS.
const yaffsMinObjects = 2

// parseYAFFS is yaffs_parser. It finds the page size from the spare data
// after the first page, the spare size from the object header that it puts
// at the third chunk, and the size of the image from its object headers.
func parseYAFFS(data []byte, off int) (walk.Match, error) {
	// The largest page and the largest spare.
	const maxObjSize = 16896
	if !binwalk.OffsetSafe(len(data), off+maxObjSize*yaffsMinObjects, -1) {
		return walk.Match{}, walk.ErrNoMatch
	}
	img := data[off:]
	big, endian := img[0] == 0, "little"
	if big {
		endian = "big"
	}
	page, ok := yaffsPageSize(img)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	spare, ok := yaffsSpareSize(img, page, big)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	size, ok := yaffsImageSize(img, page, spare, big)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.Medium,
		Description: fmt.Sprintf("YAFFSv2 filesystem, %s endian, page size: %d, spare size: %d, image size: %d bytes", endian, page, spare, size),
	}, nil
}

// yaffsPageSize is get_page_size: the first page size that the start of
// the spare data of YAFFS2 follows, with or without the two bytes of the
// mark of a bad block.
func yaffsPageSize(img []byte) (int, bool) {
	magics := []string{"\x00\x00\x10\x00", "\x00\x10\x00\x00", "\xFF\xFF\x00\x00\x10\x00", "\xFF\xFF\x00\x10\x00\x00"}
	for _, page := range []int{512, 1024, 2048, 4096, 8192, 16384} {
		for _, m := range magics {
			if b, ok := binwalk.Range(img, page, page+len(m)); ok && string(b) == m {
				return page, true
			}
		}
	}
	return 0, false
}

// yaffsSpareSize is get_spare_size: the first spare size that puts an
// object header at the third chunk.
func yaffsSpareSize(img []byte, page int, big bool) (int, bool) {
	for _, spare := range []int{16, 32, 64, 128, 256, 512} {
		if b, ok := binwalk.From(img, (page+spare)*yaffsMinObjects); ok {
			if _, ok := yaffsObjHeader(b, big); ok {
				return spare, true
			}
		}
	}
	return 0, false
}

// yaffsImageSize is get_image_size. It walks the object headers, and puts
// the next one after the chunks of the data of a file. It needs more than
// two chunks, all inside the data.
func yaffsImageSize(img []byte, page, spare int, big bool) (int, bool) {
	const fileType = 1
	size, next, prev := 0, 0, -1
	block := page + spare
	for binwalk.OffsetSafe(len(img), next, prev) {
		obj := img[next:]
		typ, ok := yaffsObjHeader(obj, big)
		if !ok {
			// Data after the image.
			break
		}
		blocks := 1
		if typ == fileType {
			n, ok := yaffsFileBlocks(obj, page, big)
			if !ok {
				return 0, false
			}
			blocks += n
		}
		prev = next
		size += blocks * block
		next = size
	}
	if size > block*yaffsMinObjects && size <= len(img) {
		return size, true
	}
	return 0, false
}

// yaffsFileBlocks is get_file_block_count: the pages of the data of a file,
// from its size in the object header.
func yaffsFileBlocks(obj []byte, page int, big bool) (int, bool) {
	const infoStart = 268
	b, ok := binwalk.From(obj, infoStart)
	if !ok {
		return 0, false
	}
	f, ok := yaffsFileInfo.Parse(b, big)
	if !ok {
		return 0, false
	}
	return (f["file_size"] + page - 1) / page, true
}

var (
	yaffsObject = binwalk.Struct{
		{Name: "type", Type: "u32"},
		{Name: "parent_id", Type: "u32"},
		{Name: "name_checksum", Type: "u16"},
	}
	yaffsFileInfo = binwalk.Struct{
		{Name: "mode", Type: "u32"},
		{Name: "uid", Type: "u32"},
		{Name: "gid", Type: "u32"},
		{Name: "atime", Type: "u32"},
		{Name: "mtime", Type: "u32"},
		{Name: "ctime", Type: "u32"},
		{Name: "file_size", Type: "u32"},
	}
)

// yaffsObjHeader is parse_yaffs_obj_header: a type from 0 to 5, a parent,
// and the unused checksum of the name, 0xFFFF.
func yaffsObjHeader(b []byte, big bool) (int, bool) {
	f, ok := yaffsObject.Parse(b, big)
	if !ok || f["type"] > 5 || f["parent_id"] <= 0 || f["name_checksum"] != 0xFFFF {
		return 0, false
	}
	return f["type"], true
}

// The object types of YAFFS, and the first object, the root folder.
const (
	yaffsUnknown = iota
	yaffsFile
	yaffsSymlink
	yaffsDirectory
	yaffsHardlink
	yaffsSpecial

	yaffsRoot = 1
)

// The sizes of unyaffs: the chunks and spares that it tries, and the
// buffer of chunk and spare that it copies data from.
const (
	yaffsMinChunk = 2048
	yaffsMaxChunk = 16384
	yaffsMinSpare = 64
	yaffsMaxSpare = 512
	yaffsMaxWarn  = 20
)

// errYAFFS is an error that ends unyaffs with exit code 1. binwalk accepts
// only exit code 0 from unyaffs, so the extraction fails.
var errYAFFS = errors.New("yaffs: unyaffs fails")

// extractYAFFS writes what `unyaffs IMAGE yaffs-root` writes from the
// bytes of the match. unyaffs reads the image in the byte order of the
// system, little endian on the systems that binwalk runs on, so a big
// endian image fails, as in binwalk. It reads only chunks of 2 KiB to
// 16 KiB, so an image of smaller pages fails too.
//
// unyaffs runs as root in the image of binwalk, so it makes devices.
// The walker writes a device as a file that names it, as for the other
// formats.
func extractYAFFS(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := parseYAFFS(data, off)
	if err != nil {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	u, ok := newUnyaffs(data[off : off+m.Size])
	// unyaffs stops with an error at a chunk that the image cuts short,
	// after it wrote the chunks before it. The extraction fails all the
	// same.
	if !ok || len(u.img)%(u.chunk+u.spare) != 0 || !out.CreateDirectory("yaffs-root") {
		return walk.Extraction{}
	}
	u.out = out.Sub("yaffs-root")
	if u.run() != nil {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true}
}

// unyaffs is the state of unyaffs: the layout of the flash, the objects by
// their identifier, and what it made at each path.
type unyaffs struct {
	img   []byte
	chunk int
	spare int
	off   int // 2 when the spare starts with the mark of a bad block

	out   *walk.Output
	next  int // the index of the next chunk
	warns int
	objs  map[uint32]*yaffsObj
	made  map[string]*yaffsMade
	saved struct {
		id   uint32
		data []byte
	}
}

// yaffsObj is the object of unyaffs: its type and path.
type yaffsObj struct {
	typ  uint32
	path string
}

// yaffsMade is what unyaffs made at a path, to copy it for a hard link.
type yaffsMade struct {
	typ          uint32 // yaffsFile, yaffsSymlink, yaffsDirectory or yaffsSpecial
	data         []byte
	exec         bool
	target       string
	mode         uint32 // the type of a special file
	major, minor int
}

// yaffsTags is the part of yaffs_PackedTags2 that unyaffs reads.
type yaffsTags struct {
	seq, obj, chunkID, count uint32
}

// newUnyaffs is detect_flash_layout. It reads the first four chunks of each
// layout from a buffer that 0xFF fills past the image, and takes the first
// layout whose four chunks look like YAFFS2.
func newUnyaffs(img []byte) (*unyaffs, bool) {
	buf := make([]byte, 4*(yaffsMaxChunk+yaffsMaxSpare))
	n := copy(buf, img)
	for i := n; i < len(buf); i++ {
		buf[i] = 0xFF
	}
	le := binary.LittleEndian
	check := func(chunk, spare, off int) bool {
		for i := range 4 {
			o := i * (chunk + spare)
			typ := le.Uint32(buf[o:])
			t := tagsAt(buf[o+chunk+off:])
			isObj := t.obj == yaffsRoot || (t.obj >= 0x100 && t.obj <= 0x100+10)
			data := t.chunkID > 0 && t.chunkID <= 10 && t.obj >= 0x100 && t.obj <= 0x100+10 && t.count <= uint32(chunk)
			header := t.chunkID == 0 && isObj && t.count == 0xFFFF && typ >= yaffsFile && typ <= yaffsSpecial
			if !data && !header {
				return false
			}
		}
		return true
	}
	for chunk := yaffsMinChunk; chunk <= yaffsMaxChunk; chunk *= 2 {
		for spare := yaffsMinSpare; spare <= yaffsMaxSpare; spare += 16 {
			for off := 0; off <= 2; off += 2 {
				if check(chunk, spare, off) {
					return &unyaffs{
						img: img, chunk: chunk, spare: spare, off: off,
						objs: map[uint32]*yaffsObj{yaffsRoot: {typ: yaffsDirectory, path: "."}},
						made: map[string]*yaffsMade{},
					}, true
				}
			}
		}
	}
	return nil, false
}

// tagsAt reads the tags at the start of b.
func tagsAt(b []byte) yaffsTags {
	le := binary.LittleEndian
	return yaffsTags{seq: le.Uint32(b), obj: le.Uint32(b[4:]), chunkID: le.Uint32(b[8:]), count: le.Uint32(b[12:])}
}

// readChunk is read_chunk: the next chunk and its spare, with the spare
// moved over the mark of a bad block. It reports false at the end.
func (u *unyaffs) readChunk() ([]byte, bool) {
	size := u.chunk + u.spare
	if u.next*size >= len(u.img) {
		return nil, false
	}
	c := u.img[u.next*size : (u.next+1)*size]
	u.next++
	if u.off == 0 {
		return c, true
	}
	b := make([]byte, size)
	copy(b, c[:u.chunk])
	copy(b[u.chunk:], c[u.chunk+u.off:])
	b[size-2], b[size-1] = 0xFF, 0xFF
	return b, true
}

// tags returns the tags of the chunk c.
func (u *unyaffs) tags(c []byte) yaffsTags {
	return tagsAt(c[u.chunk:])
}

// warn is a warning of unyaffs. It gives up at the twentieth.
func (u *unyaffs) warn() error {
	u.warns++
	if u.warns >= yaffsMaxWarn {
		return errYAFFS
	}
	return nil
}

// run is the main loop of unyaffs over process_chunk.
func (u *unyaffs) run() error {
	for {
		c, ok := u.readChunk()
		if !ok {
			return nil
		}
		if err := u.process(c); err != nil {
			return err
		}
	}
}

// process is process_chunk. A chunk of an erased page is empty. The first
// data chunk of a file can come before its header, so unyaffs keeps a chunk
// 1 until the header comes.
func (u *unyaffs) process(c []byte) error {
	t := u.tags(c)
	if t.seq == 0xFFFFFFFF {
		return nil
	}
	if u.saved.id != 0 && u.saved.id != t.obj {
		if err := u.warn(); err != nil {
			return err
		}
		u.saved.id = 0
	}
	switch {
	case t.chunkID == 1:
		u.saved.id, u.saved.data = t.obj, c
		return nil
	case t.chunkID != 0:
		return u.warn()
	}
	le := binary.LittleEndian
	typ := le.Uint32(c)
	obj, err := u.add(c, t)
	if err != nil {
		return err
	}
	mode := le.Uint32(c[268:])
	switch typ {
	case yaffsFile:
		return u.file(c, t, obj, mode)
	case yaffsSymlink:
		alias := cstring(c[300:])
		if u.exists(obj.path) || alias == "" {
			return errYAFFS
		}
		return u.make(obj.path, &yaffsMade{typ: yaffsSymlink, target: alias})
	case yaffsDirectory:
		if t.obj == yaffsRoot {
			return nil
		}
		// mkdirpath accepts a folder that exists.
		if m, ok := u.made[obj.path]; ok {
			if m.typ != yaffsDirectory {
				return errYAFFS
			}
			return nil
		}
		return u.make(obj.path, &yaffsMade{typ: yaffsDirectory})
	case yaffsHardlink:
		eq, ok := u.objs[le.Uint32(c[296:])]
		if !ok {
			return errYAFFS
		}
		// link fails on a folder, and on a path that does not exist or
		// that exists.
		m, ok := u.made[eq.path]
		if !ok || m.typ == yaffsDirectory || u.exists(obj.path) {
			return errYAFFS
		}
		return u.make(obj.path, m)
	case yaffsSpecial:
		return u.special(obj.path, mode, le.Uint32(c[460:]))
	}
	return nil
}

// add is add_object. The root folder is there from the start. Every other
// object needs a new identifier, a name that is a single part of a path,
// and a parent folder. Its path is the path of the parent and its name.
func (u *unyaffs) add(c []byte, t yaffsTags) (*yaffsObj, error) {
	le := binary.LittleEndian
	typ := le.Uint32(c)
	if t.obj == yaffsRoot {
		if typ != yaffsDirectory {
			return nil, errYAFFS
		}
		return u.objs[yaffsRoot], nil
	}
	name := cstring(c[10:])
	if typ > yaffsSpecial || name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
		return nil, errYAFFS
	}
	if _, ok := u.objs[t.obj]; ok {
		return nil, errYAFFS
	}
	parent, ok := u.objs[le.Uint32(c[4:])]
	if !ok || parent.typ != yaffsDirectory {
		return nil, errYAFFS
	}
	p := name
	if parent.path != "." {
		p = parent.path + "/" + name
	}
	obj := &yaffsObj{typ: typ, path: p}
	u.objs[t.obj] = obj
	return obj, nil
}

// file writes a file from the data chunks that follow its header, as
// next_data_chunk reads them: each has the identifier of the object, and
// the next chunk number. A chunk gives the bytes that its tags count, up
// to the size of the file.
//
// unyaffs copies those bytes from its buffer of one chunk and its spare,
// whose bytes after the spare are zero. A count larger than that buffer
// reads past it in unyaffs, and fails here. creat of unyaffs truncates a
// file that exists, and the walker cannot, so a second file of one path
// fails too.
func (u *unyaffs) file(c []byte, t yaffsTags, obj *yaffsObj, mode uint32) error {
	if u.exists(obj.path) {
		return errYAFFS
	}
	remain := int(int32(binary.LittleEndian.Uint32(c[292:])))
	var data []byte
	for remain > 0 {
		next := t.chunkID + 1
		if u.saved.id == t.obj && next == 1 {
			c = u.saved.data
			u.saved.id = 0
		} else {
			var ok bool
			c, ok = u.readChunk()
			if !ok {
				return errYAFFS
			}
			nt := u.tags(c)
			if nt.obj != t.obj || nt.chunkID != next {
				return errYAFFS
			}
		}
		t = u.tags(c)
		s := min(uint64(remain), uint64(t.count))
		if s > yaffsMaxChunk+yaffsMaxSpare {
			return errYAFFS
		}
		buf := c
		if int(s) > len(c) {
			buf = make([]byte, s)
			copy(buf, c)
		}
		data = append(data, buf[:s]...)
		remain -= int(s)
	}
	return u.make(obj.path, &yaffsMade{typ: yaffsFile, data: data, exec: mode&0o111 != 0})
}

// special is the mknod of a special object. mknod makes a device, a FIFO,
// a socket, or an empty file for a regular type. It refuses another type,
// which unyaffs takes as a warning, and fails on a path that exists.
func (u *unyaffs) special(p string, mode, rdev uint32) error {
	switch mode & sIFMT {
	case 0, sIFREG:
		if u.exists(p) {
			return errYAFFS
		}
		return u.make(p, &yaffsMade{typ: yaffsFile, exec: mode&0o111 != 0})
	case sIFCHR, sIFBLK, sIFIFO, sIFSOCK:
		if u.exists(p) {
			return errYAFFS
		}
		// The dev_t of glibc from 32 bits.
		major := int((rdev >> 8) & 0xFFF)
		minor := int((rdev & 0xFF) | ((rdev >> 12) & 0xFFF00))
		return u.make(p, &yaffsMade{typ: yaffsSpecial, mode: mode & sIFMT, major: major, minor: minor})
	}
	return nil
}

// exists reports whether unyaffs made something at p.
func (u *unyaffs) exists(p string) bool {
	_, ok := u.made[p]
	return ok
}

// make writes m at p.
func (u *unyaffs) make(p string, m *yaffsMade) error {
	var ok bool
	switch m.typ {
	case yaffsFile:
		ok = u.out.CreateFile(p, m.data)
		if ok && m.exec {
			ok = u.out.MakeExecutable(p)
		}
	case yaffsSymlink:
		ok = u.out.CreateSymlink(p, m.target)
	case yaffsDirectory:
		ok = u.out.CreateDirectory(p)
	case yaffsSpecial:
		switch m.mode {
		case sIFCHR:
			ok = u.out.CreateCharDevice(p, m.major, m.minor)
		case sIFBLK:
			ok = u.out.CreateBlockDevice(p, m.major, m.minor)
		case sIFIFO:
			ok = u.out.CreateFIFO(p)
		case sIFSOCK:
			ok = u.out.CreateSocket(p)
		}
	}
	if !ok {
		return errYAFFS
	}
	u.made[p] = m
	return nil
}

// cstring is a string of C: the bytes up to the first NUL.
func cstring(b []byte) string {
	return string(binwalk.CStringBytes(b))
}
