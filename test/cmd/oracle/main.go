//go:build oracle

// Command oracle answers questions with the reference libmagic of D33: file
// 5.48 built from the checkout against the C library of the host. The tests
// of the test module start it and talk to it, one JSON object a line on
// standard input and output. test/oracle/build.sh builds it.
//
// It runs in a process of its own, because the code under test can define the
// same C symbols, as the cgo package of today does.
package main

/*
#cgo CFLAGS: -I${SRCDIR}/../../.oracle/root/include
#cgo LDFLAGS: -L${SRCDIR}/../../.oracle/root/lib -lmagic -lz -lbz2 -llzma -lzstd

#include <stdlib.h>
#include <time.h>
#include <locale.h>
#include "magic.h"
*/
import "C"

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"unsafe"
)

// request is one question. Op is "buffer", which reads Data, or "file", which
// reads the file at Path. Magic names the rule files to load, joined by ':',
// as the variable MAGIC of file names them. Empty means the database that
// the build installed.
type request struct {
	ID    int    `json:"id"`
	Op    string `json:"op"`
	Flags int    `json:"flags"`
	Data  []byte `json:"data,omitempty"`
	Path  string `json:"path,omitempty"`
	Magic string `json:"magic,omitempty"`
}

// response is the answer. Text is what libmagic returned. Error is what
// magic_error said when libmagic returned NULL, or a fault of the oracle.
type response struct {
	ID    int    `json:"id"`
	Text  string `json:"text"`
	Error string `json:"error,omitempty"`
}

// noCompressFork stops libmagic from running an outside program to
// decompress, as the port runs none (D36).
const noCompressFork = C.MAGIC_NO_COMPRESS_FORK

func main() {
	// The reference runs in the C locale with TZ=UTC (D33), as the tests of
	// file do.
	os.Setenv("TZ", "UTC")
	C.tzset()
	C.setlocale(C.LC_ALL, C.CString("C"))

	cookies := map[string]C.magic_t{}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 64<<20)
	out := json.NewEncoder(os.Stdout)
	for in.Scan() {
		var req request
		if err := json.Unmarshal(in.Bytes(), &req); err != nil {
			fmt.Fprintf(os.Stderr, "oracle: reading a request: %v\n", err)
			os.Exit(2)
		}
		if err := out.Encode(answer(cookies, req)); err != nil {
			fmt.Fprintf(os.Stderr, "oracle: writing a response: %v\n", err)
			os.Exit(2)
		}
	}
	if err := in.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "oracle: reading standard input: %v\n", err)
		os.Exit(2)
	}
}

// answer runs one request. It keeps one cookie for each set of rule files,
// because loading the database is the slow part.
func answer(cookies map[string]C.magic_t, req request) response {
	res := response{ID: req.ID}
	ms, ok := cookies[req.Magic]
	if !ok {
		ms = C.magic_open(C.MAGIC_NONE)
		if ms == nil {
			res.Error = "oracle: magic_open failed"
			return res
		}
		var path *C.char
		if req.Magic != "" {
			path = C.CString(req.Magic)
			defer C.free(unsafe.Pointer(path))
		}
		if C.magic_load(ms, path) != 0 {
			res.Error = "oracle: magic_load: " + C.GoString(C.magic_error(ms))
			C.magic_close(ms)
			return res
		}
		cookies[req.Magic] = ms
	}
	if C.magic_setflags(ms, C.int(req.Flags|noCompressFork)) != 0 {
		res.Error = "oracle: magic_setflags: " + C.GoString(C.magic_error(ms))
		return res
	}
	var s *C.char
	switch req.Op {
	case "buffer":
		var p unsafe.Pointer
		if len(req.Data) > 0 {
			p = C.CBytes(req.Data)
			defer C.free(p)
		}
		s = C.magic_buffer(ms, p, C.size_t(len(req.Data)))
	case "file":
		path := C.CString(req.Path)
		defer C.free(unsafe.Pointer(path))
		s = C.magic_file(ms, path)
	default:
		res.Error = fmt.Sprintf("oracle: unknown op %q", req.Op)
		return res
	}
	if s == nil {
		if e := C.magic_error(ms); e != nil {
			res.Error = C.GoString(e)
		} else {
			res.Error = "libmagic returned no text and no error"
		}
		return res
	}
	res.Text = C.GoString(s)
	return res
}
