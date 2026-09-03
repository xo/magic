// Package magic is a wrapper around file's [libmagic].
//
// [libmagic]: https://astron.com/pub/file/
package magic

/*
#cgo CFLAGS: -I${SRCDIR}/libmagic

#cgo linux,amd64   LDFLAGS: -L${SRCDIR}/libmagic/linux-amd64 -lmagic -lz -lbz2 -llzma -lzstd
#cgo linux,arm64   LDFLAGS: -L${SRCDIR}/libmagic/linux-arm64 -lmagic -lz -lbz2 -llzma -lzstd
#cgo linux,arm     LDFLAGS: -L${SRCDIR}/libmagic/linux-armv7 -lmagic -lz -lbz2 -llzma -lzstd
#cgo darwin,amd64  LDFLAGS: -L${SRCDIR}/libmagic/macos-amd64 -lmagic -lz -lbz2 -llzma -lzstd
#cgo darwin,arm64  LDFLAGS: -L${SRCDIR}/libmagic/macos-arm64 -lmagic -lz -lbz2 -llzma -lzstd
#cgo windows,amd64 LDFLAGS: -L${SRCDIR}/libmagic/windows-amd64 -lmagic -lgnurx -lz -lbz2 -llzma -lzstd

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
	"unsafe"
)

// Magic is an open libmagic handle with the embedded database loaded.
//
// A magic_t cookie holds mutable per-lookup state and is not safe for
// concurrent use. Give each goroutine its own Magic, or guard one with a
// mutex.
type Magic struct {
	cookie C.magic_t
	db     unsafe.Pointer
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

	m := &Magic{cookie: cookie, db: db}
	// Backstop only -- callers should still Close explicitly.
	runtime.SetFinalizer(m, (*Magic).Close)
	return m, nil
}

// Close releases the handle and the database copy. It is safe to call twice.
func (m *Magic) Close() error {
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

// Buffer identifies the contents of buf.
func (m *Magic) Buffer(buf []byte) (string, error) {
	if m.cookie == nil {
		return "", fmt.Errorf("magic: handle is closed")
	}
	if len(buf) == 0 {
		return "", fmt.Errorf("magic: empty buffer")
	}

	// Only borrowed for the duration of the call, so a Go pointer is fine.
	s := C.magic_buffer(m.cookie, unsafe.Pointer(&buf[0]), C.size_t(len(buf)))
	runtime.KeepAlive(buf)
	if s == nil {
		return "", cookieErr(m.cookie, "magic_buffer")
	}
	return C.GoString(s), nil
}

// File identifies the file at name.
func (m *Magic) File(name string) (string, error) {
	if m.cookie == nil {
		return "", fmt.Errorf("magic: handle is closed")
	}

	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	s := C.magic_file(m.cookie, cname)
	if s == nil {
		return "", cookieErr(m.cookie, "magic_file")
	}
	return C.GoString(s), nil
}

// SetFlags changes the flags on an open handle.
func (m *Magic) SetFlags(flags int) error {
	if C.magic_setflags(m.cookie, C.int(flags)) != 0 {
		return cookieErr(m.cookie, "magic_setflags")
	}
	return nil
}

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
