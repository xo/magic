//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Package image registers the images that binwalk finds: PNG, JPEG, GIF,
// BMP, RIFF and SVG.
package image

import "github.com/xo/magic/walk"

func init() {
	walk.Register(BMP, PNG, JPEG, RIFF, GIF, SVG)
}
