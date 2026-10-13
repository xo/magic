// Package magic is a wrapper around file's [libmagic].
//
// [libmagic]: https://astron.com/pub/file/
package magic

/*
#cgo CFLAGS: -I${SRCDIR}/libmagic

#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include "magic.h"

// magic_load_buffers takes parallel arrays of pointers and sizes. Building
// those in Go means a []unsafe.Pointer whose address gets passed to C, which
// is legal but fiddly; wrapping the single-buffer case here keeps the Go side
// free of pointer-array juggling.
static int magic_load_one(magic_t ms, void *buf, size_t len) {
	return magic_load_buffers(ms, &buf, &len, 1);
}
*/
import "C"

import (
	_ "embed"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// Magic is an open libmagic handle with the embedded database loaded.
//
// A magic_t cookie holds mutable per-lookup state and is not safe for
// concurrent use, so every method below serializes on mu. That makes a single
// Magic safe to share across goroutines, at the cost of serializing lookups:
// for parallel throughput, give each worker its own handle.
type Magic struct {
	cookie C.magic_t
	db     unsafe.Pointer

	mu sync.Mutex
	// flags is the handle's configured flag set, from New or SetFlags.
	flags int
	// cur is what is actually applied to the cookie right now, which differs
	// from flags only while a With/Types call is borrowing it. Tracking it
	// lets consecutive lookups at the same flags skip the magic_setflags.
	cur int
}

// New opens a handle and loads the embedded database.
func New(flags int) (*Magic, error) {
	cookie := C.magic_open(C.int(flags))
	if cookie == nil {
		return nil, fmt.Errorf("magic_open failed")
	}

	// magic_load_buffers does not copy: it keeps the pointer for the lifetime
	// of the cookie, and byte-swaps the contents in place when the database's
	// endianness doesn't match the host. Handing it MagicDB directly would
	// both retain a Go pointer past the call (which cgo forbids) and risk
	// writing to a read-only section, so copy into C memory once.
	db := C.CBytes(MagicDB)
	if C.magic_load_one(cookie, db, C.size_t(len(MagicDB))) != 0 {
		err := cookieErr(cookie, "magic_load_buffers")
		C.free(db)
		C.magic_close(cookie)
		return nil, err
	}

	m := &Magic{cookie: cookie, db: db, flags: flags, cur: flags}
	// Backstop only -- callers should still Close explicitly.
	runtime.SetFinalizer(m, (*Magic).Close)
	return m, nil
}

// Close releases the handle and the database copy. It is safe to call twice,
// and safe to race with other methods: they report a closed handle rather
// than touching a freed cookie.
func (m *Magic) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cookie != nil {
		C.magic_close(m.cookie)
		m.cookie = nil
	}
	if m.db != nil {
		C.free(m.db)
		m.db = nil
	}
	runtime.SetFinalizer(m, nil)
	return nil
}

// Flags returns the handle's configured flags.
func (m *Magic) Flags() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.flags
}

// SetFlags changes the flags on an open handle.
func (m *Magic) SetFlags(flags int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cookie == nil {
		return errClosed
	}
	if err := m.apply(flags); err != nil {
		return err
	}
	m.flags = flags
	return nil
}

// Buffer identifies the contents of buf using the handle's flags.
func (m *Magic) Buffer(buf []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buffer(m.flags, buf)
}

// File identifies the file at name using the handle's flags.
func (m *Magic) File(name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.file(m.flags, name)
}

// BufferWith identifies the contents of buf using flags, leaving the handle's
// configured flags unchanged.
func (m *Magic) BufferWith(flags int, buf []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buffer(flags, buf)
}

// FileWith identifies the file at name using flags, leaving the handle's
// configured flags unchanged.
func (m *Magic) FileWith(flags int, name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.file(flags, name)
}

// BufferTypes identifies the contents of buf once per entry in flags,
// returning a result for each. Passing no flags uses the handle's own.
//
// The whole sequence runs under a single lock, so the results describe the
// same buffer through the same handle with no other goroutine interleaving:
//
//	desc, err := m.BufferTypes(buf, magic.None, magic.MimeType, magic.Extension)
func (m *Magic) BufferTypes(buf []byte, flags ...int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.each(flags, func(f int) (string, error) {
		return m.buffer(f, buf)
	})
}

// FileTypes identifies the file at name once per entry in flags, returning a
// result for each. Passing no flags uses the handle's own.
//
// The whole sequence runs under a single lock, so the results describe the
// same file through the same handle with no other goroutine interleaving:
//
//	desc, err := m.FileTypes("x.png", magic.None, magic.MimeType, magic.Extension)
func (m *Magic) FileTypes(name string, flags ...int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.each(flags, func(f int) (string, error) {
		return m.file(f, name)
	})
}

