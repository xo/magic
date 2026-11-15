// Package engine is the port of the engine of libmagic 5.48: it runs the
// compiled rules and the detectors on a buffer or a file, and gives the text
// that libmagic gives (D22, D34).
//
// The flags are the MAGIC_* flags of libmagic, so that the differential
// test can ask the port and the oracle the same question. The public API of
// docs/API.md maps its options onto them.
package engine

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/xo/magic/internal/rules"
)

// Flags are the flags of magic_open, with the values of magic.h.
type Flags int

// The flags that the engine knows.
const (
	None         Flags = 0x0000000
	Continue     Flags = 0x0000020
	Raw          Flags = 0x0000100
	MimeType     Flags = 0x0000010
	MimeEncoding Flags = 0x0000400
	Mime               = MimeType | MimeEncoding
	Apple        Flags = 0x0000800
	Extension    Flags = 0x1000000
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
	e     *Engine
	flags Flags
	out   []byte
	err   error

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
}

// printf appends to the output, as file_printf does.
func (st *state) printf(format string, args ...any) {
	st.out = fmt.Appendf(st.out, format, args...)
}

// errorf records an error, as file_error does. The first error wins.
func (st *state) errorf(format string, args ...any) {
	if st.err == nil {
		st.err = fmt.Errorf(format, args...)
	}
}

// magErr records an error of the rules, as file_magerror does.
func (st *state) magErr(format string, args ...any) {
	st.errorf(format, args...)
}

// Buffer returns what libmagic says of data with flags, as magic_buffer.
func (e *Engine) Buffer(data []byte, flags Flags) (string, error) {
	if data == nil {
		data = []byte{}
	}
	st := &state{e: e, flags: flags}
	return st.result(st.buffer(&buffer{data: data}, len(data)))
}

// File returns what libmagic says of the file at path with flags, as
// magic_file.
func (e *Engine) File(path string, flags Flags) (string, error) {
	st := &state{e: e, flags: flags}
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot stat `%s' (%w)", path, err)
	}
	st.mode = statMode(fi)
	switch {
	case fi.IsDir():
		if flags&Mime != 0 {
			st.mime("directory")
		} else {
			st.printf("directory")
		}
		return st.result(1)
	case !fi.Mode().IsRegular():
		return "", fmt.Errorf("%s: not a regular file", path)
	case fi.Size() == 0:
		if flags&Mime != 0 {
			st.mime("x-empty")
		} else {
			st.printf("empty")
		}
		return st.result(1)
	}
	f, err := os.Open(path)
	if err != nil {
		// unreadable_info: libmagic describes a file that it can stat and
		// not open.
		if writable(path) {
			st.printf("writable, ")
		}
		if executable(path) {
			st.printf("executable, ")
		}
		st.printf("regular file, no read permission")
		return st.result(1)
	}
	defer f.Close()
	data := make([]byte, bytesMax)
	n, err := io.ReadFull(f, data)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", fmt.Errorf("cannot read `%s' (%w)", path, err)
	}
	data = data[:n]
	info := &fileInfo{regular: true, size: fi.Size(), r: f}
	if fi.Size() <= int64(n) {
		info.tail = data
	} else {
		tl := min(fi.Size(), bytesMax)
		info.tail = make([]byte, tl)
		if _, err := f.ReadAt(info.tail, fi.Size()-tl); err != nil {
			return "", fmt.Errorf("cannot read `%s' (%w)", path, err)
		}
	}
	return st.result(st.buffer(&buffer{data: data, file: info}, n))
}

// mime prints a MIME type that fsmagic finds, as handle_mime.
func (st *state) mime(sub string) {
	if st.flags&MimeType != 0 {
		st.printf("inode/%s", sub)
		if st.flags&MimeEncoding != 0 {
			st.printf("; charset=")
		}
	}
	if st.flags&MimeEncoding != 0 {
		st.printf("binary")
	}
}

// statMode returns the st_mode of fi, for the rules that ask whether a file
// can run.
func statMode(fi os.FileInfo) uint32 {
	m := uint32(fi.Mode().Perm())
	if fi.Mode().IsRegular() {
		m |= 0o100000
	}
	return m
}

// result is what magic_buffer returns: the output, or the error.
func (st *state) result(rv int) (string, error) {
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
