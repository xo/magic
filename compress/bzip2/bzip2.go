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
