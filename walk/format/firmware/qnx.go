//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates src/formats/qnx.rs of binwalk-ng, and the parsers of
// headers and directory entries in src/startup.rs and src/image.rs of the
// crate qnx-ifs 0.2.0 of binwalk-ng, which qnx.rs calls.

package firmware

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// QNXIFS is the signature "qnx_ifs" of binwalk-ng. binwalk-ng extracts it
// with the crate qnx-ifs, which can decompress. The walker finds the image
// and does not extract it.
var QNXIFS = &walk.Signature{
	Name: "qnx_ifs",
	// The magic of the startup header with a version of 1, in both orders
	// of bytes, and the signature of a bare image file system.
	Magic: [][]byte{
		[]byte("\xEB\x7E\xFF\x00\x01\x00"),
		[]byte("\x00\xFF\x7E\xEB\x00\x01"),
		[]byte(qnxImageSignature),
	},
	Description: "QNX IFS image",
	Parse:       parseQNXIFS,
}

// qnxImageSignature is IMAGE_SIGNATURE of qnx-ifs.
const qnxImageSignature = "imagefs"

// qnxEndian gives the order of bytes and its name, as Endianness of
// binwalk-ng shows it.
func qnxEndian(big bool) (binary.ByteOrder, string) {
	if big {
		return binary.BigEndian, "Big Endian"
	}
	return binary.LittleEndian, "Little Endian"
}

// parseQNXIFS is qnx_ifs_parser. A bare image file system is not
// compressed, so the parser reads all of its directory entries. For a
// bootable image, the startup header is enough, because the file system
// can be compressed.
func parseQNXIFS(data []byte, off int) (walk.Match, error) {
	d := data[off:]
	if bytes.HasPrefix(d, []byte(qnxImageSignature)) {
		h, ok := qnxImageFS(d)
		if !ok {
			return walk.Match{}, walk.ErrNoMatch
		}
		return walk.Match{
			Offset:     off,
			Size:       h.imageSize,
			Confidence: walk.High,
			Description: fmt.Sprintf("QNX IFS image, %s, mountpoint: %s, total size: %d bytes",
				h.endian, h.mountpoint, h.imageSize),
		}, nil
	}
	h, ok := qnxStartupHeader(d)
	if !ok || h.storedSize == 0 || h.storedSize > len(d) {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Size:       h.storedSize,
		Confidence: walk.High,
		Description: fmt.Sprintf("QNX IFS image, %s, compression: %s, total size: %d bytes",
			h.endian, h.compression, h.storedSize),
	}, nil
}

type qnxStartup struct {
	endian      string
	compression string
	storedSize  int
}

// qnxStartupHeader is parse_startup_header of qnx-ifs, with lenient false.
// The header has 64 bytes. Bit 1 of flags1 selects big-endian order for the
// fields of more than one byte.
func qnxStartupHeader(d []byte) (qnxStartup, bool) {
	const (
		headerSize  = 64
		zerosOffset = 50
	)
	if len(d) < headerSize {
		return qnxStartup{}, false
	}
	if sig := string(d[:4]); sig != "\xEB\x7E\xFF\x00" && sig != "\x00\xFF\x7E\xEB" {
		return qnxStartup{}, false
	}
	flags1, flags2 := d[6], d[7]
	order, endian := qnxEndian(flags1&0x02 != 0)
	if order.Uint16(d[4:]) != 1 || flags2 != 0 {
		return qnxStartup{}, false
	}
	for _, b := range d[zerosOffset:headerSize] {
		if b != 0 {
			return qnxStartup{}, false
		}
	}
	hsize := int(order.Uint16(d[8:]))
	startupSize := int(order.Uint32(d[32:]))
	storedSize := int(order.Uint32(d[36:]))
	if hsize < headerSize || startupSize < hsize || storedSize < startupSize {
		return qnxStartup{}, false
	}
	var compression string
	switch flags1 & 0x1C {
	case 0x00:
		compression = "none"
	case 0x04:
		compression = "zlib"
	case 0x08:
		compression = "lzo"
	case 0x0C:
		compression = "ucl"
	default:
		return qnxStartup{}, false
	}
	return qnxStartup{endian: endian, compression: compression, storedSize: storedSize}, true
}

type qnxImage struct {
	endian     string
	mountpoint string
	imageSize  int
}

