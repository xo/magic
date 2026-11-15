//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/eva.rs of binwalk-ng.

package exec

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// The magics of an EVA image: a container of two kernels, the record of
// the first kernel, and the record of the second kernel.
const (
	evaDualKernelMagic = 0xFEED9112
	evaTIAR7Magic      = 0xFEED1281
	evaTIAR72ndMagic   = 0xFEEDB007
	evaLZMATypeMagic   = 0x075A0201
	evaFileSigMagic    = 0xC453DE23
)

// The sizes of the structures of an EVA image.
const (
	evaTIHeaderSize      = 12
	evaTITrailerSize     = 12
	evaLZMAHeaderSize    = 16
	evaLZMAStreamHeader  = 8
	evaFileSignatureSize = 8
	// evaFileSignatureScan is how far after the image binwalk-ng looks for
	// the file signature.
	evaFileSignatureScan = 4096
)

// EVA is the signature "eva" of binwalk-ng: a Fritz!Box EVA kernel image.
var EVA = &walk.Signature{
	Name: "eva",
	Magic: [][]byte{
		binary.LittleEndian.AppendUint32(nil, evaDualKernelMagic),
		binary.LittleEndian.AppendUint32(nil, evaTIAR7Magic),
		binary.LittleEndian.AppendUint32(nil, evaTIAR72ndMagic),
	},
	Description: "Fritz!Box EVA kernel image",
	Parse:       parseEVA,
	Extract:     extractEVA,
}

// evaLZMA is EvaLzmaPayload.
type evaLZMA struct {
	compressedLen   int
	uncompressedLen int
	dataCRCValid    bool
	properties      byte
	dictSize        uint32
}

// evaRecord is EvaTiRecord: a TI record, which holds one kernel.
type evaRecord struct {
	loadAddr     uint32
	crcValid     bool
	entryAddr    uint32
	lzma         evaLZMA
	headerOffset int
	totalSize    int
}

// evaFileSignature is EvaFileSignature.
type evaFileSignature struct {
	crc    uint32
	valid  bool
	endOff int
}

// The kinds of EvaImageKind.
const (
	evaSingleKernel = iota
	evaSecondaryFragment
	evaDualKernel
)

// evaImage is EvaImage. A single kernel or a fragment has its record in
// primary.
type evaImage struct {
	kind          int
	primary       evaRecord
	secondary     evaRecord
	trailerValid  bool
	fileSignature *evaFileSignature
	totalSize     int
}

// parseEVA is eva_parser.
func parseEVA(data []byte, off int) (walk.Match, error) {
	img, ok := parseEVAImage(data, off)
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	failures := img.failures()
	conf := walk.High
	if len(failures) != 0 {
		conf = walk.Low
		if off == 0 {
			conf = walk.Medium
		}
	}
	var kind, records string
	switch img.kind {
	case evaSingleKernel:
		kind, records = "single-kernel", evaRecordDesc("primary", img.primary)
	case evaSecondaryFragment:
		kind, records = "secondary-kernel fragment", evaRecordDesc("secondary", img.primary)
	default:
		kind = "dual-kernel"
		records = evaRecordDesc("primary", img.primary) + ", " + evaRecordDesc("secondary", img.secondary)
	}
	desc := "Fritz!Box EVA kernel image, " + kind + ", " + records
	if img.fileSignature != nil {
		desc += fmt.Sprintf(", file signature CRC: 0x%08X", img.fileSignature.crc)
	}
	if len(failures) != 0 {
		desc += ", checksum validation failed (" + strings.Join(failures, ", ") + ")"
	}
	return walk.Match{
		Offset:      off,
		Size:        img.totalSize,
		Confidence:  conf,
		Description: desc,
	}, nil
}

// evaRecordDesc is record_desc.
func evaRecordDesc(label string, r evaRecord) string {
	return fmt.Sprintf("%s load: 0x%X, entry: 0x%X, compressed: %d bytes, uncompressed: %d bytes",
		label, r.loadAddr, r.entryAddr, r.lzma.compressedLen, r.lzma.uncompressedLen)
}

// failures is collect_checksum_failures. The image has all its checksums
// valid, as all_checksums_valid says, when it returns none.
func (img *evaImage) failures() []string {
	var f []string
	add := func(label string, r evaRecord) {
		if !r.crcValid {
			f = append(f, label+" TI")
		}
		if !r.lzma.dataCRCValid {
			f = append(f, label+" LZMA data")
		}
	}
	switch img.kind {
	case evaSingleKernel:
		add("primary", img.primary)
	case evaSecondaryFragment:
		add("secondary", img.primary)
	default:
		add("primary", img.primary)
		add("secondary", img.secondary)
		if !img.trailerValid {
			f = append(f, "dual trailer")
		}
	}
	if img.fileSignature != nil && !img.fileSignature.valid {
		f = append(f, "file signature")
	}
	return f
}

