//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/autel.rs of binwalk-ng.

package vendor

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// Autel is the signature "autel" of binwalk.
var Autel = &walk.Signature{
	Name:        "autel",
	Magic:       [][]byte{[]byte("ECC0101\x00")},
	Description: "Autel obfuscated firmware",
	Parse:       parseAutel,
	Extract:     extractAutel,
}

// parseAutel is autel_parser.
func parseAutel(data []byte, off int) (walk.Match, error) {
	headerSize, dataSize, ok := autelHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := headerSize + dataSize
	// The header and the data must end in the data.
	if off+size > len(data) {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       size,
		Confidence: walk.Medium,
		Description: fmt.Sprintf("Autel obfuscated firmware, header size: %d bytes, data size: %d, total size: %d",
			headerSize, dataSize, size),
	}, nil
}

var autelStruct = binwalk.Struct{
	{Name: "magic", Type: "u64"},
	{Name: "data_size", Type: "u32"},
	{Name: "header_size", Type: "u32"},
}

// autelHeader is parse_autel_header. It returns the size of the header and
// of the data. A copyright string of 16 bytes follows the structure.
func autelHeader(data []byte) (int, int, bool) {
	const (
		headerSize    = 0x20
		copyrightSize = 16
		copyright     = "Copyright Autel"
	)
	h, ok := autelStruct.Parse(data, false)
	if !ok || h["header_size"] != headerSize {
		return 0, 0, false
	}
	start := autelStruct.Size()
	b, ok := binwalk.Range(data, start, start+copyrightSize)
	if !ok || binwalk.CString(b) != copyright {
		return 0, 0, false
	}
	return h["header_size"], h["data_size"], true
}

// extractAutel is autel_deobfuscate: it decodes the data after the header,
// in blocks of 256 bytes, to autel.decoded.
func extractAutel(data []byte, off int, out *walk.Output) walk.Extraction {
	const blockSize = 256
	headerSize, dataSize, ok := autelHeader(data[off:])
	if !ok {
		return walk.Extraction{}
	}
	start := off + headerSize
	b, ok := binwalk.Range(data, start, start+dataSize)
	if !ok {
		return walk.Extraction{}
	}
	for i := 0; i < len(b); i += blockSize {
		block := decodeAutelBlock(b[i:min(i+blockSize, len(b))])
		// binwalk stops at a failed write, and reports a failure.
		if out != nil && !out.AppendFile("autel.decoded", block) {
			return walk.Extraction{}
		}
	}
	return walk.Extraction{OK: true, Size: dataSize, HasSize: true}
}

// decodeAutelBlock is decode_autel_block. A block has at most 256 bytes.
func decodeAutelBlock(block []byte) []byte {
	out := make([]byte, len(block))
	for i, c := range block {
		e := autelTable[i]
		out[i] = byte((int(c) + e[0]) ^ e[1])
	}
	return out
}

// autelTable is the table of decode_autel_block: for each byte of a block,
// a number to add and a number to XOR.
var autelTable = [256][2]int{
	{54, 147}, {96, 129}, {59, 193}, {191, 0}, {45, 130}, {96, 144},
	{27, 129}, {152, 0}, {44, 180}, {118, 141}, {115, 129}, {210, 0},
	{13, 164}, {27, 133}, {20, 192}, {139, 0}, {28, 166}, {17, 133},
	{19, 193}, {224, 0}, {20, 161}, {145, 0}, {14, 193}, {12, 132},
	{18, 161}, {17, 140}, {29, 192}, {246, 0}, {115, 178}, {28, 132},
	{155, 0}, {12, 132}, {31, 165}, {20, 136}, {27, 193}, {142, 0},
	{96, 164}, {18, 133}, {145, 0}, {23, 132}, {13, 165}, {13, 148},
	{23, 193}, {19, 132}, {27, 178}, {83, 137}, {146, 0}, {145, 0},
	{18, 166}, {96, 148}, {13, 193}, {159, 0}, {96, 166}, {20, 129},
	{20, 193}, {27, 132}, {9, 160}, {96, 148}, {13, 192}, {159, 0},
	{96, 180}, {142, 0}, {31, 193}, {155, 0}, {7, 166}, {224, 0},
	{20, 192}, {27, 132}, {28, 160}, {17, 149}, {19, 193}, {96, 132},
	{76, 164}, {208, 0}, {80, 192}, {78, 132}, {96, 160}, {27, 144},
	{24, 193}, {140, 0}, {96, 178}, {17, 141}, {12, 193}, {224, 0},
	{14, 161}, {17, 141}, {151, 0}, {14, 132}, {16, 165}, {96, 137},
	{13, 193}, {155, 0}, {20, 161}, {29, 141}, {23, 192}, {24, 132},
	{27, 178}, {10, 133}, {96, 192}, {140, 0}, {14, 180}, {17, 133},
	{16, 192}, {144, 0}, {11, 163}, {13, 141}, {96, 192}, {17, 132},
	{12, 178}, {96, 141}, {28, 192}, {27, 132}, {27, 130}, {18, 141},
	{96, 193}, {31, 132}, {96, 181}, {13, 140}, {23, 193}, {224, 0},
	{27, 166}, {142, 0}, {27, 192}, {24, 132}, {12, 183}, {96, 133},
	{84, 192}, {14, 132}, {27, 178}, {10, 140}, {155, 0}, {9, 132},
	{17, 160}, {56, 133}, {96, 192}, {82, 132}, {13, 160}, {27, 137},
	{20, 193}, {139, 0}, {28, 161}, {145, 0}, {19, 192}, {118, 132},
	{115, 165}, {20, 132}, {145, 0}, {14, 132}, {12, 167}, {146, 0},
	{17, 193}, {29, 132}, {96, 176}, {28, 144}, {27, 193}, {140, 0},
	{31, 180}, {148, 0}, {27, 192}, {14, 132}, {83, 160}, {18, 137},
	{17, 193}, {23, 132}, {13, 165}, {13, 145}, {151, 0}, {147, 0},
	{27, 178}, {96, 137}, {19, 193}, {159, 0}, {14, 160}, {25, 148},
	{17, 193}, {142, 0}, {16, 180}, {27, 136}, {14, 193}, {224, 0},
	{17, 178}, {12, 144}, {224, 0}, {28, 132}, {27, 160}, {13, 141},
	{11, 193}, {96, 132}, {27, 165}, {30, 140}, {224, 0}, {146, 0},
	{31, 165}, {29, 129}, {96, 192}, {140, 0}, {31, 161}, {24, 145},
	{140, 0}, {96, 132}, {27, 165}, {29, 140}, {31, 192}, {154, 0},
	{14, 161}, {27, 145}, {140, 0}, {18, 132}, {23, 167}, {96, 140},
	{21, 129}, {14, 132}, {17, 165}, {9, 137}, {12, 193}, {155, 0},
	{18, 161}, {96, 141}, {27, 192}, {148, 0}, {29, 178}, {23, 133},
	{24, 192}, {155, 0}, {10, 180}, {96, 133}, {28, 192}, {14, 132},
	{31, 130}, {28, 129}, {18, 193}, {31, 132}, {12, 180}, {13, 144},
	{96, 193}, {31, 132}, {96, 160}, {13, 141}, {27, 193}, {18, 132},
	{23, 181}, {26, 140}, {27, 193}, {156, 0}, {96, 166}, {79, 141},
	{211, 0}, {76, 132}, {77, 160}, {75, 133}, {206, 0}, {182, 0},
	{96, 129}, {59, 133}, {191, 0}, {173, 0},
}