// The sizes of the structures of image.rs of qnx-ifs.
const (
	qnxImageHeaderSize    = 88
	qnxAttrSize           = 24
	qnxFileDirentFixed    = qnxAttrSize + 8
	qnxSymlinkDirentFixed = qnxAttrSize + 4
	qnxDeviceDirentFixed  = qnxAttrSize + 8
	qnxMaxEntries         = 1_000_000
	qnxMaxMountpoint      = 4096
)

// The types of file of the mode of a directory entry.
const (
	qnxIFMT   = 0o170000
	qnxIFIFO  = 0o010000
	qnxIFCHR  = 0o020000
	qnxIFDIR  = 0o040000
	qnxIFNAM  = 0o050000
	qnxIFBLK  = 0o060000
	qnxIFREG  = 0o100000
	qnxIFLNK  = 0o120000
	qnxIFSOCK = 0o140000
)

// qnxImageFS is Image::parse_imagefs_at of qnx-ifs at offset 0: the header of
// the file system, then its directory entries, which must end with an entry
// of size 0. It reads no data of a file.
func qnxImageFS(d []byte) (qnxImage, bool) {
	// parse_image_header. Bit 0 of the flags selects big-endian order.
	if len(d) < qnxImageHeaderSize {
		return qnxImage{}, false
	}
	order, endian := qnxEndian(d[7]&0x01 != 0)
	imageSize := int(order.Uint32(d[8:]))
	hdrDirSize := int(order.Uint32(d[12:]))
	dirOffset := int(order.Uint32(d[16:]))
	// validate_image_header
	if dirOffset < qnxImageHeaderSize || dirOffset > hdrDirSize ||
		imageSize < hdrDirSize+4 || imageSize > len(d) {
		return qnxImage{}, false
	}
	raw := binwalk.CStringBytes(d[qnxImageHeaderSize:min(dirOffset, len(d))])
	if len(raw) > qnxMaxMountpoint {
		return qnxImage{}, false
	}
	if !qnxDirents(d, dirOffset, hdrDirSize, imageSize, order) {
		return qnxImage{}, false
	}
	return qnxImage{endian: endian, mountpoint: binwalk.Lossy(raw), imageSize: imageSize}, true
}

// qnxDirents is parse_dirents_bounded of qnx-ifs, with a base of 0. It
// reports whether the entries from pos are valid, up to the entry of size 0
// that ends them.
func qnxDirents(d []byte, pos, dirEnd, dataEnd int, order binary.ByteOrder) bool {
	entries := 0
	for {
		// Two bytes are enough for the size of 0 at the end.
		if pos+2 > dirEnd || pos+2 > dataEnd {
			return false
		}
		size := int(order.Uint16(d[pos:]))
		if size == 0 {
			return true
		}
		end := pos + size
		if size < qnxAttrSize || end > dirEnd || end > dataEnd {
			return false
		}
		dirent := d[pos:end]
		// The kernel skips an entry with an inode of 0, so the crate does
		// too.
		if order.Uint32(dirent[4:]) == 0 {
			pos = end
			continue
		}
		if !qnxDirent(dirent, dataEnd, order) {
			return false
		}
		if entries >= qnxMaxEntries {
			return false
		}
		entries++
		pos = end
	}
}

// qnxDirent checks one entry, as parse_file_entry, parse_dir_entry,
// parse_symlink_entry and parse_device_entry of qnx-ifs do.
func qnxDirent(dirent []byte, dataEnd int, order binary.ByteOrder) bool {
	switch order.Uint32(dirent[8:]) & qnxIFMT {
	case qnxIFREG:
		// The data of the file is in the image.
		if len(dirent) < qnxFileDirentFixed {
			return false
		}
		offset := int(order.Uint32(dirent[qnxAttrSize:]))
		size := int(order.Uint32(dirent[qnxAttrSize+4:]))
		return offset+size <= dataEnd
	case qnxIFDIR:
		return true
	case qnxIFLNK:
		if len(dirent) < qnxSymlinkDirentFixed {
			return false
		}
		symOffset := int(order.Uint16(dirent[qnxAttrSize:]))
		symSize := int(order.Uint16(dirent[qnxAttrSize+2:]))
		pathEnd := qnxSymlinkDirentFixed + symOffset
		if pathEnd > len(dirent) {
			return false
		}
		return symSize == 0 || pathEnd+symSize <= len(dirent)
	case qnxIFCHR, qnxIFBLK, qnxIFIFO, qnxIFNAM, qnxIFSOCK:
		return len(dirent) >= qnxDeviceDirentFixed
	}
	return false
}
