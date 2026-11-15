//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/svg.rs, src/structures/svg.rs and
// src/extractors/svg.rs of binwalk.

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
	Magic:       [][]byte{[]byte("<svg ")},
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
	svgOpen  = "<svg "
	svgClose = "</svg>"
	svgHead  = `xmlns="http://www.w3.org/2000/svg"`
)

// svgSize is parse_svg_image: the size of the SVG up to the close tag that
// closes every tag, after exactly one tag that names the namespace of SVG.
func svgSize(data []byte) (int, bool) {
	heads, unclosed := 0, 0
	for i := 0; i < len(data); i++ {
		// The open and the close tag cannot overlap, so a match of either
		// at each offset gives the order of the matcher of binwalk.
		if !bytes.HasPrefix(data[i:], []byte(svgOpen)) && !bytes.HasPrefix(data[i:], []byte(svgClose)) {
			continue
		}
		head, open, closed, ok := svgTag(data[i:])
		if !ok {
			break
		}
		if head {
			heads++
		}
		if open {
			unclosed++
		}
		if closed {
			unclosed--
		}
		if heads > 1 {
			break
		}
		if heads == 1 && unclosed == 0 {
			return i + len(svgClose), true
		}
	}
	return 0, false
}

// svgTag is parse_svg_tag. A tag ends at the first '>' after which the bytes
// of the tag are valid UTF-8.
func svgTag(data []byte) (head, open, closed, ok bool) {
	for i, c := range data {
		if c != '>' || !utf8.Valid(data[:i+1]) {
			continue
		}
		tag := string(data[:i+1])
		return strings.Contains(tag, svgHead), strings.HasPrefix(tag, svgOpen), strings.HasPrefix(tag, svgClose), true
	}
	return false, false, false, false
}
