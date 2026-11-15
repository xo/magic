// Copyright (c) Christos Zoulas 2003.
// All Rights Reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice immediately at the beginning of the file, without modification,
//    this list of conditions, and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED. IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE FOR
// ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
// This file translates src/magic.c, src/funcs.c of file to Go, at the commit
// that docs/upstream/upstreams.tsv names (D29, D48).

// Package engine is the port of the engine of libmagic 5.48: it runs the
// compiled rules and the detectors on a buffer or a file, and gives the text
// that libmagic gives (D22, D34).
//
// The flags are the MAGIC_* flags of libmagic, so that the differential
// test can ask the port and the oracle the same question. The public API of
// docs/API.md maps its options onto them.
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/xo/magic/internal/rules"
)

// Flags are the flags of magic_open, with the values of magic.h.
type Flags int

// The flags that the engine knows.
const (
	None     Flags = 0x0000000
	Continue Flags = 0x0000020
	Symlink  Flags = 0x0000002
	Devices  Flags = 0x0000008
	Raw      Flags = 0x0000100
	Error    Flags = 0x0000200

	// The flags that turn off one test of libmagic each.
	NoCheckCompress Flags = 0x0001000
	NoCheckTar      Flags = 0x0002000
	NoCheckSoft     Flags = 0x0004000
	NoCheckAppType  Flags = 0x0008000
	NoCheckELF      Flags = 0x0010000
	NoCheckText     Flags = 0x0020000
	NoCheckCDF      Flags = 0x0040000
	NoCheckCSV      Flags = 0x0080000
	NoCheckTokens   Flags = 0x0100000
	NoCheckEncoding Flags = 0x0200000
	NoCheckJSON     Flags = 0x0400000
	NoCheckSIMH     Flags = 0x0800000
	MimeType        Flags = 0x0000010
	MimeEncoding    Flags = 0x0000400
	Mime                  = MimeType | MimeEncoding
	Apple           Flags = 0x0000800
	Extension       Flags = 0x1000000
	// NoDesc is MAGIC_NODESC: an output that prints no description.
	NoDesc = Extension | Mime | Apple
)

// Engine runs a compiled database. It is safe for concurrent use.
type Engine struct {
	sets  [2]*table
	names map[string]nameRef
}

// nameRef is where the rule of a name starts in set 1, and how many
// entries it has, as file_magicfind finds it.
type nameRef struct{ start, n int }

// regexSlot holds the compiled regular expression of one entry, compiled
// the first time a rule needs it.
type regexSlot struct {
	once sync.Once
	rx   *regex
	err  error
}

// New returns an engine for db.
func New(db *rules.Database) *Engine {
	e := &Engine{names: map[string]nameRef{}}
	for i, set := range db.Sets {
		t := &table{}
		for _, g := range set {
			t.entries = append(t.entries, g...)
		}
		t.rx = make([]regexSlot, len(t.entries))
		e.sets[i] = t
	}
	// file_magicfind takes the first rule of the name, and ends it at the
	// next line at level 0.
	t := e.sets[1]
	for i := range t.entries {
		m := &t.entries[i]
		if m.Type != rules.Name {
			continue
		}
		name := string(cstr(m.Value[:]))
		if _, ok := e.names[name]; ok {
			continue
		}
		j := i + 1
		for j < len(t.entries) && t.entries[j].ContLevel != 0 {
			j++
		}
		e.names[name] = nameRef{i, j - i}
	}
	return e
}

// regex returns the compiled regular expression of entry idx of t.
func (t *table) regex(idx int) (*regex, error) {
	s := &t.rx[idx]
	s.once.Do(func() {
		m := &t.entries[idx]
		icase := m.StrFlags()&(rules.StrIgnoreLowercase|rules.StrIgnoreUppercase) != 0
		s.rx, s.err = compileRegex(cstr(m.Value[:]), icase)
		if s.err != nil {
			s.err = fmt.Errorf("line %d: %w", m.Lineno, s.err)
		}
	})
	return s.rx, s.err
}

// state is what one detection changes, as struct magic_set of libmagic.
type state struct {
	e        *Engine
	flags    Flags
	ctx      context.Context
	bytesMax int
	out      []byte
	err      error

	offset  uint32
	eoffset uint32
	mode    uint32 // st_mode of the file

	search struct {
		s      []byte // from the start of the search to the end of the buffer
		null   bool   // the search has no buffer
		sLen   uint64
		offset uint64
		rmLen  uint64
	}
	val value
	li  []levelInfo
	// windows holds the bytes that regular expressions read, as Latin-1.
	windows windows
	line    uint32 // the line of the rule that runs, for an error
}

// printf appends to the output, as file_printf does. After an error it
// prints nothing. One call prints at most 1024 bytes, and the output holds at
// most 1 MiB.
func (st *state) printf(format string, args ...any) int {
	return st.put(fmt.Appendf(nil, format, args...))
}

// cprintf is file_printf with a C format from a rule. It refuses a format
// that file_checkfmt refuses.
func (st *state) cprintf(format []byte, args ...carg) int {
	if st.err != nil {
		return 0
	}
	if r := checkPrintfFormat(format); r != "" {
		st.out = st.out[:0]
		st.errorf("Bad magic format `%s' (%s)", format, r)
		return -1
	}
	return st.put(cprintf(format, args...))
}

