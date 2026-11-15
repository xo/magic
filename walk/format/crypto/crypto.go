//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Package crypto registers the keys, certificates and tables of cryptography
// that binwalk finds: PEM certificates and keys, RSA session keys, PKCS DER
// hashes, DPAPI blobs, LUKS headers, the tables of AES, and the constants of
// CRC32, SHA256 and MD5.
package crypto

import "github.com/xo/magic/walk"

func init() {
	walk.Register(
		CRC32, SHA256, PEMCertificate, PEMPublicKey, PEMPrivateKey, RSA,
		AESSBox, AESForwardTable, AESReverseTable, AESRCON,
		AESAccelerationTable, LUKS, PKCSDERHash, DPAPI, MD5,
	)
}
