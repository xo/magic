//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Package filesystem registers the file systems that binwalk finds and
// extracts: RomFS, which binwalk extracts with its own code, SquashFS,
// which binwalk extracts with sasquatch, JFFS2, which binwalk extracts with
// jefferson, YAFFS2, which binwalk extracts with unyaffs, CramFS and
// ISO 9660, which binwalk extracts with 7zz, and UBI and UBIFS, which
// binwalk extracts with ubireader_extract_images and
// ubireader_extract_files.
package filesystem

import "github.com/xo/magic/walk"

func init() {
	walk.Register(SquashFS, RomFS, JFFS2, YAFFS, CramFS, ISO9660, UBI, UBIFS)
}
