//go:build !linux

package walk

import "os"

// allocated reports false: the test reads the blocks of a file on Linux
// only.
func allocated(os.FileInfo) (int64, bool) {
	return 0, false
}
