// Copyright (c) Ian F. Darwin 1986-1995.
// Software written by Ian F. Darwin and others;
// maintained 1995-present by Christos Zoulas and others.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice immediately at the beginning of the file, without modification,
//    this list of conditions, and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED. IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE FOR
// ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
// Copyright (c) Christos Zoulas 2003.
// All Rights Reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice immediately at the beginning of the file, without modification,
//    this list of conditions, and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED. IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE FOR
// ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
// This file translates src/fsmagic.c, src/magic.c of file to Go, at the commit
// that docs/upstream/upstreams.tsv names (D29, D48).

package engine

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

// This file is file_or_fd of magic.c with file_fsmagic of fsmagic.c: what
// libmagic says of a path before it reads the file, and how it reads it.

// File returns what libmagic says of the file at path with flags, as
// magic_file.
func (e *Engine) File(path string, flags Flags) (string, error) {
	return e.FileContext(context.Background(), path, Call{Flags: flags})
}

// FileContext is File with a context and a limit.
func (e *Engine) FileContext(ctx context.Context, path string, c Call) (string, error) {
	st := e.newState(ctx, c)
	switch st.fsmagic(path) {
	case -1:
		return st.result(-1)
	case 1:
		return st.result(1)
	}
	f, err := os.Open(path)
	if err != nil {
		if fi, serr := os.Stat(path); serr == nil {
			st.unreadable(path, fi)
		}
		return st.result(1)
	}
	// The file is only read, so an error of Close changes nothing.
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return st.result(1)
	}
	size := int64(st.bytesMax)
	if fi.Mode().IsRegular() {
		size = min(fi.Size(), size)
	}
	data := make([]byte, size)
	n, err := io.ReadFull(f, data)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		st.errorf("cannot read `%s' (%s)", path, strerror(err))
		return st.result(-1)
	}
	data = data[:n]
	info := &fileInfo{regular: fi.Mode().IsRegular(), size: fi.Size(), r: f}
	if fi.Size() <= int64(n) {
		info.tail = data
	} else {
		tl := min(fi.Size(), int64(st.bytesMax))
		info.tail = make([]byte, tl)
		if _, err := f.ReadAt(info.tail, fi.Size()-tl); err != nil {
			info.tail = nil
		}
	}
	return st.result(st.buffer(&buffer{data: data, file: info, mode: statMode(fi)}, n))
}

// unreadable is unreadable_info: a file that libmagic can stat and not
// open.
func (st *state) unreadable(path string, fi fs.FileInfo) {
	if writable(path) {
		st.printf("writable, ")
	}
	if executable(path) {
		st.printf("executable, ")
	}
	if fi.Mode().IsRegular() {
		st.printf("regular file, ")
	}
	st.printf("no read permission")
}

// statMode returns st_mode for fi.
func statMode(fi fs.FileInfo) uint32 {
	m := fi.Mode()
	v := uint32(m.Perm())
	if m&fs.ModeSetuid != 0 {
		v |= 0o4000
	}
	if m&fs.ModeSetgid != 0 {
		v |= 0o2000
	}
	if m&fs.ModeSticky != 0 {
		v |= 0o1000
	}
	switch {
	case m.IsRegular():
		v |= 0o100000
	case m.IsDir():
		v |= 0o040000
	case m&fs.ModeSymlink != 0:
		v |= 0o120000
	case m&fs.ModeNamedPipe != 0:
		v |= 0o010000
	case m&fs.ModeSocket != 0:
		v |= 0o140000
	case m&fs.ModeCharDevice != 0:
		v |= 0o020000
	case m&fs.ModeDevice != 0:
		v |= 0o060000
	}
	return v
}

