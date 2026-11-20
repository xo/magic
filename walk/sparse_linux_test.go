//go:build linux

package walk

import (
	"os"
	"syscall"
)

// allocated returns the bytes that the file system gives to the file of fi.
func allocated(fi os.FileInfo) (int64, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Blocks * 512, true
}
