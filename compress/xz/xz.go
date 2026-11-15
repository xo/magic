// Copyright (c) Ian F. Darwin 1986-1995.
// Software written by Ian F. Darwin and others;
// maintained 1995-present by Christos Zoulas and others.
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
// This file translates src/compress.c of file 5.48 to Go (D29).

// Package xz registers the decoders of xz and lzma data with
// github.com/xo/magic/compress, as the library xzlib does in libmagic. It is
// a module of its own, because it needs github.com/ulikunitz/xz (D28, D45).
// Import it for its effect:
//
//	import _ "github.com/xo/magic/compress/xz"
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
	compress.Register(compress.XZ, "xzlib", decodeXZ)
	compress.Register(compress.LZMA, "xzlib", decodeLZMA)
}

// The values of lzma_ret that libmagic prints, as "unxz error %d".
const (
	lzmaFormatError  = 7
	lzmaOptionsError = 8
	lzmaDataError    = 9
)

// decodeXZ is uncompressxzlib of compress.c on xz data: one call of
// lzma_code, which gives what it can decode, at most max bytes.
func decodeXZ(src []byte, max int) ([]byte, error) {
	r, err := xz.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, lzmaError(err, lzmaFormatError)
	}
	out, err := read(r, max)
	if err != nil || len(out) > 0 {
		return out, err
	}
	// The reader of xz gives nothing until it read a whole block, where
	// liblzma gives what it decoded of a cut block. So decode the LZMA2 data
	// of the first block alone.
	if part, ok := firstBlock(src, max); ok {
		return part, nil
	}
	return out, nil
}

// firstBlock decodes the LZMA2 data of the first block of an xz stream, as
// far as the data goes. It returns false when the block is not plain LZMA2.
func firstBlock(src []byte, max int) ([]byte, bool) {
	const streamHeader = 12
	if len(src) <= streamHeader {
		return nil, false
	}
	p := streamHeader
	size := (int(src[p]) + 1) * 4
	if src[p] == 0 || len(src) < p+size {
		return nil, false
	}
	hdr := src[p : p+size]
	flags := hdr[1]
	if flags&0x03 != 0 {
		// More than one filter, such as BCJ before LZMA2.
		return nil, false
	}
	q := 2
	for _, has := range []bool{flags&0x40 != 0, flags&0x80 != 0} {
		if has {
			_, n := binary.Uvarint(hdr[q:])
			if n <= 0 {
				return nil, false
			}
			q += n
		}
	}
	id, n := binary.Uvarint(hdr[q:])
	if n <= 0 || id != 0x21 {
		return nil, false
	}
	q += n
	if psize, n := binary.Uvarint(hdr[q:]); n <= 0 || psize != 1 {
		return nil, false
	}
	q++
	dictProp := hdr[q]
	if dictProp > 40 {
		return nil, false
	}
	dictCap := 0xffffffff
	if dictProp < 40 {
		dictCap = (2 | int(dictProp&1)) << (dictProp/2 + 11)
	}
	return firstChunk(src[p+size:], uint32(dictCap), max)
}

// firstChunk decodes the first LZMA2 chunk of a block, as far as the data
// goes. The LZMA2 reader drops a cut chunk, but the first chunk is LZMA data
// with its properties, which the reader of .lzma decodes as far as it goes.
func firstChunk(d []byte, dictCap uint32, max int) ([]byte, bool) {
	if len(d) == 0 {
		return nil, true
	}
	control := d[0]
	switch {
	case control == 1 || control == 2:
		// A chunk that is not compressed.
		if len(d) < 3 {
			return nil, true
		}
		n := int(d[1])<<8 | int(d[2]) + 1
		data := d[3:]
		return data[:min(n, len(data), max)], true
	case control < 0xe0:
		return nil, false
	}
	if len(d) < 6 {
		return nil, true
	}
	usize := uint64(control&0x1f)<<16 | uint64(d[1])<<8 | uint64(d[2]) + 1
	hdr := make([]byte, 13)
	hdr[0] = d[5]
	binary.LittleEndian.PutUint32(hdr[1:], dictCap)
	binary.LittleEndian.PutUint64(hdr[5:], usize)
	r, err := lzma.NewReader(bytes.NewReader(append(hdr, d[6:]...)))
	if err != nil {
		return nil, true
	}
	out, err := read(r, max)
	if err != nil {
		return nil, false
	}
	return out, true
}

// decodeLZMA is uncompressxzlib of compress.c on lzma data.
func decodeLZMA(src []byte, max int) ([]byte, error) {
	r, err := lzma.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, lzmaError(err, lzmaOptionsError)
	}
	return read(r, max)
}

// lzmaError gives the error of liblzma for err. Data that is cut short is
// not an error: liblzma waits for more.
func lzmaError(err error, code int) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return nil
	}
	return fmt.Errorf("unxz error %d", code)
}

func read(r io.Reader, max int) ([]byte, error) {
	var out []byte
	buf := make([]byte, 32*1024)
	cut := false
	for len(out) < max {
		n, err := r.Read(buf[:min(len(buf), max-len(out))])
		out = append(out, buf[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			if e := lzmaError(err, lzmaDataError); e != nil {
				return nil, e
			}
			// The reader keeps what it decoded before the end of the
			// input, and gives it on the next reads.
			if cut && n == 0 {
				break
			}
			cut = true
		}
	}
	return out, nil
}
