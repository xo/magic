// Package zstd registers the decoder of zstd data with
// github.com/xo/magic/compress, as the library zstd does in libmagic. It is a
// module of its own, because it needs github.com/klauspost/compress (D28,
// D45). Import it for its effect:
//
//	import _ "github.com/xo/magic/compress/zstd"
package zstd

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
	"github.com/xo/magic/compress"
)

func init() {
	compress.Register(compress.Zstd, "zstd", decode)
}

// The codes of ZSTD_ErrorCode that libmagic prints, as "zstd error %d".
const (
	errPrefixUnknown    = 10
	errWindowTooLarge   = 16
	errFrameUnsupported = 14
	errCorruption       = 20
	errChecksumWrong    = 22
	errDictionary       = 32
)

// maxWindow is the largest window that libzstd decodes by default,
// 1 << ZSTD_WINDOWLOG_LIMIT_DEFAULT.
const maxWindow = 1 << 27

// decode is uncompresszstd of compress.c: one call of
// ZSTD_decompressStream, which gives what it can decode, at most max bytes.
func decode(src []byte, max int) ([]byte, error) {
	// libzstd reads nothing until it holds the whole header of the frame,
	// and only then checks it.
	if len(src) < headerSize(src) {
		return nil, nil
	}
	d, err := zstd.NewReader(bytes.NewReader(src), zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxWindow(maxWindow), zstd.WithDecoderLowmem(true))
	if err != nil {
		return nil, zstdError(err)
	}
	defer d.Close()
	var out []byte
	buf := make([]byte, 32*1024)
	for len(out) < max {
		n, err := d.Read(buf[:min(len(buf), max-len(out))])
		out = append(out, buf[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			if e := zstdError(err); e != nil {
				return nil, e
			}
			break
		}
	}
	return out, nil
}

// headerSize is ZSTD_frameHeaderSize: the size of the header of the frame
// at the start of src, from its descriptor.
func headerSize(src []byte) int {
	if len(src) < 5 {
		return 5
	}
	fhd := src[4]
	dictSize := [4]int{0, 1, 2, 4}[fhd&3]
	single := int(fhd >> 5 & 1)
	fcsID := fhd >> 6
	fcsSize := [4]int{0, 2, 4, 8}[fcsID]
	n := 4 + 1 + (1 - single) + dictSize + fcsSize
	if single == 1 && fcsID == 0 {
		n++
	}
	return n
}

// zstdError gives the error of libzstd for err. Data that is cut short is
// not an error: libzstd waits for more.
func zstdError(err error) error {
	code := errCorruption
	switch {
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		return nil
	case errors.Is(err, zstd.ErrMagicMismatch):
		code = errPrefixUnknown
	case errors.Is(err, zstd.ErrUnknownDictionary):
		code = errDictionary
	case errors.Is(err, zstd.ErrWindowSizeExceeded), errors.Is(err, zstd.ErrWindowSizeTooSmall):
		code = errWindowTooLarge
	case errors.Is(err, zstd.ErrCRCMismatch):
		code = errChecksumWrong
	case err.Error() == "reserved bit set on frame header":
		code = errFrameUnsupported
	}
	return fmt.Errorf("zstd error %d", code)
}
