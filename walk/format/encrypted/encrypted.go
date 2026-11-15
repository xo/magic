//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the MIT license, in LICENSE.
//
// This file translates the extractors of src/formats/encfw.rs,
// src/formats/mh01.rs and src/formats/dlink_fw.rs of binwalk-ng.

// Package encrypted decrypts the firmware images of D-Link that binwalk
// decrypts, with the keys that binwalk carries in the crate delink (D51). It
// adds the extractors of binwalk to the signatures of the package vendor:
// encfw, encrpted_img, dlink_tlv, dlke, shrs, dkbs and openssl, which binwalk
// only decrypts, dlink_fw, which binwalk-ng decrypts and strips of its
// header, and mh01, which binwalk decrypts, or else carves in parts.
//
// A program that imports vendor and not this package finds those images and
// carries no key. One that imports this package decrypts them:
//
//	import _ "github.com/xo/magic/walk/format/encrypted"
package encrypted

import (
	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/format/vendor"
)

func init() {
	for _, s := range []*walk.Signature{
		vendor.EncFW, vendor.EncrptedImg, vendor.DLinkTLV, vendor.DLKE,
		vendor.SHRS, vendor.DKBS, vendor.OpenSSL,
	} {
		s.Extract = decryptEncFW
	}
	vendor.DLinkFW.Extract = decryptDLinkFW
	carve := vendor.MH01.Extract
	vendor.MH01.Extract = func(data []byte, off int, out *walk.Output) walk.Extraction {
		return extractMH01(data, off, out, carve)
	}
}

// decryptEncFW is encfw_decrypt: it tries each decryptor of delink on the
// data from off, and writes what the first that works gives to
// decrypted.bin.
func decryptEncFW(data []byte, off int, out *walk.Output) walk.Extraction {
	b, ok := decrypt(data[off:])
	if !ok {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: out == nil || out.CreateFile("decrypted.bin", b)}
}

// extractMH01 is extract_mh01_image: it decrypts the image with the keys of
// MH01 to decrypted.bin, or else carves its parts, as the extractor of
// vendor does.
func extractMH01(data []byte, off int, out *walk.Output, carve walk.Extractor) walk.Extraction {
	r := carve(data, off, nil)
	if !r.OK || out == nil {
		return r
	}
	if b, ok := decryptMH01(data[off:]); ok {
		r.OK = out.CreateFile("decrypted.bin", b)
		return r
	}
	return carve(data, off, out)
}

// decryptDLinkFW is dlink_fw_decrypt: it tries each decryptor of delink on
// the data from off, as decryptEncFW does. The decryptor of DAP-1325 puts
// the header of 64 bytes in plain text before the decrypted data, and
// binwalk-ng removes it, so that the walk of decrypted.bin starts at the
// uImage and does not find dlink_fw again.
func decryptDLinkFW(data []byte, off int, out *walk.Output) walk.Extraction {
	const headerSize = 64
	b, ok := decrypt(data[off:])
	if !ok {
		return walk.Extraction{}
	}
	if len(b) > headerSize {
		b = b[headerSize:]
	}
	return walk.Extraction{OK: out == nil || out.CreateFile("decrypted.bin", b)}
}
