//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Package all imports every format of the walker. A program that walks for
// every format that binwalk finds imports it:
//
//	import _ "github.com/xo/magic/walk/format/all"
package all

import (
	_ "github.com/xo/magic/walk/format/archive"    // tarball, cpio, zip
	_ "github.com/xo/magic/walk/format/compressed" // gzip, zlib, bzip2, xz, lzma, zstd, gpg_signed
	_ "github.com/xo/magic/walk/format/crypto"     // pem, rsa, pkcs_der_hash, dpapi, luks, aes, crc32, sha256, md5
	_ "github.com/xo/magic/walk/format/disk"       // mbr, efigpt, fat, ntfs, ext, btrfs, apfs, logfs, qcow, dmg, android_sparse
	_ "github.com/xo/magic/walk/format/doc"        // pdf, pjl, copyright, pcapng, srecord, srecord_generic
	_ "github.com/xo/magic/walk/format/exec"       // elf, pe, dxbc, wince, vxworks_symtab, ecos, wind_kernel, linux_kernel, linux_boot_image, linux_arm_zimage, linux_arm64_boot_image, android_bootimg, uboot
	_ "github.com/xo/magic/walk/format/filesystem" // romfs
	_ "github.com/xo/magic/walk/format/firmware"   // uimage, trx, dlob, packimg, chk, seama, cfe, tplink, tplink_rtos, jboot_arm, jboot_stag, jboot_sch2, binhdr, rtk
	_ "github.com/xo/magic/walk/format/image"      // bmp, png, jpeg, riff, gif, svg
	_ "github.com/xo/magic/walk/format/vendor"     // openssl, mh01, autel, dms, matter_ota, dahua_zip, encfw, encrpted_img, dlink_tlv, dlke, shrs, dkbs
)
