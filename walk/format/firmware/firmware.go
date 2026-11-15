//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Package firmware registers the firmware headers that binwalk finds:
// uImage, TRX, DLOB, PackImg, CHK, SEAMA, CFE, TP-Link, TP-Link RTOS, JBOOT
// ARM, JBOOT STAG, JBOOT SCH2, BIN header and RTK.
//
// binwalk writes a hexadecimal number as {:#X}, which gives "0x" and then
// uppercase digits. The verb %#X of Go gives "0X", so this package writes
// "0x%X".
package firmware

import "github.com/xo/magic/walk"

func init() {
	walk.Register(UImage, TRX, DLOB, PackImg, CHK, SEAMA, CFE, TPLink,
		TPLinkRTOS, JBOOTARM, JBOOTSTAG, JBOOTSCH2, BINHdr, RTK)
}
