//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/extractors/tsk.rs of binwalk-ng. binwalk-ng
// extracts FAT, EXT and NTFS with tsk_recover of The Sleuth Kit, as
// "tsk_recover -i raw -a <image> rootfs", and recoverTSK writes the files
// that tsk_recover writes. It copies the behavior of tsk_recover and of the
// library of The Sleuth Kit, not their code. A comment names the C function
// that a Go function copies.

package disk

import (
	"strings"

	"github.com/xo/magic/walk"
)

// tskType is the type of a name or of its metadata, as TSK_FS_NAME_TYPE
// and TSK_FS_META_TYPE of The Sleuth Kit.
type tskType uint8

// The types that tsk_recover tells apart.
const (
	tskUndef tskType = iota // the name does not give the type
	tskReg
	tskDir
	tskOther
)

// tskName is a name in a folder, as TSK_FS_NAME.
type tskName struct {
	name  string
	typ   tskType
	alloc bool   // the name is allocated
	addr  uint64 // the address of the metadata
}

// tskMeta is the metadata of a file, as TSK_FS_META, and how to read its
// content.
type tskMeta struct {
	typ  tskType
	size int64

	// read gives the content of the file to emit, in order, as
	// tsk_fs_file_walk with no flags. It stops at the first error of the
	// file system, and the part before the error stays written. It returns
	// false only when emit returns false.
	read func(emit func([]byte) bool) bool
}

// tskFS is a file system that tsk_recover reads.
type tskFS interface {
	// root is the address of the root folder.
	root() uint64
	// dir gives the names of the folder at addr, as dir_open_meta, or
	// false when The Sleuth Kit cannot open it.
	dir(addr uint64) ([]tskName, bool)
	// meta gives the metadata at addr, as file_add_meta, or nil when the
	// look up fails.
	meta(addr uint64) *tskMeta
}

// The limits of tsk_fs_dir_walk_recursive of fs_dir.cpp.
const (
	tskMaxDepth = 128  // MAX_DEPTH
	tskDirSize  = 4096 // DIR_STRSZ, the room for the path of a folder
)

// tskMaxNames is the most names that one walk reads. The Sleuth Kit has no
// such limit, so a hostile image whose folders name each other many times
// can make it walk for ever. The walker stops there, and fails.
const tskMaxNames = 1 << 24

// The limits of the system, which make mkdir and fopen fail.
const (
	tskPathMax = 4096 // PATH_MAX, with the NUL
	tskNameMax = 255  // NAME_MAX
)

// tskRoot is the output folder that binwalk gives tsk_recover.
const tskRoot = "rootfs"

// recoverTSK runs tsk_recover with -a on fs, and writes what it writes to
// out. It is false when tsk_recover exits with an error, which happens when
// the root folder cannot be read, or when out takes no more.
func recoverTSK(fs tskFS, out *walk.Output) bool {
	names, ok := fs.dir(fs.root())
	if !ok {
		return false
	}
	r := &tskRecovery{fs: fs, nodes: map[string]*tskNode{}, seen: map[uint64]bool{}}
	r.walkDir(fs.root(), names, "", 0)
	return r.names <= tskMaxNames && r.write(out)
}

// tskNode is a path that tsk_recover makes: a folder, or a file with the
// metadata of the last file that it wrote there.
type tskNode struct {
	path string
	dir  bool
	src  *tskMeta
}

// tskRecovery is the state of one run of tsk_recover. It records the
// folders and files in the order that tsk_recover makes them, and the
// content of a file comes from the last file that fopen truncated there.
type tskRecovery struct {
	fs    tskFS
	nodes map[string]*tskNode
	order []*tskNode
	seen  map[uint64]bool // the stack of folders, stack_seen
	names int             // the names that the walk read
}

// walkDir is tsk_fs_dir_walk_recursive with the flags ALLOC and RECURSE,
// and the callback processFile of tsk_recover. dirs is the path of the
// folder, with a slash after each name. It is false when the walk of the
// folder stops with an error.
func (r *tskRecovery) walkDir(addr uint64, names []tskName, dirs string, depth int) bool {
	if addr == r.fs.root() {
		names = tskPrioritize(names)
	}
	for _, n := range names {
		if r.names++; r.names > tskMaxNames {
			return false
		}
		// The callback gets only allocated names, and a folder with an
		// unallocated name holds no allocated name, so the walk skips it.
		if !n.alloc {
			continue
		}
		m := r.fs.meta(n.addr)
		r.processFile(n, m, dirs)
		if n.typ != tskDir && n.typ != tskUndef || m == nil || m.typ != tskDir || n.name == "." || n.name == ".." {
			continue
		}
		if r.seen[n.addr] {
			continue
		}
		r.seen[n.addr] = true
		// tsk_fs_dir_walk_recursive returns here without a pop, so the
		// folder stays on the stack.
		if depth >= tskMaxDepth || len(dirs)+len(n.name) >= tskDirSize {
			return false
		}
		if sub, ok := r.fs.dir(n.addr); ok {
			r.walkDir(n.addr, sub, dirs+n.name+"/", depth+1)
		}
		delete(r.seen, n.addr)
	}
	return true
}

