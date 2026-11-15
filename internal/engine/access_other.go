//go:build !unix

package engine

import "os"

// writable is access(2) with W_OK, as far as the mode tells it.
func writable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().Perm()&0o200 != 0
}

// executable is false, because libmagic does not ask X_OK on Windows.
func executable(string) bool { return false }
