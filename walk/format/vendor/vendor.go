//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Package vendor registers the firmware images of vendors, most of them
// encrypted or obfuscated, that binwalk finds: OpenSSL encryption, Arcadyan
// obfuscated LZMA, D-Link MH01, Autel, DMS, Matter OTA, Dahua ZIP, CSman DAT,
// known encrypted firmware, D-Link encrpted_img, D-Link TLV, DLKE, SHRS and
// DKBS.
//
// The package carries no key. It extracts the formats that binwalk extracts
// without a key. The package encrypted adds the decryption of the others,
// with the keys of binwalk (D51).
package vendor

import "github.com/xo/magic/walk"

func init() {
	walk.Register(
		Arcadyan, OpenSSL, Autel, DahuaZIP, MH01, CSman, DLinkTLV, DLKE, SHRS,
		EncrptedImg, DMS, DKBS, EncFW, MatterOTA,
	)
}
