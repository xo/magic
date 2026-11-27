//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Package filesystem registers the file systems that binwalk-ng finds and
// extracts: RomFS, which binwalk-ng extracts with its own code, SquashFS,
// which binwalk-ng extracts with sasquatch, JFFS2, which binwalk-ng
// extracts with jefferson, YAFFS2, which binwalk-ng extracts with unyaffs,
// CramFS and ISO 9660, which binwalk-ng extracts with 7z, and UBI and
// UBIFS, which binwalk-ng extracts with ubireader_extract_images and
// ubireader_extract_files, and MINIFS, which binwalk-ng extracts with its
// own code.
package filesystem

import "github.com/xo/magic/walk"

func init() {
	walk.Register(SquashFS, RomFS, JFFS2, YAFFS, CramFS, ISO9660, UBI, UBIFS, MINIFS)
}

// endianName is the text of Endianness of binwalk-ng, which a description
// names.
func endianName(big bool) string {
	if big {
		return "Big Endian"
	}
	return "Little Endian"
}