// tskPrioritize is prioritizeDirNames of fs_dir.cpp: the order of the
// names of the root folder, with the folders of users first.
func tskPrioritize(names []tskName) []tskName {
	score := func(name string) int {
		has := func(p string) bool {
			return len(name) >= len(p) && strings.EqualFold(name[:len(p)], p)
		}
		switch {
		case has("Users"), has("Documents and Settings"), has("home"), has("ProgramData"):
			return 0
		case has("Windows"), has("$Orphan"):
			return 2
		case has("pagefile"), has("hiberfil"):
			return 3
		}
		return 1
	}
	out := make([]tskName, 0, len(names))
	for s := range 4 {
		for _, n := range names {
			if score(n.name) == s {
				out = append(out, n)
			}
		}
	}
	return out
}

// processFile is processFile of tsk_recover. It skips "." and "..",
// folders, the virtual files of FAT, which the readers do not give, and
// files of no size.
func (r *tskRecovery) processFile(n tskName, m *tskMeta, dirs string) {
	if n.typ == tskDir || n.typ == tskUndef && m != nil && m.typ == tskDir {
		return
	}
	if m == nil || m.size == 0 {
		return
	}
	r.writeFile(n.name, m, dirs)
}

// writeFile is writeFile of tsk_recover, on a system like Linux. It makes
// the folders of the path, then opens the file with "w+", which truncates
// a file that is there.
func (r *tskRecovery) writeFile(name string, m *tskMeta, dirs string) {
	fbuf := []byte(tskRoot + "//" + dirs)
	if len(fbuf) >= tskPathMax {
		return
	}
	// Clean up control characters, and a "/../" with a byte after it.
	for i := range fbuf {
		if fbuf[i] < 0x20 {
			fbuf[i] = '^'
		}
		if i+4 < len(fbuf) && string(fbuf[i:i+4]) == "/../" {
			fbuf[i+1], fbuf[i+2] = '^', '^'
		}
	}
	if _, ok := r.lstat(string(fbuf)); !ok {
		for i := range fbuf {
			if i > 0 && fbuf[i] == '/' && fbuf[i-1] != '/' || fbuf[i] != '/' && i == len(fbuf)-1 {
				p := string(fbuf[:i+1])
				if fbuf[i] == '/' {
					p = string(fbuf[:i])
				}
				if _, ok := r.lstat(p); !ok && !r.mkdir(p) {
					return
				}
			}
		}
	}
	if fbuf[len(fbuf)-1] != '/' {
		fbuf = append(fbuf, '/')
	}
	nb := []byte(name)
	for i, c := range nb {
		if c < 0x20 || c == '/' {
			nb[i] = '^'
		}
	}
	r.create(string(append(fbuf, nb...)), m)
}

// resolve is the path p as the system resolves it, from the folder of
// tsk_recover, and whether a part is too long. A ".." at the top stays at
// the top, where the output of the walker stops it too.
func resolve(p string) (string, bool) {
	var parts []string
	for part := range strings.SplitSeq(p, "/") {
		if len(part) > tskNameMax {
			return "", false
		}
		switch part {
		case "", ".":
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "/"), true
}

// lstat finds the node at p. The folder of tsk_recover, "", is always
// there.
func (r *tskRecovery) lstat(p string) (*tskNode, bool) {
	q, ok := resolve(p)
	if !ok {
		return nil, false
	}
	if q == "" {
		return &tskNode{dir: true}, true
	}
	n, ok := r.nodes[q]
	return n, ok
}

// parentDir reports whether the folder of the path q is a folder.
func (r *tskRecovery) parentDir(q string) bool {
	i := strings.LastIndexByte(q, '/')
	if i < 0 {
		return true
	}
	n, ok := r.nodes[q[:i]]
	return ok && n.dir
}

// mkdir makes the folder p, as mkdir(2). It fails when p is too long, or
// when its parent is not a folder.
func (r *tskRecovery) mkdir(p string) bool {
	q, ok := resolve(p)
	if !ok || len(p) >= tskPathMax || q == "" || !r.parentDir(q) {
		return false
	}
	if _, ok := r.nodes[q]; ok {
		return false
	}
	n := &tskNode{path: q, dir: true}
	r.nodes[q] = n
	r.order = append(r.order, n)
	return true
}

// create opens the file p with "w+", and records that the content of m is
// its content. It fails on a folder, and when the parent is not a folder.
func (r *tskRecovery) create(p string, m *tskMeta) {
	q, ok := resolve(p)
	if !ok || len(p) >= tskPathMax || q == "" || strings.HasSuffix(p, "/") || !r.parentDir(q) {
		return
	}
	if n, ok := r.nodes[q]; ok {
		if !n.dir {
			n.src = m
		}
		return
	}
	n := &tskNode{path: q, src: m}
	r.nodes[q] = n
	r.order = append(r.order, n)
}

// tskChunk is the most bytes that write gives AppendFile at once.
const tskChunk = 1 << 20

// write writes the folders and the files of the run to out, in the order
// that tsk_recover makes them. It is false when out takes no more.
func (r *tskRecovery) write(out *walk.Output) bool {
	for _, n := range r.order {
		if n.dir {
			if !out.CreateDirectory(n.path) {
				return false
			}
			continue
		}
		if !out.CreateFile(n.path, nil) {
			return false
		}
		var buf []byte
		flush := func() bool {
			ok := len(buf) == 0 || out.AppendFile(n.path, buf)
			buf = buf[:0]
			return ok
		}
		if !n.src.read(func(b []byte) bool {
			buf = append(buf, b...)
			return len(buf) < tskChunk || flush()
		}) || !flush() {
			return false
		}
	}
	return true
}
