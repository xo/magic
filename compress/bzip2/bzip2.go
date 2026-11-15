// Package bzip2 registers the decoder of bzip2 data with
// github.com/xo/magic/compress, as the library bzlib does in libmagic.
// Import it for its effect:
//
//	import _ "github.com/xo/magic/compress/bzip2"
package bzip2

import (
	"bytes"
	"compress/bzip2"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/xo/magic/compress"
)

func init() {
	compress.Register(compress.Bzip2, "bzlib", decode)
}

// The errors of libbz2 that libmagic prints.
const (
	bzDataError      = -4
	bzDataErrorMagic = -5
)

// decode is uncompressbzlib of compress.c: one call of BZ2_bzDecompress,
// which gives the blocks that it can decode, at most max bytes.
func decode(src []byte, max int) ([]byte, error) {
	r := bzip2.NewReader(bytes.NewReader(src))
	var out []byte
	buf := make([]byte, 32*1024)
	for len(out) < max {
		n, err := r.Read(buf[:min(len(buf), max-len(out))])
		out = append(out, buf[:n]...)
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			code := bzDataError
			var se bzip2.StructuralError
			if errors.As(err, &se) && strings.Contains(string(se), "magic") {
				code = bzDataErrorMagic
			}
			return nil, fmt.Errorf("bunzip error %d", code)
		}
	}
	return out, nil
}
