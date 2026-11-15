package zstd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
	"github.com/xo/magic/compress"
)

func init() {
	compress.RegisterStream(compress.Zstd, streamZstd)
}

// errFrame is the error of a frame that is not valid, or is cut short.
var errFrame = errors.New("zstd: the frame is not valid")

// streamZstd decodes the first frame of src, as ZSTD_decompressStream
// stops at the end of a frame. The size comes from the frame header and the
// block headers, and the decoder checks the data.
func streamZstd(src []byte, w io.Writer) (int, error) {
	n, err := frameSize(src)
	if err != nil {
		return 0, err
	}
	d, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return 0, fmt.Errorf("zstd: %w", err)
	}
	defer d.Close()
	out, err := d.DecodeAll(src[:n], nil)
	if err != nil {
		return 0, fmt.Errorf("zstd: %w", err)
	}
	if _, err := w.Write(out); err != nil {
		return 0, fmt.Errorf("zstd: %w", err)
	}
	return n, nil
}

// frameSize returns the bytes of the frame at the start of src: the
// header, the blocks, and the checksum.
func frameSize(src []byte) (int, error) {
	if len(src) < 5 || binary.LittleEndian.Uint32(src) != 0xFD2FB528 {
		return 0, errFrame
	}
	fhd := src[4]
	p := 5
	single := fhd&0x20 != 0
	if !single {
		p++ // the window descriptor
	}
	p += [4]int{0, 1, 2, 4}[fhd&3]
	fcs := [4]int{0, 2, 4, 8}[fhd>>6]
	if fhd>>6 == 0 && single {
		fcs = 1
	}
	p += fcs
	for {
		if p+3 > len(src) {
			return 0, errFrame
		}
		h := int(src[p]) | int(src[p+1])<<8 | int(src[p+2])<<16
		p += 3
		size := h >> 3
		switch (h >> 1) & 3 {
		case 1:
			size = 1 // RLE: one byte, repeated
		case 3:
			return 0, errFrame
		}
		p += size
		if h&1 != 0 {
			break
		}
	}
	if fhd&0x04 != 0 {
		p += 4
	}
	if p > len(src) {
		return 0, errFrame
	}
	return p, nil
}
