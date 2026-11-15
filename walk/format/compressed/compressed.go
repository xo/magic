//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Package compressed registers the compressed data that binwalk-ng finds:
// gzip, zlib, bzip2, xz, lzma, zstd, the zlib data of a GPG signed file,
// LZ4, lzop, the LZW data of compress, and LZFSE. The walker extracts each
// of them except LZFSE.
//
// The walker decodes xz and lzma with the decoder of
// github.com/xo/magic/compress/xz, and extracts zstd with the decoder of
// github.com/xo/magic/compress/zstd. Each is a module of its own (D45), and
// a program that walks for these formats imports it:
//
//	import _ "github.com/xo/magic/compress/xz"
//
// Without the decoder of xz, the walker rejects each candidate of xz and
// lzma, because binwalk-ng decodes the data to find its size. Without the
// decoder of zstd, it finds zstd data and cannot extract it.
package compressed

import "github.com/xo/magic/walk"

func init() {
	walk.Register(Gzip, XZ, LZMA, Bzip2, Zstd, Zlib, GPGSigned, LZ4, LZOP,
		Compressd, LZFSE)
}
