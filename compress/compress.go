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
//
// The packages palmdoc, huffcdic and lzx decode formats that libmagic does
// not decode, for MOBI books and CHM files. They do not register (D61).
package compress

import (
	"fmt"
	"io"
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

// Decoder decompresses src. It returns at most limit bytes, and as much of the
// data as it can decode when src is cut short. The text of its error is the
// text that libmagic prints for the error of its library, such as
// "bunzip error -4".
type Decoder func(src []byte, limit int) ([]byte, error)

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

// Stream decodes the first stream at the start of src, as the library of C
// decodes it, and writes its data to w. It returns the bytes of src that
// the stream takes, and stops at the end of the stream, before any data that
// follows it. The binary walker uses it to find where a stream ends (D52).
type Stream func(src []byte, w io.Writer) (int, error)

var streams = map[Method]Stream{}

// RegisterStream makes s the stream decoder of m. It panics when m has one,
// or when s is nil.
func RegisterStream(m Method, s Stream) {
	mu.Lock()
	defer mu.Unlock()
	if s == nil {
		panic(fmt.Sprintf("compress: RegisterStream of method %d with a nil decoder", m))
	}
	if _, ok := streams[m]; ok {
		panic(fmt.Sprintf("compress: RegisterStream called twice for method %d", m))
	}
	streams[m] = s
}

// LookupStream returns the stream decoder of m, and false when m has none.
func LookupStream(m Method) (Stream, bool) {
	mu.RLock()
	defer mu.RUnlock()
	s, ok := streams[m]
	return s, ok
}