// fsmagic is file_fsmagic. It returns 1 when the kind of the file is the
// answer, 0 when the file must be read, and -1 on an error.
func (st *state) fsmagic(fn string) int {
	mime := st.flags & Mime
	silent := st.flags&(Apple|Extension) != 0
	did := 0
	comma := func() string {
		did++
		if did > 1 {
			return ", "
		}
		return ""
	}
	var fi fs.FileInfo
	var err error
	if st.flags&Symlink == 0 {
		fi, err = os.Lstat(fn)
	} else {
		fi, err = os.Stat(fn)
	}
	if err != nil {
		if st.flags&Error != 0 {
			st.errorf("cannot stat `%s' (%s)", fn, strerror(err))
			return -1
		}
		st.printf("cannot open `%s' (%s)", fn, strerror(err))
		return 0
	}
	m := fi.Mode()
	ret := 1
	if mime == 0 && !silent {
		// pr_flags of fsmagic.c prints each name before its comma, so two
		// flags print as "setuidsetgid, ".
		for _, f := range []struct {
			mode fs.FileMode
			name string
		}{{fs.ModeSetuid, "setuid"}, {fs.ModeSetgid, "setgid"}, {fs.ModeSticky, "sticky"}} {
			if m&f.mode != 0 {
				st.printf("%s%s", f.name, comma())
			}
		}
	}
	say := func(sub, desc string) {
		switch {
		case mime != 0:
			st.mime(sub)
		case silent:
		default:
			st.printf("%s%s", comma(), desc)
		}
	}
	switch {
	case m.IsDir():
		say("directory", "directory")
	case m&fs.ModeCharDevice != 0:
		if st.flags&Devices != 0 {
			ret = 0
			break
		}
		say("chardevice", "character special"+rdev(fi))
	case m&fs.ModeDevice != 0:
		if st.flags&Devices != 0 {
			ret = 0
			break
		}
		say("blockdevice", "block special"+rdev(fi))
	case m&fs.ModeNamedPipe != 0:
		if st.flags&Devices != 0 {
			break
		}
		say("fifo", "fifo (named pipe)")
	case m&fs.ModeSymlink != 0:
		target, err := os.Readlink(fn)
		if err != nil || target == "" {
			if st.flags&Error != 0 {
				st.errorf("unreadable symlink `%s' (%s)", fn, strerror(err))
				return -1
			}
			say("symlink", "unreadable symlink `"+fn+"' ("+strerror(err)+")")
			break
		}
		if !linkTargetExists(fn, target) {
			return st.badLink(target)
		}
		if st.flags&Symlink != 0 {
			// libmagic follows the link with magic_file here, but it
			// never gets here, because it stats with stat for this flag.
			break
		}
		say("symlink", "symbolic link to "+target)
	case m&fs.ModeSocket != 0:
		say("socket", "socket")
	case m.IsRegular():
		if st.flags&Devices == 0 && fi.Size() == 0 {
			say("x-empty", "empty")
			break
		}
		ret = 0
	default:
		st.errorf("invalid mode 0%o", statMode(fi))
		return -1
	}
	if !silent && mime == 0 && did > 0 && ret == 0 {
		st.printf(" ")
	}
	if ret == 1 && silent {
		return 0
	}
	return ret
}

// linkTargetExists is the check of fsmagic for a broken link: on Linux
// libmagic stats the link itself, and elsewhere its target.
func linkTargetExists(fn, target string) bool {
	_, err := os.Stat(fn)
	if err == nil || pathIsLinux {
		return err == nil
	}
	// libmagic puts a relative target after the folder of the link, up to
	// its last '/'.
	if !strings.HasPrefix(target, "/") {
		if i := strings.LastIndexByte(fn, '/'); i >= 0 {
			target = fn[:i+1] + target
		}
	}
	_, err = os.Stat(target)
	return err == nil
}

// badLink is bad_link of fsmagic.c.
func (st *state) badLink(target string) int {
	mime := st.flags & Mime
	switch {
	case mime&MimeType != 0:
		st.printf("inode/symlink")
	case mime == 0:
		if st.flags&Error != 0 {
			st.errorf("broken symbolic link to %s", target)
			return -1
		}
		st.printf("broken symbolic link to %s", target)
	}
	return 1
}

// strerror is the text of strerror of glibc for the error of a system call.
func strerror(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if s, ok := errnoText[errno]; ok {
			return s
		}
		s := errno.Error()
		return strings.ToUpper(s[:1]) + s[1:]
	}
	if err == nil {
		return "Success"
	}
	return err.Error()
}

// mime prints the MIME type of a kind of file, as handle_mime.
func (st *state) mime(sub string) {
	if st.flags&MimeType != 0 {
		st.printf("inode/%s", sub)
		if st.flags&MimeEncoding != 0 {
			st.printf("; charset=")
		}
	}
	if st.flags&MimeEncoding != 0 {
		st.printf("binary")
	}
}
