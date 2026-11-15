//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Package filesystem registers the file systems that binwalk finds and
// extracts with its own code: RomFS.
package filesystem

import "github.com/xo/magic/walk"

func init() {
	walk.Register(RomFS)
}
