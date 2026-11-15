//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package walk

import "os"

// mapFile reads f into memory, up to limit bytes. This system has no
// syscall.Mmap.
func mapFile(f *os.File, limit int64) ([]byte, func(), error) {
	return readFile(f, limit)
}
