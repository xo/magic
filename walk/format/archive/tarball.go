//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/tarball.rs of binwalk-ng. binwalk-ng
// extracts a tar archive with its own code, through the crate tar, and
// extractTarball writes the files that it writes.

package archive

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// Tarball is the signature "tarball" of binwalk-ng.
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
// header. A header counts when its checksum and its magic are valid, and its
// entry ends in the data.
func parseTarball(data []byte, off int) (walk.Match, error) {
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
		size, ok := tarEntrySize(block)
		if !ok || size > len(data)-next {
			break
		}
		count++
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
	want, ok := tarOctal(block[tarChecksumStart:tarChecksumEnd])
	if !ok {
		return false
	}
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
// in whole blocks.
func tarEntrySize(block []byte) (int, bool) {
	if string(block[tarMagicOffset:tarMagicOffset+tarMagicSize]) != tarUniversalMagic {
		return 0, false
	}
	size, ok := tarOctal(block[tarSizeOffset : tarSizeOffset+tarSizeLen])
	if !ok {
		return 0, false
	}
	return (1 + (size+tarBlockSize-1)/tarBlockSize) * tarBlockSize, true
}

// tarOctal is tarball_octal. A NUL or a space ends the number, and only
// NULs and spaces can follow it. Any other byte rejects the field. The
// field is at most 11 digits, so the number cannot overflow.
func tarOctal(b []byte) (int, bool) {
	n, ended := 0, false
	for _, c := range b {
		switch {
		case c >= '0' && c <= '7':
			if ended {
				return 0, false
			}
			n = n*8 + int(c-'0')
		case c == 0 || c == ' ':
			ended = true
		default:
			return 0, false
		}
	}
	return n, true
}

// counter counts the bytes that the reader of tar reads. It hides the
// method Seek of the bytes, so that the reader reads each byte that it
// skips, and the count is the position in the archive.
type counter struct {
	r io.Reader
	n int
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err //nolint:wrapcheck // a reader returns io.EOF as it is
}

// extractTarball is extract_tarball. It reads the entries from off to the
// end of the data, and stops at the first entry that it cannot read. A
// folder, a regular file and a link are written. A hard link becomes a
// symbolic link to its target. Another type of entry is skipped, and counts
// as written. The size is the end of the data of the last entry that was
// read, in whole blocks.
func extractTarball(data []byte, off int, out *walk.Output) walk.Extraction {
	b, ok := binwalk.From(data, off)
	if !ok {
		return walk.Extraction{}
	}
	c := &counter{r: bytes.NewReader(b)}
	tr := tar.NewReader(c)
	wrote, consumed := false, 0
	// The modes of the folders are set after each entry is written, so that
	// a folder without the permission to write still gets its files.
	type dir struct {
		name string
		mode uint32
	}
	var dirs []dir
	for {
		h, err := tr.Next()
		if err != nil && !errors.Is(err, tar.ErrInsecurePath) {
			break
		}
		// After Next, the count is the position of the data of the entry.
		consumed = min(c.n+int(max(h.Size, 0)), len(b))
		consumed = (consumed + tarBlockSize - 1) / tarBlockSize * tarBlockSize
		if out == nil {
			wrote = true
			continue
		}
		mode := uint32(h.Mode) & 0o7777
		var ok bool
		switch h.Typeflag {
		case tar.TypeDir:
			ok = out.CreateDirectory(h.Name)
			if ok {
				dirs = append(dirs, dir{h.Name, mode})
			}
		case tar.TypeSymlink, tar.TypeLink:
			ok = out.CreateSymlink(h.Name, h.Linkname)
			if ok {
				out.SetOwnership(h.Name, uint32(h.Uid), uint32(h.Gid))
			}
		case tar.TypeReg, tar.TypeCont, tar.TypeGNUSparse:
			// The content keeps the bytes before an error of the data, as
			// the crate tar reads a cut entry.
			content, _ := io.ReadAll(tr)
			out.CreateDirectory(path.Dir(h.Name))
			ok = out.CreateFile(h.Name, content)
			if ok {
				out.SetOwnership(h.Name, uint32(h.Uid), uint32(h.Gid))
				out.SetMode(h.Name, mode)
			}
		default:
			ok = true
		}
		wrote = wrote || ok
	}
	for _, d := range dirs {
		out.SetOwnership(d.name, 0, 0)
		out.SetMode(d.name, d.mode)
	}
	if !wrote {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: true, Size: consumed, HasSize: true}
}
