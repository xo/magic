//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/zip.rs of binwalk-ng, whose parser is in
// the package zipsig. binwalk-ng extracts a ZIP archive with 7z, from
// src/formats/sevenzip.rs, and extractZIP writes the files that 7z writes.

package archive

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"io"
	"io/fs"
	"strings"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/format/internal/zipsig"
)

// ZIP is the signature "zip" of binwalk-ng.
var ZIP = &walk.Signature{
	Name:        "zip",
	Magic:       [][]byte{[]byte("PK\x03\x04")},
	Description: "ZIP archive",
	Parse:       zipsig.Parse,
	Extract:     extractZIP,
	Carve:       true,
}

// extractZIP writes the members of the ZIP archive at off, as 7z writes
// them from the bytes that binwalk-ng carves, which are the data from off.
// It reads the central directory as 7z does. When the bytes have no
// central directory that it can read, it follows the local file headers.
// A member that is encrypted, or whose method Go cannot decode, is skipped.
func extractZIP(data []byte, off int, out *walk.Output) walk.Extraction {
	if _, err := zipsig.Parse(data, off); err != nil {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	b := data[off:]
	w := &writer{out: out}
	entries, base, ok := zipCentral(b)
	if !ok {
		zipLocal(w, b)
		return w.result()
	}
	for _, e := range entries {
		if w.stopped {
			break
		}
		zipEntry(w, b, base, e)
	}
	return w.result()
}

// zipCentralEntry is an entry of the central directory.
type zipCentralEntry struct {
	creator, flags, method int
	crc                    uint32
	csize, usize, local    int
	attrs                  uint32
	name                   string
}

// zipCentral reads the central directory of b, as 7z finds it: it ends at
// the last end of central directory, and starts the size of the directory
// before it. The local headers are at their offsets from the start of the
// archive, which is where the directory starts less its offset. When that
// start is before b, 7z does not use the directory.
func zipCentral(b []byte) ([]zipCentralEntry, int, bool) {
	const eocdSize, centralSize = 22, 46
	from := max(0, len(b)-(65535+eocdSize))
	end := bytes.LastIndex(b[from:], []byte("PK\x05\x06"))
	if end < 0 {
		return nil, 0, false
	}
	end += from
	if end+eocdSize > len(b) {
		return nil, 0, false
	}
	le := binary.LittleEndian
	count := int(le.Uint16(b[end+10:]))
	start := end - int(le.Uint32(b[end+12:]))
	if start < 0 {
		return nil, 0, false
	}
	var entries []zipCentralEntry
	for p := start; len(entries) < count; {
		if p+centralSize > end || !bytes.HasPrefix(b[p:], []byte("PK\x01\x02")) {
			return nil, 0, false
		}
		n, x, c := int(le.Uint16(b[p+28:])), int(le.Uint16(b[p+30:])), int(le.Uint16(b[p+32:]))
		if p+centralSize+n+x+c > end {
			return nil, 0, false
		}
		entries = append(entries, zipCentralEntry{
			creator: int(le.Uint16(b[p+4:])),
			flags:   int(le.Uint16(b[p+8:])),
			method:  int(le.Uint16(b[p+10:])),
			crc:     le.Uint32(b[p+16:]),
			csize:   int(le.Uint32(b[p+20:])),
			usize:   int(le.Uint32(b[p+24:])),
			attrs:   le.Uint32(b[p+38:]),
			local:   int(le.Uint32(b[p+42:])),
			name:    string(b[p+centralSize : p+centralSize+n]),
		})
		p += centralSize + n + x + c
	}
	base := start - int(le.Uint32(b[end+16:]))
	if base < 0 {
		return nil, 0, false
	}
	return entries, base, true
}

// zipLocalAt reports whether a local file header is at p in b.
func zipLocalAt(b []byte, p int) bool {
	return p >= 0 && p+zipsig.LocalHeaderSize <= len(b) && bytes.HasPrefix(b[p:], []byte("PK\x03\x04"))
}

// zipEntry writes one member of the central directory. When its local
// header is missing, or does not match the entry, 7z writes an empty file.
// Else it writes the data that it decodes, up to the size of the member,
// and keeps the bytes before an error of the data.
func zipEntry(w *writer, b []byte, base int, e zipCentralEntry) {
	h := zip.FileHeader{Name: e.name, CreatorVersion: uint16(e.creator), ExternalAttrs: e.attrs}
	mode := h.Mode()
	m := member{name: e.name, mode: mode}
	if strings.HasSuffix(e.name, "/") {
		m.mode |= fs.ModeDir
	}
	if m.mode.IsDir() {
		w.write(m)
		return
	}
	if e.flags&1 != 0 {
		return
	}
	m.mode = mode & fs.ModePerm
	p := base + e.local
	if !zipLocalAt(b, p) || !zipSameLocal(b[p:], e) {
		w.write(m)
		return
	}
	le := binary.LittleEndian
	start := p + zipsig.LocalHeaderSize + int(le.Uint16(b[p+26:])) + int(le.Uint16(b[p+28:]))
	if start > len(b) {
		w.write(m)
		return
	}
	r := zipDecoder(bytes.NewReader(b[start:min(start+e.csize, len(b))]), e.method, int64(e.usize))
	if r == nil {
		return
	}
	if mode&fs.ModeSymlink != 0 {
		target, _ := io.ReadAll(io.LimitReader(r, 4096))
		m.mode, m.link = mode, string(target)
		w.write(m)
		return
	}
	m.data = r
	w.write(m)
}

// zipSameLocal reports whether the local header at the start of b matches
// the entry e of the central directory, as 7z compares them: the method,
// the flags, and without a data descriptor, the sizes and the CRC-32, which
// can be 0 in the central directory.
func zipSameLocal(b []byte, e zipCentralEntry) bool {
	le := binary.LittleEndian
	flags, method := int(le.Uint16(b[6:])), int(le.Uint16(b[8:]))
	if method != e.method {
		return false
	}
	if flags != e.flags {
		mask := 0xffff
		switch {
		case method == int(zip.Deflate):
			mask = 0x7ff9
		case method <= 6:
			mask = 0x7fff
		}
		if (flags^e.flags)&0x800 != 0 && zipASCII(e.name) {
			mask &^= 0x800
		}
		if flags&mask != e.flags&mask {
			return false
		}
	}
	if flags&8 != 0 {
		return true
	}
	crc := le.Uint32(b[14:])
	return int(le.Uint32(b[18:])) == e.csize && int(le.Uint32(b[22:])) == e.usize && (crc == e.crc || e.crc == 0)
}

// zipASCII reports whether s holds only ASCII.
func zipASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// zipFillReader is the input of the decoder of deflate. 7z reads the bits
// of a stream that is cut short as if 0xFF bytes followed it, and decodes
// the codes that start in the first of those bytes. So ReadByte, which
// gives the bits of the codes, gives 4 bytes of 0xFF after the data. Read,
// which gives the bytes of a stored block, gives only the data.
type zipFillReader struct {
	r    *bufio.Reader
	fill int
}

func (z *zipFillReader) Read(p []byte) (int, error) {
	return z.r.Read(p) //nolint:wrapcheck // a reader returns io.EOF as it is
}

func (z *zipFillReader) ReadByte() (byte, error) {
	c, err := z.r.ReadByte()
	if err == nil || z.fill >= 4 {
		return c, err //nolint:wrapcheck // a reader returns io.EOF as it is
	}
	z.fill++
	return 0xff, nil
}

// zipDecoder returns the reader of the data of a member, which gives at
// most size bytes, or nil for a method that the walker cannot decode.
func zipDecoder(raw io.Reader, method int, size int64) io.Reader {
	switch method {
	case int(zip.Store):
		return raw
	case int(zip.Deflate):
		return io.LimitReader(flate.NewReader(&zipFillReader{r: bufio.NewReader(raw)}), size)
	}
	return nil
}

// zipDescriptor is the signature of a data descriptor.
var zipDescriptor = []byte("PK\x07\x08")

// zipLocal writes the members of the local file headers in b, as 7z reads
// an archive without a central directory. A member whose sizes follow its
// data, in a data descriptor, ends at the first descriptor whose size of
// compressed data is the distance from the start of the data. Without one,
// the member runs to the end of b, and is the last. A member that is cut
// short keeps the bytes that b holds. Only the methods stored and deflated
// are decoded.
func zipLocal(w *writer, b []byte) {
	next := 0
	for next < len(b) && !w.stopped {
		h, ok := zipsig.Header(b[next:])
		if !ok || !bytes.HasPrefix(b[next:], []byte("PK")) {
			return
		}
		if next+h.HeaderSize > len(b) {
			// 7z skips a header that passes the end, and looks for the
			// next one.
			i := bytes.Index(b[next+1:], []byte("PK\x03\x04"))
			if i < 0 {
				return
			}
			next += 1 + i
			continue
		}
		start := next + zipsig.LocalHeaderSize
		name := string(b[start : start+h.NameLen])
		dataStart := next + h.HeaderSize
		end := next + h.TotalSize
		if h.Flags&8 != 0 {
			end, next = len(b), len(b)
			for i := dataStart; ; i++ {
				j := bytes.Index(b[i:], zipDescriptor)
				if j < 0 || i+j+16 > len(b) {
					break
				}
				i += j
				if int(binary.LittleEndian.Uint32(b[i+8:])) == i-dataStart {
					end, next = i, i+16
					break
				}
			}
		} else {
			end = min(end, len(b))
			next = end
		}
		content := bytes.NewReader(b[dataStart:end])
		size := int64(h.Size)
		if h.Flags&8 != 0 || size == 0 {
			size = 1<<63 - 1
		}
		switch {
		case strings.HasSuffix(name, "/"):
			w.write(member{name: name, mode: fs.ModeDir | 0o755})
		case h.Flags&1 != 0:
		default:
			if r := zipDecoder(content, h.Method, size); r != nil {
				w.write(member{name: name, mode: 0o644, data: r})
			}
		}
	}
}
