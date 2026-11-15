//go:build unix

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
// This file translates src/fsmagic.c of file 5.48 to Go (D29).

package engine

import (
	"fmt"
	"io/fs"
	"runtime"
	"syscall"
)

// pathIsLinux is whether the checks of libmagic for Linux apply.
const pathIsLinux = runtime.GOOS == "linux"

// errnoText holds the texts of glibc that differ from those of Go.
var errnoText = map[syscall.Errno]string{
	syscall.ENOENT:       "No such file or directory",
	syscall.EACCES:       "Permission denied",
	syscall.ENOTDIR:      "Not a directory",
	syscall.EISDIR:       "Is a directory",
	syscall.ELOOP:        "Too many levels of symbolic links",
	syscall.ENAMETOOLONG: "File name too long",
	syscall.EIO:          "Input/output error",
}

// rdev returns the major and minor numbers of a device, as fsmagic prints
// them.
func rdev(fi fs.FileInfo) string {
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	maj, min := devNumbers(uint64(sys.Rdev))
	return fmt.Sprintf(" (%d/%d)", maj, min)
}

// devNumbers splits a device number as major and minor of the C library.
func devNumbers(d uint64) (int64, int64) {
	switch runtime.GOOS {
	case "linux":
		return int64((d>>8)&0xfff | (d>>32)&^0xfff), int64(d&0xff | (d>>12)&^0xff)
	case "darwin":
		return int64(d >> 24 & 0xff), int64(d & 0xffffff)
	case "freebsd":
		return int64((d>>32)&0xffffff00 | (d>>8)&0xff), int64((d>>24)&0xff00 | d&0xffff00ff)
	}
	return int64(d >> 8 & 0xff), int64(d & 0xff)
}
