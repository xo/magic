//go:build unix

package engine

import "syscall"

// writable and executable are access(2) with W_OK and X_OK, which
// unreadable_info of libmagic asks.
func writable(path string) bool   { return syscall.Access(path, 0x2) == nil }
func executable(path string) bool { return syscall.Access(path, 0x1) == nil }