// evaFileSignatureTable is the table of eva_file_signature_crc32: a CRC-32
// with the polynomial 0x04C11DB7, not reflected.
var evaFileSignatureTable = func() [256]uint32 {
	const poly = 0x04C11DB7
	var t [256]uint32
	for i := 1; i < 256; i++ {
		crc := t[i/2]
		c := crc>>31 ^ uint32(i)&1
		crc <<= 1
		if c&1 != 0 {
			crc ^= poly
		}
		t[i] = crc
	}
	return t
}()

// evaFileSignatureCRC is eva_file_signature_crc32. The CRC takes the length
// of the data after the data, one byte at a time from the lowest.
func evaFileSignatureCRC(data []byte) uint32 {
	var crc uint32
	for _, b := range data {
		crc = crc<<8 ^ evaFileSignatureTable[byte(crc>>24)^b]
	}
	for n := len(data); n > 0; n >>= 8 {
		crc = crc<<8 ^ evaFileSignatureTable[byte(crc>>24)^byte(n)]
	}
	return ^crc
}

// parseEVAImage is parse_eva_image.
func parseEVAImage(data []byte, off int) (evaImage, bool) {
	d := data[off:]
	if len(d) < evaTIHeaderSize {
		return evaImage{}, false
	}
	switch binary.LittleEndian.Uint32(d) {
	case evaDualKernelMagic:
		return parseEVADual(data, off)
	case evaTIAR7Magic:
		return parseEVASingle(data, off, evaTIAR7Magic)
	case evaTIAR72ndMagic:
		return parseEVASingle(data, off, evaTIAR72ndMagic)
	}
	return evaImage{}, false
}

// parseEVADual is parse_dual_kernel_image. The container has a TI header,
// the two records, and a TI trailer whose checksum covers both.
func parseEVADual(data []byte, off int) (evaImage, bool) {
	d := data[off:]
	le := binary.LittleEndian
	payloadLen := le.Uint32(d[4:])
	loadAddr := le.Uint32(d[8:])
	primary, ok := parseEVARecord(d, evaTIHeaderSize, evaTIAR7Magic)
	if !ok {
		return evaImage{}, false
	}
	// The second record starts at the next offset that is a multiple of 4.
	second := (primary.headerOffset + primary.totalSize + 3) &^ 3
	secondary, ok := parseEVARecord(d, second, evaTIAR72ndMagic)
	if !ok {
		return evaImage{}, false
	}
	trailer := evaTIHeaderSize + int(payloadLen)
	if trailer < secondary.headerOffset+secondary.totalSize {
		return evaImage{}, false
	}
	end := trailer + evaTITrailerSize
	if len(d) < end || le.Uint32(d[trailer+4:]) != 0 {
		return evaImage{}, false
	}
	// The checksum covers the bytes from the header to the trailer.
	sum := evaTIChecksum(payloadLen, loadAddr, d[evaTIHeaderSize:trailer])
	img := evaImage{
		kind:          evaDualKernel,
		primary:       primary,
		secondary:     secondary,
		trailerValid:  le.Uint32(d[trailer:]) == sum,
		fileSignature: evaDetectFileSignature(data, off, end),
		totalSize:     end,
	}
	if img.fileSignature != nil {
		img.totalSize = img.fileSignature.endOff
	}
	return img, true
}

// parseEVASingle is parse_single_kernel_image. A fragment of a second
// kernel has no file signature.
func parseEVASingle(data []byte, off int, magic uint32) (evaImage, bool) {
	r, ok := parseEVARecord(data[off:], 0, magic)
	if !ok {
		return evaImage{}, false
	}
	img := evaImage{kind: evaSecondaryFragment, primary: r, totalSize: r.totalSize}
	if magic == evaTIAR7Magic {
		img.kind = evaSingleKernel
		img.fileSignature = evaDetectFileSignature(data, off, r.totalSize)
		if img.fileSignature != nil {
			img.totalSize = img.fileSignature.endOff
		}
	}
	return img, true
}

// parseEVARecord is parse_ti_record: a TI header, the LZMA payload, and a
// TI trailer.
func parseEVARecord(d []byte, off int, magic uint32) (evaRecord, bool) {
	le := binary.LittleEndian
	hdr, ok := binwalk.Range(d, off, off+evaTIHeaderSize)
	if !ok || le.Uint32(hdr) != magic {
		return evaRecord{}, false
	}
	payloadLen := le.Uint32(hdr[4:])
	loadAddr := le.Uint32(hdr[8:])
	start := off + evaTIHeaderSize
	end := start + int(payloadLen)
	trailerEnd := end + evaTITrailerSize
	if len(d) < trailerEnd {
		return evaRecord{}, false
	}
	payload := d[start:end]
	lzma, ok := parseEVALZMA(payload)
	if !ok {
		return evaRecord{}, false
	}
	trailer := d[end:trailerEnd]
	if le.Uint32(trailer[4:]) != 0 {
		return evaRecord{}, false
	}
	return evaRecord{
		loadAddr:     loadAddr,
		crcValid:     le.Uint32(trailer) == evaTIChecksum(payloadLen, loadAddr, payload),
		entryAddr:    le.Uint32(trailer[8:]),
		lzma:         lzma,
		headerOffset: off,
		totalSize:    trailerEnd - off,
	}, true
}

