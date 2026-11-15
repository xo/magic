//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/bzip2.rs and src/extractors/bzip2.rs
// of binwalk.

package compressed

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/bzip2"
)

// Bzip2 is the signature "bzip2" of binwalk.
var Bzip2 = &walk.Signature{
	Name: "bzip2",
	Magic: [][]byte{
		[]byte("BZh91AY&SY"), []byte("BZh81AY&SY"), []byte("BZh71AY&SY"),
		[]byte("BZh61AY&SY"), []byte("BZh51AY&SY"), []byte("BZh41AY&SY"),
		[]byte("BZh31AY&SY"), []byte("BZh21AY&SY"), []byte("BZh11AY&SY"),
	},
	Description: "bzip2 compressed data",
	Parse:       parseBzip2,
	Extract:     extractBzip2,
}

// parseBzip2 is bzip2_parser.
func parseBzip2(data []byte, off int) (walk.Match, error) {
	r := extractBzip2(data, off, nil)
	if !r.OK {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        r.Size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("bzip2 compressed data, total size: %d bytes", r.Size),
	}, nil
}

// extractBzip2 is bzip2_decompressor: it decodes the first stream at off,
// and writes what comes out to decompressed.bin when out is set. A stream
// that is cut short or damaged fails, as with libbz2.
func extractBzip2(data []byte, off int, out *walk.Output) walk.Extraction {
	w := &appender{out: out, name: "decompressed.bin"}
	n, err := bzip2.Decode(data[off:], w)
	if err != nil || w.failed {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true, Size: n, HasSize: true}
}

// appender appends what it gets to a file of an output, or counts it in a
// dry run.
type appender struct {
	out    *walk.Output
	name   string
	n      int
	failed bool
}

func (a *appender) Write(p []byte) (int, error) {
	if a.out != nil && len(p) > 0 && !a.out.AppendFile(a.name, p) {
		a.failed = true
		return 0, errWrite
	}
	a.n += len(p)
	return len(p), nil
}
