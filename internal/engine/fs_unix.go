//go:build unix

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
