//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Package disk registers the disk images and the file systems that
// binwalk-ng finds: MBR, EFI GPT, FAT, NTFS, EXT, BTRFS, APFS, LogFS, QCOW,
// DMG and Android sparse images.
package disk

import (
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

func init() {
	walk.Register(MBR, EFIGPT, FAT, NTFS, EXT, BTRFS, APFS, LogFS, QCOW, DMG, AndroidSparse)
}

// u64 is the value of a field as the usize of binwalk-ng. A u64 field above the
// largest int is negative in binwalk.Fields, and the conversion gives back
// its bits.
func u64(f binwalk.Fields, name string) uint64 {
	return uint64(f[name])
}
