//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package archive

import (
	"bytes"
	"compress/bzip2"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"
	"slices"

	"github.com/xo/magic/compress"
)

// The identifiers of the coders that the walker decodes, as 7-Zip writes
// them in Methods.txt.
const (
	szIDCopy    = "\x00"
	szIDDelta   = "\x03"
	szIDX86     = "\x03\x03\x01\x03"
	szIDBCJ2    = "\x03\x03\x01\x1B"
	szIDPPC     = "\x03\x03\x02\x05"
	szIDIA64    = "\x03\x03\x04\x01"
	szIDARM     = "\x03\x03\x05\x01"
	szIDARMT    = "\x03\x03\x07\x01"
	szIDSPARC   = "\x03\x03\x08\x05"
	szIDARM64   = "\x0A"
	szIDLZMA    = "\x03\x01\x01"
	szIDLZMA2   = "\x21"
	szIDDeflate = "\x04\x01\x08"
	szIDBZip2   = "\x04\x02\x02"
)

// szMethod decodes the input streams of the coder c to w. size is the size
// of its output, from the header.
type szMethod func(c *szCoder, in [][]byte, size int, w io.Writer) error

// szMethods holds the decoder of each method that takes its input streams
// whole. To add a method, add its identifier and its function here.
var szMethods = map[string]szMethod{
	szIDCopy:    szDecodeCopy,
	szIDLZMA:    szDecodeLZMA,
	szIDLZMA2:   szDecodeLZMA2,
	szIDDeflate: szDecodeDeflate,
	szIDBZip2:   szDecodeBZip2,
	szIDBCJ2:    szDecodeBCJ2,
	szIDPPMd:    szDecodePPMd,
}

// szFilters holds the filters of one stream, which change the data in
// place as it streams. Each function makes the state of the filter from
// the properties of the coder, and reports false for properties that
// 7-Zip rejects.
var szFilters = map[string]func(props []byte) (szConverter, bool){
	szIDDelta: newSzDelta,
	szIDX86:   newSzX86,
	szIDPPC:   szBranchFilter(szPPCConvert),
	szIDIA64:  szBranchFilter(szIA64Convert),
	szIDARM:   szBranchFilter(szARMConvert),
	szIDARMT:  szBranchFilter(szARMTConvert),
	szIDSPARC: szBranchFilter(szSPARCConvert),
	szIDARM64: newSzARM64,
}

// szGraph decodes the coders of a folder.
type szGraph struct {
	f       *szFolder
	packs   [][]byte // the packed streams, in the order of f.packed
	firstIn []int    // the index of the first input stream of each coder
}

// decode writes the output of the coder j to w. depth counts the coders
// above it, so that a loop of bind pairs stops.
func (g *szGraph) decode(j int, w io.Writer, depth int) error {
	if j < 0 || j >= len(g.f.coders) || depth > len(g.f.coders) {
		return errSevenZip
	}
	c := &g.f.coders[j]
	if mk, ok := szFilters[c.id]; ok {
		conv, ok := mk(c.props)
		if !ok || c.numIn != 1 {
			return errSevenZip
		}
		fw := &szFilterWriter{c: conv, w: w}
		if err := g.input(g.firstIn[j], fw, depth); err != nil {
			return err
		}
		return fw.flush()
	}
	m, ok := szMethods[c.id]
	if !ok {
		return errSevenZip
	}
	in := make([][]byte, c.numIn)
	for k := range in {
		b, err := g.inputBytes(g.firstIn[j]+k, depth)
		if err != nil {
			return err
		}
		in[k] = b
	}
	return m(c, in, g.f.sizes[j], w)
}

// input writes the input stream k to w: a packed stream, or the output of
// the coder that a bind pair gives to it.
func (g *szGraph) input(k int, w io.Writer, depth int) error {
	if b := slices.Index(g.f.bindIn, k); b >= 0 {
		return g.decode(g.f.bound[b], w, depth+1)
	}
	if p := slices.Index(g.f.packed, k); p >= 0 {
		if _, err := w.Write(g.packs[p]); err != nil {
			return fmt.Errorf("writing a packed stream: %w", err)
		}
		return nil
	}
	return errSevenZip
}

// inputBytes gives the input stream k whole. A packed stream is not copied.
func (g *szGraph) inputBytes(k int, depth int) ([]byte, error) {
	if p := slices.Index(g.f.packed, k); p >= 0 {
		return g.packs[p], nil
	}
	b := slices.Index(g.f.bindIn, k)
	if b < 0 || g.f.bound[b] < 0 || g.f.bound[b] >= len(g.f.sizes) {
		return nil, errSevenZip
	}
	buf := &szBuffer{max: g.f.sizes[g.f.bound[b]]}
	if err := g.decode(g.f.bound[b], buf, depth+1); err != nil {
		return nil, err
	}
	return buf.b, nil
}

// szDecodeCopy is the method Copy.
func szDecodeCopy(_ *szCoder, in [][]byte, size int, w io.Writer) error {
	if len(in) != 1 || size > len(in[0]) {
		return errSevenZip
	}
	if _, err := w.Write(in[0][:size]); err != nil {
		return fmt.Errorf("writing the data of Copy: %w", err)
	}
	return nil
}

// szDecodeLZMA is the method LZMA. It needs the stream decoder of LZMA,
// which github.com/xo/magic/compress/xz registers.
func szDecodeLZMA(c *szCoder, in [][]byte, size int, w io.Writer) error {
	dec, ok := compress.LookupStream(compress.LZMA)
	if !ok || len(in) != 1 || len(c.props) != 5 {
		return errSevenZip
	}
	// The header of a .lzma file: the properties, and the size of the data.
	b := make([]byte, 0, 13+len(in[0]))
	b = append(b, c.props...)
	b = binary.LittleEndian.AppendUint64(b, uint64(size))
	_, err := dec(append(b, in[0]...), w)
	return err
}

// szDecodeLZMA2 is the method LZMA2. It needs the stream decoder of xz,
// which github.com/xo/magic/compress/xz registers.
func szDecodeLZMA2(c *szCoder, in [][]byte, size int, w io.Writer) error {
	dec, ok := compress.LookupStream(compress.XZ)
	if !ok || len(in) != 1 || len(c.props) != 1 {
		return errSevenZip
	}
	_, err := dec(szXZ(c.props[0], in[0], size), w)
	return err
}

// szDecodeDeflate is the method Deflate.
func szDecodeDeflate(_ *szCoder, in [][]byte, size int, w io.Writer) error {
	if len(in) != 1 {
		return errSevenZip
	}
	fr := flate.NewReader(bytes.NewReader(in[0]))
	_, err := io.Copy(w, io.LimitReader(fr, int64(size)))
	_ = fr.Close()
	if err != nil {
		return fmt.Errorf("decoding deflate: %w", err)
	}
	return nil
}

// szDecodeBZip2 is the method BZip2.
func szDecodeBZip2(_ *szCoder, in [][]byte, size int, w io.Writer) error {
	if len(in) != 1 {
		return errSevenZip
	}
	if _, err := io.Copy(w, io.LimitReader(bzip2.NewReader(bytes.NewReader(in[0])), int64(size))); err != nil {
		return fmt.Errorf("decoding bzip2: %w", err)
	}
	return nil
}