// each runs fn once per flag set and collects the results. Caller must hold
// m.mu.
func (m *Magic) each(flags []int, fn func(int) (string, error)) ([]string, error) {
	if m.cookie == nil {
		return nil, errClosed
	}
	if len(flags) == 0 {
		flags = []int{m.flags}
	}
	// Hand the cookie back on the configured flags. Purely an optimization:
	// cur is reconciled lazily, so a plain lookup would fix it up anyway --
	// doing it once here just saves a magic_setflags per borrow. The error
	// is moot for the same reason, and m.flags applied cleanly already.
	defer func() { _ = m.apply(m.flags) }()

	out := make([]string, len(flags))
	for i, f := range flags {
		s, err := fn(f)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

// buffer is Buffer without the locking. Caller must hold m.mu.
func (m *Magic) buffer(flags int, buf []byte) (string, error) {
	if m.cookie == nil {
		return "", errClosed
	}
	if len(buf) == 0 {
		return "", fmt.Errorf("magic: empty buffer")
	}
	if err := m.apply(flags); err != nil {
		return "", err
	}

	// Only borrowed for the duration of the call, so a Go pointer is fine.
	s := C.magic_buffer(m.cookie, unsafe.Pointer(&buf[0]), C.size_t(len(buf)))
	runtime.KeepAlive(buf)
	if s == nil {
		return "", cookieErr(m.cookie, "magic_buffer")
	}
	return C.GoString(s), nil
}

// file is File without the locking. Caller must hold m.mu.
func (m *Magic) file(flags int, name string) (string, error) {
	if m.cookie == nil {
		return "", errClosed
	}
	if err := m.apply(flags); err != nil {
		return "", err
	}

	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	s := C.magic_file(m.cookie, cname)
	if s == nil {
		return "", cookieErr(m.cookie, "magic_file")
	}
	return C.GoString(s), nil
}

// apply points the cookie at flags, skipping the call when it is already
// there. Caller must hold m.mu and must have checked for a closed handle.
func (m *Magic) apply(flags int) error {
	if m.cur == flags {
		return nil
	}
	if C.magic_setflags(m.cookie, C.int(flags)) != 0 {
		return cookieErr(m.cookie, "magic_setflags")
	}
	m.cur = flags
	return nil
}

// errClosed is returned by every method that needs a live cookie.
var errClosed = fmt.Errorf("magic: handle is closed")

// Version returns the libmagic version as a dotted string, e.g. "5.48".
//
// magic_version() encodes the minor part in the low two digits: 548 is 5.48,
// 504 is 5.04. The %02d matters -- without it 5.04 would render as "5.4".
func Version() string {
	v := int(C.magic_version())
	return fmt.Sprintf("%d.%02d", v/100, v%100)
}

// VersionInt returns the libmagic version as an int.
func VersionInt() int {
	return int(C.magic_version())
}

// cookieErr turns libmagic's last error into a Go error. The string it
// returns is owned by the cookie and only valid until the next call, so
// GoString's copy matters here.
func cookieErr(cookie C.magic_t, op string) error {
	if s := C.magic_error(cookie); s != nil {
		return fmt.Errorf("%s: %s", op, C.GoString(s))
	}
	return fmt.Errorf("%s failed", op)
}

// Magic flags. Mirrors MAGIC_* constants in magic.h.
const (
	None          = C.MAGIC_NONE
	Debug         = C.MAGIC_DEBUG
	Symlink       = C.MAGIC_SYMLINK
	Compress      = C.MAGIC_COMPRESS
	Devices       = C.MAGIC_DEVICES
	MimeType      = C.MAGIC_MIME_TYPE
	MimeEncoding  = C.MAGIC_MIME_ENCODING
	Mime          = C.MAGIC_MIME
	Continue      = C.MAGIC_CONTINUE
	Check         = C.MAGIC_CHECK
	PreserveATime = C.MAGIC_PRESERVE_ATIME
	Raw           = C.MAGIC_RAW
	Error         = C.MAGIC_ERROR
	Extension     = C.MAGIC_EXTENSION
)

// MagicDB is the compiled magic database. It is byte-identical for every
// target -- the format is endian-independent and libmagic byte-swaps on load
// -- so one embedded copy serves all platforms.
//
//go:embed libmagic/magic.mgc
var MagicDB []byte
