// Package compress is the registry of the decoders that magic uses to look
// inside compressed data. It holds none itself. A program chooses the
// formats that it decodes with a blank import of their packages, as it does
// with the drivers of database/sql:
//
//	import (
//		_ "github.com/xo/magic/compress/bzip2"
//		_ "github.com/xo/magic/compress/zlib"
//	)
//
// libmagic links one library for each format: zlib for gzip and zlib data,
// bzlib for bzip2, xzlib for xz and lzma, and zstd. Each package here takes
// the place of one library, and registers the formats that the library
// decodes. For a format with no decoder, magic answers as libmagic does when
// it was built without the library and finds no program that decodes it.
package compress

import (
	"fmt"
	"sync"
)

// Method is a format of compressed data that a decoder can register.
type Method int

// The formats that libmagic decodes with a library.
const (
	Gzip Method = iota + 1
	Zlib
	Bzip2
	XZ
	LZMA
	Zstd
)

// Decoder decompresses src. It returns at most max bytes, and as much of the
// data as it can decode when src is cut short. The text of its error is the
// text that libmagic prints for the error of its library, such as
// "bunzip error -4".
type Decoder func(src []byte, max int) ([]byte, error)

type entry struct {
	name    string
	decoder Decoder
}

var (
	mu       sync.RWMutex
	decoders = map[Method]entry{}
)

// Register makes d the decoder of m. name is the name of the library that
// libmagic prints in an error, such as "zlib". Register panics when m has a
// decoder, or when d is nil.
func Register(m Method, name string, d Decoder) {
	mu.Lock()
	defer mu.Unlock()
	if d == nil {
		panic(fmt.Sprintf("compress: Register of method %d with a nil decoder", m))
	}
	if _, ok := decoders[m]; ok {
		panic(fmt.Sprintf("compress: Register called twice for method %d", m))
	}
	decoders[m] = entry{name, d}
}

// Lookup returns the decoder of m and the name of its library, and false
// when m has none.
func Lookup(m Method) (string, Decoder, bool) {
	mu.RLock()
	defer mu.RUnlock()
	e, ok := decoders[m]
	return e.name, e.decoder, ok
}
