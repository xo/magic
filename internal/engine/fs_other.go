//go:build !unix

package engine

import (
	"io/fs"
	"syscall"
)

const pathIsLinux = false

var errnoText = map[syscall.Errno]string{}

func rdev(fs.FileInfo) string { return "" }
