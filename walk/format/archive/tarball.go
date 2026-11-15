//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/tarball.rs of binwalk. binwalk
// extracts a tar archive with tar, from src/extractors/tarball.rs, and
// extractTarball writes the files that tar writes.

package archive

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// Tarball is the signature "tarball" of binwalk.
var Tarball = &walk.Signature{
	Name:        "tarball",
	Magic:       [][]byte{[]byte("ustar\x00"), []byte("ustar  \x00")},
	Description: "POSIX tar archive",
	Parse:       parseTarball,
	Extract:     extractTarball,
}

// The constants of tarball.rs.
const (
	tarBlockSize       = 512
	tarMagicOffset     = 257
	tarMagicSize       = 5
	tarSizeOffset      = 124
	tarSizeLen         = 11
	tarMinHeaders      = 10
	tarChecksumStart   = 148
	tarChecksumEnd     = 156
	tarUniversalMagic  = "ustar"
	tarDescriptionText = "POSIX tar archive"
)

// parseTarball is tarball_parser. The magic is at 257 bytes into the first
// header.
func parseTarball(data []byte, off int) (walk.Match, error) {
	// binwalk subtracts in a usize, which wraps below 0 to an offset past
	// the data, and finds nothing.
	start := off - tarMagicOffset
	if start < 0 {
		return walk.Match{}, walk.ErrNoMatch
	}
	total, count := 0, 0
	next, previous := start, -1
	for binwalk.OffsetSafe(len(data), next, previous) {
		block, ok := binwalk.Range(data, next, next+tarBlockSize)
		if !ok || !tarChecksumValid(block) {
			break
		}
		count++
		size, ok := tarEntrySize(block)
		if !ok {
			break
		}
		total += size
		previous = next
		next += size
	}
	if total < tarBlockSize {
		return walk.Match{}, walk.ErrNoMatch
	}
	confidence := walk.Medium
	if count >= tarMinHeaders {
		confidence = walk.High
	}
	return walk.Match{
		Offset:      start,
		Size:        total,
		Confidence:  confidence,
		Description: fmt.Sprintf("%s, file count: %d", tarDescriptionText, count),
	}, nil
}

// tarChecksumValid is header_checksum_is_valid: the sum of the bytes of the
// header, with spaces in place of the checksum, against the checksum.
func tarChecksumValid(block []byte) bool {
	want := tarOctal(block[tarChecksumStart:tarChecksumEnd])
	sum := 0
	for i, c := range block {
		if i >= tarChecksumStart && i < tarChecksumEnd {
			sum += 0x20
		} else {
			sum += int(c)
		}
	}
	return sum == want
}

// tarEntrySize is tarball_entry_size: the header and the data of an entry,
// in whole blocks. binwalk divides in an f32, which loses precision above
// 2^24 bytes, and the Go code does the same.
func tarEntrySize(block []byte) (int, bool) {
	if string(block[tarMagicOffset:tarMagicOffset+tarMagicSize]) != tarUniversalMagic {
		return 0, false
	}
	size := tarOctal(block[tarSizeOffset : tarSizeOffset+tarSizeLen])
	blocks := 1 + int(math.Ceil(float64(float32(size)/float32(tarBlockSize))))
	return blocks * tarBlockSize, true
}

// tarOctal is tarball_octal. It stops at the first byte that is not a
// digit, and takes 8 and 9 as digits, as binwalk does.
func tarOctal(b []byte) int {
	n := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			break
		}
		n = n*8 + int(c-'0')
	}
	return n
}

// extractTarball writes the members of the tar archive whose first header
// is at off, as tar -x writes them from the bytes that binwalk carves. A
// hard link is a copy of its target. tar stops at a header that it cannot
// read, and keeps what it wrote, so the extractor does the same.
func extractTarball(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := parseTarball(data, off+tarMagicOffset)
	if err != nil || m.Offset != off {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	// binwalk carves the bytes of the match to a file, and fails when the
	// match goes past the end of the data.
	b, ok := binwalk.Range(data, off, off+m.Size)
	if !ok {
		return walk.Extraction{}
	}
	w := &writer{out: out}
	// files holds the content and the mode of each regular file, for the
	// hard links to it.
	type file struct {
		data []byte
		mode fs.FileMode
	}
	files := map[string]file{}
	tr := tar.NewReader(bytes.NewReader(b))
	for !w.stopped {
		h, err := tr.Next()
		if err != nil && !errors.Is(err, tar.ErrInsecurePath) {
			break
		}
		mem := member{name: h.Name, mode: fs.FileMode(h.Mode) & fs.ModePerm, link: h.Linkname}
		switch h.Typeflag {
		case tar.TypeReg, tar.TypeCont, tar.TypeGNUSparse:
			b, err := io.ReadAll(tr)
			if err != nil {
				return w.result()
			}
			files[h.Name] = file{b, mem.mode}
			mem.data = bytes.NewReader(b)
		case tar.TypeLink:
			// A hard link shares the mode of its target.
			f, ok := files[h.Linkname]
			if !ok {
				continue
			}
			files[h.Name] = f
			mem.data, mem.mode = bytes.NewReader(f.data), f.mode
		case tar.TypeDir:
			mem.mode |= fs.ModeDir
		case tar.TypeSymlink:
			mem.mode |= fs.ModeSymlink
		case tar.TypeChar:
			mem.mode |= fs.ModeDevice | fs.ModeCharDevice
		case tar.TypeBlock:
			mem.mode |= fs.ModeDevice
		case tar.TypeFifo:
			mem.mode |= fs.ModeNamedPipe
		default:
			continue
		}
		mem.major, mem.minor = int(h.Devmajor), int(h.Devminor)
		w.write(mem)
	}
	return w.result()
}
