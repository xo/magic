//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Package exec registers the executables, kernels and boot images that
// binwalk-ng finds: ELF, PE, DirectX shader bytecode, Windows CE images, the
// VxWorks WIND kernel and symbol table, the eCos exception handler, the
// Linux kernel version string, the Linux boot images for x86, ARM and
// ARM64, Android boot images, the U-Boot version string and Fritz!Box EVA
// kernel images.
package exec

import "github.com/xo/magic/walk"

func init() {
	walk.Register(
		LinuxKernel, LinuxBootImage, LinuxARMZImage, ELF, PE, WindKernel,
		VxWorksSymtab, ECos, LinuxARM64BootImage, WinCE, DXBC,
		AndroidBootImg, UBoot, EVA,
	)
}