// parseEVALZMA is parse_eva_lzma_payload: a header of 16 bytes, the header
// of an LZMA stream, and the compressed data.
func parseEVALZMA(p []byte) (evaLZMA, bool) {
	le := binary.LittleEndian
	if len(p) < evaLZMAHeaderSize+evaLZMAStreamHeader || le.Uint32(p) != evaLZMATypeMagic {
		return evaLZMA{}, false
	}
	compressed := int(le.Uint32(p[4:]))
	start := evaLZMAHeaderSize + evaLZMAStreamHeader
	if len(p) < start+compressed {
		return evaLZMA{}, false
	}
	return evaLZMA{
		compressedLen:   compressed,
		uncompressedLen: int(le.Uint32(p[8:])),
		dataCRCValid:    binwalk.CRC32(p[start:start+compressed]) == le.Uint32(p[12:]),
		properties:      p[evaLZMAHeaderSize],
		dictSize:        le.Uint32(p[evaLZMAHeaderSize+1:]),
	}, true
}

// evaTIChecksum is calculate_ti_checksum: the negative of the sum of the
// length, the load address and each byte of the payload, in 32 bits.
func evaTIChecksum(payloadLen, loadAddr uint32, payload []byte) uint32 {
	var sum uint32
	for _, b := range payload {
		sum += uint32(b)
	}
	return -(payloadLen + loadAddr + sum)
}

// evaDetectFileSignature is detect_file_signature. It looks for the file
// signature at each multiple of 4 bytes after the end of the image, up to
// 4096 bytes after it.
func evaDetectFileSignature(data []byte, off, end int) *evaFileSignature {
	d := data[off:]
	scanEnd := min(len(d), end+evaFileSignatureScan)
	for c := end; c+evaFileSignatureSize <= scanEnd; c += 4 {
		if s := evaTryFileSignature(data, off, c); s != nil {
			return s
		}
	}
	return nil
}

// evaTryFileSignature is try_file_signature_at. The CRC covers the file
// from its first byte, not from the image, up to the signature.
func evaTryFileSignature(data []byte, off, c int) *evaFileSignature {
	le := binary.LittleEndian
	s, ok := binwalk.Range(data[off:], c, c+evaFileSignatureSize)
	if !ok || le.Uint32(s) != evaFileSigMagic {
		return nil
	}
	crc := le.Uint32(s[4:])
	return &evaFileSignature{
		crc:    crc,
		valid:  crc == evaFileSignatureCRC(data[:off+c]),
		endOff: c + evaFileSignatureSize,
	}
}

// extractEVA is extract_eva. It accepts an image whose checksums are all
// valid. It writes each kernel as a stream of LZMA alone: the properties,
// the size of the dictionary, the uncompressed size in 64 bits, and the
// compressed data. The first kernel goes to kernel.lzma, and the second to
// kernel_2nd.lzma. It does not decompress them.
func extractEVA(data []byte, off int, out *walk.Output) walk.Extraction {
	img, ok := parseEVAImage(data, off)
	if !ok || len(img.failures()) != 0 {
		return walk.Extraction{}
	}
	d := data[off:]
	type file struct {
		name string
		data []byte
	}
	var files []file
	switch img.kind {
	case evaSingleKernel:
		files = []file{{"kernel.lzma", evaLZMAAlone(d, img.primary)}}
	case evaSecondaryFragment:
		files = []file{{"kernel_2nd.lzma", evaLZMAAlone(d, img.primary)}}
	default:
		files = []file{
			{"kernel.lzma", evaLZMAAlone(d, img.primary)},
			{"kernel_2nd.lzma", evaLZMAAlone(d, img.secondary)},
		}
	}
	if out != nil {
		for _, f := range files {
			if !out.CreateFile(f.name, f.data) {
				return walk.Extraction{}
			}
		}
	}
	return walk.Extraction{OK: true, Size: img.totalSize, HasSize: true}
}

// evaLZMAAlone is reconstruct_lzma_alone. The parser checked that the
// compressed data is in the image, so it cannot fail here.
func evaLZMAAlone(d []byte, r evaRecord) []byte {
	start := r.headerOffset + evaTIHeaderSize + evaLZMAHeaderSize + evaLZMAStreamHeader
	b := make([]byte, 0, 13+r.lzma.compressedLen)
	b = append(b, r.lzma.properties)
	b = binary.LittleEndian.AppendUint32(b, r.lzma.dictSize)
	b = binary.LittleEndian.AppendUint64(b, uint64(r.lzma.uncompressedLen))
	return append(b, d[start:start+r.lzma.compressedLen]...)
}