// put appends s, as the end of file_vprintf.
func (st *state) put(s []byte) int {
	if st.err != nil {
		return 0
	}
	if i := bytes.IndexByte(s, 0); i >= 0 {
		// vasprintf makes a C string.
		s = s[:i]
	}
	if len(s) > 1024 || len(s)+len(st.out) > 1024*1024 {
		blen := len(st.out)
		st.out = st.out[:0]
		st.errorf("Output buffer space exceeded %d+%d", len(s), blen)
		return -1
	}
	if st.out == nil {
		// The output exists now, even when s is empty.
		st.out = []byte{}
	}
	st.out = append(st.out, s...)
	return 0
}

// pushed is file_pushbuf_t: the output and the offset that push saved.
type pushed struct {
	out    []byte
	offset uint32
}

// push is file_push_buffer: it saves the output and the offset, and starts
// an empty output.
func (st *state) push() pushed {
	pb := pushed{out: st.out, offset: st.offset}
	st.out, st.offset = nil, 0
	return pb
}

// pop is file_pop_buffer: it returns the output since push, and restores
// what push saved. It fails after an error.
func (st *state) pop(pb pushed) ([]byte, bool) {
	if st.err != nil {
		return nil, false
	}
	inner := st.out
	st.out, st.offset = pb.out, pb.offset
	return inner, true
}

// errorf records an error, as file_error does: the text of the error
// follows the output so far, and the first error wins.
func (st *state) errorf(format string, args ...any) {
	st.errorAt(0, fmt.Sprintf(format, args...))
}

// magErr records an error of a rule, as file_magerror does: the output
// becomes the line of the rule and the text.
func (st *state) magErr(format string, args ...any) {
	st.errorAt(st.line, fmt.Sprintf(format, args...))
}

// errorAt is file_error_core.
func (st *state) errorAt(line uint32, msg string) {
	if st.err != nil {
		return
	}
	var b []byte
	if line != 0 {
		b = fmt.Appendf(b, "line %d:", line)
	} else {
		b = append(b, st.out...)
	}
	if len(b) > 0 {
		b = append(b, ' ')
	}
	b = append(b, msg...)
	st.err = errors.New(string(b))
}

// Buffer returns what libmagic says of data with flags, as magic_buffer.
func (e *Engine) Buffer(data []byte, flags Flags) (string, error) {
	return e.BufferContext(context.Background(), data, Call{Flags: flags})
}

// Call is what one detection takes besides its input.
type Call struct {
	Flags Flags
	// Limit is how many bytes are read and examined, bytes_max of
	// libmagic. 0 means 7 MiB.
	Limit int
}

// newState returns the state of one detection.
func (e *Engine) newState(ctx context.Context, c Call) *state {
	limit := c.Limit
	if limit <= 0 {
		limit = bytesMax
	}
	return &state{e: e, flags: c.Flags, ctx: ctx, bytesMax: limit}
}

// BufferContext is Buffer with a context and a limit. The engine does not
// keep data, and does not change it.
func (e *Engine) BufferContext(ctx context.Context, data []byte, c Call) (string, error) {
	if data == nil {
		data = []byte{}
	}
	st := e.newState(ctx, c)
	if len(data) > st.bytesMax {
		data = data[:st.bytesMax]
	}
	return st.result(st.buffer(&buffer{data: data}, len(data)))
}

// ReaderAtContext describes the size bytes of r, as libmagic describes an
// open regular file: it reads the start of r, and the end of r for a rule
// that reads from the end.
func (e *Engine) ReaderAtContext(ctx context.Context, r io.ReaderAt, size int64, c Call) (string, error) {
	st := e.newState(ctx, c)
	n := int(min(size, int64(st.bytesMax)))
	data := make([]byte, n)
	got, err := r.ReadAt(data, 0)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("reading: %w", err)
	}
	data = data[:got]
	info := &fileInfo{regular: true, size: size, r: r}
	if size <= int64(got) {
		info.tail = data
	} else {
		tl := min(size, int64(st.bytesMax))
		info.tail = make([]byte, tl)
		if _, err := r.ReadAt(info.tail, size-tl); err != nil && err != io.EOF {
			return "", fmt.Errorf("reading: %w", err)
		}
	}
	return st.result(st.buffer(&buffer{data: data, file: info}, len(data)))
}

// result is what magic_buffer returns: the output, or the error.
func (st *state) result(rv int) (string, error) {
	if st.ctx != nil {
		if err := st.ctx.Err(); err != nil {
			return "", err
		}
	}
	if st.err != nil {
		return "", st.err
	}
	if rv == -1 {
		return "", fmt.Errorf("magic: the detection failed")
	}
	if st.flags&Raw != 0 {
		return string(st.out), nil
	}
	return printable(st.out), nil
}

// printable is file_getbuffer: in the C locale, each byte that is not
// printable becomes an octal escape, the newline of a separator too. The
// output ends at its first NUL, as a C string.
func printable(b []byte) string {
	b = cstr(b)
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if isPrint(c) {
			out = append(out, c)
			continue
		}
		out = append(out, '\\', '0'+(c>>6)&3, '0'+(c>>3)&7, '0'+c&7)
	}
	return string(out)
}
