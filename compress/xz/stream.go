package xz

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/ulikunitz/xz"
	"github.com/ulikunitz/xz/lzma"
	"github.com/xo/magic/compress"
)

func init() {
	compress.RegisterStream(compress.XZ, streamXZ)
	compress.RegisterStream(compress.LZMA, streamLZMA)
}

// errStream is the error of a stream that is not valid, or is cut short.
var errStream = errors.New("xz: the stream is not valid")

// streamXZ decodes the first xz stream of src, as the auto decoder of
// liblzma without LZMA_CONCATENATED: it stops after the footer, and reads
// no padding. The size comes from the structure of the stream, and the
// reader of xz checks the data and its checks.
func streamXZ(src []byte, w io.Writer) (int, error) {
	n, err := xzSize(src)
	if err != nil {
		return 0, err
	}
	r, err := xz.ReaderConfig{SingleStream: true}.NewReader(bytes.NewReader(src[:n]))
	if err != nil {
		return 0, fmt.Errorf("xz: %w", err)
	}
	if _, err := io.Copy(w, r); err != nil {
		return 0, fmt.Errorf("xz: %w", err)
	}
	return n, nil
}

// checkSizes is the size of the check of each check type of xz.
var checkSizes = [16]int{0, 4, 4, 4, 8, 8, 8, 16, 16, 16, 32, 32, 32, 64, 64, 64}

// xzSize returns the bytes of the first stream of src: the header, the
// blocks, the index and the footer. Each block is a header, LZMA2 chunks,
// padding to four bytes, and a check.
func xzSize(src []byte) (int, error) {
	const headerSize, footerSize = 12, 12
	if len(src) < headerSize || !bytes.HasPrefix(src, []byte("\xFD7zXZ\x00")) {
		return 0, errStream
	}
	check := checkSizes[src[7]&0x0F]
	p := headerSize
	for {
		if p >= len(src) {
			return 0, errStream
		}
		if src[p] == 0 {
			break // the index
		}
		start := p
		p += (int(src[p]) + 1) * 4
		end, err := lzma2End(src, p)
		if err != nil {
			return 0, err
		}
		p = end
		for (p-start)%4 != 0 {
			p++
		}
		p += check
	}
	// The index: the indicator, the count of records, a pair of sizes for
	// each, padding to four bytes, and a CRC-32.
	start := p
	p++
	count, n := uvarint(src, p)
	if n <= 0 {
		return 0, errStream
	}
	p += n
	for range count {
		for range 2 {
			_, n := uvarint(src, p)
			if n <= 0 {
				return 0, errStream
			}
			p += n
		}
	}
	for (p-start)%4 != 0 {
		p++
	}
	p += 4 + footerSize
	if p > len(src) || !bytes.Equal(src[p-2:p], []byte("YZ")) {
		return 0, errStream
	}
	return p, nil
}

// lzma2End returns the end of the LZMA2 chunks that start at p.
func lzma2End(src []byte, p int) (int, error) {
	for {
		if p >= len(src) {
			return 0, errStream
		}
		c := src[p]
		switch {
		case c == 0:
			return p + 1, nil
		case c == 1 || c == 2:
			if p+3 > len(src) {
				return 0, errStream
			}
			p += 3 + int(binary.BigEndian.Uint16(src[p+1:])) + 1
		case c < 0x80:
			return 0, errStream
		default:
			// An LZMA chunk: the control, the size of its data less one,
			// the size of the chunk less one, and the properties when the
			// chunk sets new ones.
			h := 5
			if c >= 0xC0 {
				h = 6
			}
			if p+h > len(src) {
				return 0, errStream
			}
			p += h + int(binary.BigEndian.Uint16(src[p+3:])) + 1
		}
	}
}

// uvarint reads a number of xz at p, and returns its size, or 0.
func uvarint(src []byte, p int) (uint64, int) {
	if p >= len(src) {
		return 0, 0
	}
	v, n := binary.Uvarint(src[p:min(len(src), p+9)])
	return v, n
}

// streamLZMA decodes a stream of .lzma at the start of src, as the auto
// decoder of liblzma. The range decoder reads one byte at a time, so the
// bytes that it read are the size of the stream.
//
// The reader of lzma allocates the whole dictionary that the header names,
// up to 512 MiB in the magic of binwalk, where liblzma maps the memory and
// touches only what it uses. A match reaches back only into data that has
// come out, so streamLZMA first decodes with a dictionary of at most 64
// times the input, and decodes again with the whole one only when that
// fails after more data than the smaller one holds. The output is the same.
func streamLZMA(src []byte, w io.Writer) (int, error) {
	const minDict = 1 << 20
	if len(src) >= 13 {
		dict := binary.LittleEndian.Uint32(src[1:5])
		limit := uint32(min(max(64*len(src), minDict), 1<<31-1))
		if dict > limit {
			small := bytes.Clone(src[:13])
			binary.LittleEndian.PutUint32(small[1:5], limit)
			var buf bytes.Buffer
			n, err := lzmaOnce(append(small, src[13:]...), &buf)
			if err == nil {
				if _, err := w.Write(buf.Bytes()); err != nil {
					return 0, fmt.Errorf("lzma: %w", err)
				}
				return n, nil
			}
			// A distance past the smaller dictionary can be valid only
			// after more data than it holds came out. Any other failure
			// is a failure with the whole dictionary too.
			if buf.Len() <= int(limit) {
				return 0, err
			}
		}
	}
	return lzmaOnce(src, w)
}

// lzmaOnce decodes the stream of .lzma at the start of src.
func lzmaOnce(src []byte, w io.Writer) (int, error) {
	br := bytes.NewReader(src)
	r, err := lzma.NewReader(br)
	if err != nil {
		return 0, fmt.Errorf("lzma: %w", err)
	}
	if _, err := io.Copy(w, r); err != nil {
		return 0, fmt.Errorf("lzma: %w", err)
	}
	return len(src) - br.Len(), nil
}
