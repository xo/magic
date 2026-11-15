//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Package archive registers the archives that binwalk finds: ZIP, POSIX tar,
// CPIO, 7-Zip, ARJ, Microsoft Cabinet and RAR.
//
// binwalk extracts them with programs of the system: 7zz for ZIP, CPIO,
// 7-Zip and ARJ, tar for POSIX tar, cabextract for a cabinet, and unrar for
// RAR. The walker runs no program (D36), so each extractor of this package
// reads the archive in Go, and writes the files that those programs write.
// ARJ and RAR have no extractor: each file says why. binwalk finds Debian
// packages too, and does not extract them.
package archive

import (
	"io"
	"io/fs"
	"slices"
	"strings"

	"github.com/xo/magic/walk"
)

func init() {
	walk.Register(Tarball, CPIO, ZIP, SevenZip, ARJ, CAB, RAR)
	walk.Register(Deb)
}

// parent makes the folder of name, as tar and 7zz make the folders of a
// member that the archive does not hold.
func parent(out *walk.Output, name string) bool {
	if i := strings.LastIndex(name, "/"); i > 0 {
		return out.CreateDirectory(name[:i])
	}
	return true
}

// clean drops the parts "." and "" of a path, such as the "./" that starts
// each name of many archives, as tar and 7zz drop them. Output keeps a part
// ".", and cleans the parts "..".
func clean(name string) string {
	parts := strings.Split(name, "/")
	parts = slices.DeleteFunc(parts, func(p string) bool { return p == "." || p == "" })
	return strings.Join(parts, "/")
}

// member is one member of an archive, as the extractors write it.
type member struct {
	name  string
	mode  fs.FileMode // the type, and the permissions of the archive
	link  string      // the target of a symbolic link
	major int         // the numbers of a device
	minor int
	data  io.Reader // the content of a regular file, or nil when it is empty
}

// writer writes the members of an archive to an output. It counts the
// members that it wrote, and stops at the first write that fails for the
// limit of the output.
type writer struct {
	out     *walk.Output
	wrote   bool // at least one member was written
	stopped bool // a write reached the limit of the output
}

// write writes m. A member that cannot be written, such as one whose path
// exists, is skipped, as tar and 7zz report it and go on.
func (w *writer) write(m member) {
	m.name = clean(m.name)
	if m.name == "" {
		return
	}
	if !parent(w.out, m.name) {
		return
	}
	var ok bool
	switch {
	case m.mode.IsDir():
		ok = w.out.CreateDirectory(m.name)
	case m.mode&fs.ModeSymlink != 0:
		ok = w.out.CreateSymlink(m.name, m.link)
	case m.mode&fs.ModeCharDevice != 0:
		ok = w.out.CreateCharDevice(m.name, m.major, m.minor)
	case m.mode&fs.ModeDevice != 0:
		ok = w.out.CreateBlockDevice(m.name, m.major, m.minor)
	case m.mode&fs.ModeNamedPipe != 0:
		ok = w.out.CreateFIFO(m.name)
	case m.mode&fs.ModeSocket != 0:
		ok = w.out.CreateSocket(m.name)
	default:
		ok = w.copy(m.name, m.data)
	}
	if !ok {
		return
	}
	w.wrote = true
	if m.mode.IsRegular() && m.mode&0o111 != 0 {
		w.out.MakeExecutable(m.name)
	}
}

// copy writes the content of r to a new file in parts, so that the limit
// of the output stops a member that decompresses to too many bytes. When r
// fails, the file keeps the bytes before the failure, as 7zz keeps them.
func (w *writer) copy(name string, r io.Reader) bool {
	if !w.out.CreateFile(name, nil) {
		return false
	}
	if r == nil {
		return true
	}
	buf := make([]byte, 64<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 && !w.out.AppendFile(name, buf[:n]) {
			w.stopped = true
			return true
		}
		if err != nil {
			return true
		}
	}
}

// result is the result of the extraction: binwalk takes the exit code of
// the program, and then fails when the output holds nothing. The size is
// unknown, as for each program of binwalk.
func (w *writer) result() walk.Extraction {
	return walk.Extraction{OK: w.wrote && !w.stopped}
}
