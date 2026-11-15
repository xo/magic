//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/svg.rs of binwalk-ng.

package image

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/xo/magic/walk"
)

// SVG is the signature "svg" of binwalk.
var SVG = &walk.Signature{
	Name:        "svg",
	Magic:       [][]byte{[]byte(svgOpen)},
	Description: "SVG image",
	Parse:       parseSVG,
	Extract:     extractSVG,
	NoRecurse:   true,
}

// extractSVG is extract_svg_image: it carves the SVG to image.svg.
func extractSVG(data []byte, off int, out *walk.Output) walk.Extraction {
	size, ok := svgSize(data[off:])
	if !ok {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: out == nil || out.CarveFile("image.svg", data, off, size), Size: size, HasSize: true}
}

// parseSVG is svg_parser.
func parseSVG(data []byte, off int) (walk.Match, error) {
	size, ok := svgSize(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.Medium,
		Description: fmt.Sprintf("SVG image, total size: %d bytes", size),
		Declined:    off == 0,
	}, nil
}

const (
	svgOpen  = "<svg"
	svgClose = "</svg>"
)

// svgSize is parse_svg_image: the size of the SVG up to the close tag that
// closes the first open tag. Each open tag adds one to the depth, and each
// close tag takes one away. The bytes before each tag must be valid UTF-8.
func svgSize(data []byte) (int, bool) {
	var next svgTags
	off, depth := 0, 0
	for {
		start, isOpen := next.find(data, off)
		if start < 0 || !utf8.Valid(data[off:start]) {
			return 0, false
		}
		if isOpen {
			end := start + len(svgOpen)
			// The name of the tag must end with a space, a '>' or a '/'.
			if end >= len(data) || !strings.ContainsRune(" >/\t\r\n", rune(data[end])) {
				return 0, false
			}
			n := bytes.IndexByte(data[end:], '>')
			if n < 0 || !utf8.Valid(data[end:end+n]) {
				return 0, false
			}
			depth++
			off = end + n + 1
		} else {
			if depth == 0 {
				return 0, false
			}
			depth--
			off = start + len(svgClose)
		}
		if depth == 0 {
			return off, true
		}
	}
}

// svgTags finds the open and close tags of SVG, as the matcher of
// binwalk-ng finds them. It keeps the offset of the next tag of each kind,
// so that each search reads the data once.
type svgTags struct {
	open, close int  // the offset of the next tag of each kind, or -1
	ready       bool // true when open and close hold a search
}

// find returns the offset of the first tag at off or after it, or -1, and
// true when it is an open tag. The two tags cannot overlap, so the first of
// the two is the one that the matcher finds.
func (t *svgTags) find(data []byte, off int) (int, bool) {
	search := func(at int, tag string) int {
		if t.ready && (at < 0 || at >= off) {
			return at
		}
		i := bytes.Index(data[off:], []byte(tag))
		if i < 0 {
			return -1
		}
		return off + i
	}
	t.open, t.close = search(t.open, svgOpen), search(t.close, svgClose)
	t.ready = true
	switch {
	case t.open < 0 && t.close < 0:
		return -1, false
	case t.close < 0 || t.open >= 0 && t.open < t.close:
		return t.open, true
	default:
		return t.close, false
	}
}
