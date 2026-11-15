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
// This file translates src/compress.c of file to Go, at the commit that
// docs/upstream/upstreams.tsv names (D29, D48).

// Package zlib registers the decoders of gzip and zlib data with
// github.com/xo/magic/compress, as the library zlib does in libmagic. Import
// it for its effect:
//
//	import _ "github.com/xo/magic/compress/zlib"
package zlib

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"errors"
	"io"

	"github.com/xo/magic/compress"
)

func init() {
	compress.Register(compress.Gzip, "zlib", gzipped)
	compress.Register(compress.Zlib, "zlib", zlibData)
}

// The flags of the header of gzip.
const (
	fhcrc    = 1 << 1
	fextra   = 1 << 2
	fname    = 1 << 3
	fcomment = 1 << 4
)

var errTooShort = errors.New("File too short")

// gzipped is uncompressgzipped of compress.c: it skips the header of gzip
// and inflates the deflate data after it. libmagic ignores the checksum at
// the end.
func gzipped(src []byte, max int) ([]byte, error) {
	n := len(src)
	if n < 4 {
		return nil, errTooShort
	}
	flg := src[3]
	start := 10
	if flg&fextra != 0 {
		if start+1 >= n {
			return nil, errTooShort
		}
		start += 2 + int(src[start]) + int(src[start+1])*256
	}
	if flg&fname != 0 {
		for start < n && src[start] != 0 {
			start++
		}
		start++
	}
	if flg&fcomment != 0 {
		for start < n && src[start] != 0 {
			start++
		}
		start++
	}
	if flg&fhcrc != 0 {
		start += 2
	}
	if start >= n {
		return nil, errTooShort
	}
	return inflate(flate.NewReader(bytes.NewReader(src[start:])), max)
}

// zlibData is uncompresszlib of compress.c with the header of zlib, which
// checks the Adler-32 at the end.
func zlibData(src []byte, max int) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(src))
	switch {
	case errors.Is(err, zlib.ErrDictionary):
		return nil, errors.New("need dictionary")
	case err != nil:
		// The header is cut, and inflate waits for more.
		return nil, nil
	}
	return inflate(r, max)
}

// inflate is one call of inflate of zlib: what it can decode, at most max
// bytes. Data that is cut short is not an error.
func inflate(r io.Reader, max int) ([]byte, error) {
	out, err := readMax(r, max)
	var ce flate.CorruptInputError
	switch {
	case err == nil || errors.Is(err, io.ErrUnexpectedEOF):
		return out, nil
	case errors.Is(err, zlib.ErrChecksum):
		return nil, errors.New("incorrect data check")
	case errors.As(err, &ce):
		// zlib names the fault, as "invalid distance too far back". Go's
		// flate does not, so this gives the class of the error of zlib.
		return nil, errors.New("data error")
	}
	return nil, err
}

// readMax reads r to its end or to max bytes.
func readMax(r io.Reader, max int) ([]byte, error) {
	var out []byte
	buf := make([]byte, 32*1024)
	for len(out) < max {
		n, err := r.Read(buf[:min(len(buf), max-len(out))])
		out = append(out, buf[:n]...)
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
	return out, nil
}
