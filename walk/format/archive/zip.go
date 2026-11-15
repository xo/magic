//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/zip.rs and src/structures/zip.rs of
// binwalk. binwalk extracts a ZIP archive with 7zz, from
// src/extractors/sevenzip.rs, and extractZIP writes the files that 7zz
// writes.

package archive

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"errors"
	"io"
	"io/fs"
	"strings"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/format/internal/zipsig"
	"github.com/xo/magic/walk/internal/binwalk"
)

// ZIP is the signature "zip" of binwalk.
var ZIP = &walk.Signature{
	Name:        "zip",
	Magic:       [][]byte{[]byte("PK\x03\x04")},
	Description: "ZIP archive",
	Parse:       zipsig.Parse,
	Extract:     extractZIP,
}

// extractZIP writes the members of the ZIP archive at off, as 7zz writes
// them from the bytes that binwalk carves. It reads the central directory
// with archive/zip. When the archive has no central directory that
// archive/zip can read, it follows the local file headers, as the parser
// does. A member that is encrypted, or whose method Go cannot decode, is
// skipped.
func extractZIP(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := zipsig.Parse(data, off)
	if err != nil {
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
	r, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		zipLocal(w, b)
		return w.result()
	}
	for _, f := range r.File {
		if w.stopped {
			break
		}
		zipFile(w, f)
	}
	return w.result()
}

// zipFile writes one member of the central directory.
func zipFile(w *writer, f *zip.File) {
	mode := f.Mode()
	m := member{name: f.Name, mode: mode}
	if strings.HasSuffix(f.Name, "/") {
		m.mode |= fs.ModeDir
	}
	if m.mode.IsDir() {
		w.write(m)
		return
	}
	if f.Flags&1 != 0 {
		return
	}
	rc, err := f.Open()
	if err != nil {
		return
	}
	defer func() { _ = rc.Close() }()
	if mode&fs.ModeSymlink != 0 {
		target, err := io.ReadAll(io.LimitReader(rc, 4096))
		if err != nil {
			return
		}
		m.link = string(target)
		w.write(m)
		return
	}
	m.mode = mode & fs.ModePerm
	m.data = rc
	w.write(m)
}

// zipLocal writes the members of the local file headers in b, which the
// parser counted. Only the methods stored and deflated are decoded.
func zipLocal(w *writer, b []byte) {
	next := 0
	for next < len(b) && !w.stopped {
		h, ok := zipsig.Header(b[next:])
		if !ok || next+h.TotalSize > len(b) {
			return
		}
		start := next + zipsig.LocalHeaderSize
		name := string(b[start : start+h.NameLen])
		content := b[next+h.HeaderSize : next+h.TotalSize]
		next += h.TotalSize
		switch {
		case strings.HasSuffix(name, "/"):
			w.write(member{name: name, mode: fs.ModeDir | 0o755})
		case h.Flags&1 != 0:
		case h.Method == int(zip.Store):
			w.write(member{name: name, mode: 0o644, data: bytes.NewReader(content)})
		case h.Method == int(zip.Deflate):
			fr := flate.NewReader(bytes.NewReader(content))
			w.write(member{name: name, mode: 0o644, data: fr})
			_ = fr.Close()
		}
	}
}
