//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package walk

import (
	"fmt"
	"os"
	"syscall"
)

// mapFile maps f into memory, and returns a function that unmaps it. A file
// that cannot be mapped, such as a pipe, is read into memory, up to limit
// bytes.
func mapFile(f *os.File, limit int64) ([]byte, func(), error) {
	fi, err := f.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", f.Name(), err)
	}
	if !fi.Mode().IsRegular() {
		return readFile(f, limit)
	}
	if fi.Size() == 0 {
		return nil, func() {}, nil
	}
	if int64(int(fi.Size())) != fi.Size() {
		return nil, nil, fmt.Errorf("%s is too large to map", f.Name())
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, int(fi.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return readFile(f, limit)
	}
	return data, func() { _ = syscall.Munmap(data) }, nil
}
