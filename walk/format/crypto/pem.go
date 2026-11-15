//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/pem.rs and src/extractors/pem.rs of
// binwalk.

package crypto

import (
	"bytes"
	"encoding/base64"
	"strings"
	"unicode/utf8"

	"github.com/xo/magic/walk"
)

// PEMCertificate is the signature "pem_certificate" of binwalk.
var PEMCertificate = &walk.Signature{
	Name:        "pem_certificate",
	Magic:       pemCertificateMagic,
	Description: "PEM certificate",
	Parse:       parsePEM,
	Extract:     extractPEMCertificate,
	NoRecurse:   true,
}

// PEMPublicKey is the signature "pem_public_key" of binwalk.
var PEMPublicKey = &walk.Signature{
	Name:        "pem_public_key",
	Magic:       pemPublicKeyMagic,
	Description: "PEM public key",
	Parse:       parsePEM,
	Extract:     extractPEMKey,
	NoRecurse:   true,
}

// PEMPrivateKey is the signature "pem_private_key" of binwalk.
var PEMPrivateKey = &walk.Signature{
	Name:        "pem_private_key",
	Magic:       pemPrivateKeyMagic,
	Description: "PEM private key",
	Parse:       parsePEM,
	Extract:     extractPEMKey,
	NoRecurse:   true,
}

// extractPEMCertificate is pem_certificate_carver: it carves the PEM to
// pem.crt.
func extractPEMCertificate(data []byte, off int, out *walk.Output) walk.Extraction {
	return extractPEM(data, off, out, "pem.crt")
}

// extractPEMKey is pem_key_carver: it carves the PEM to pem.key.
func extractPEMKey(data []byte, off int, out *walk.Output) walk.Extraction {
	return extractPEM(data, off, out, "pem.key")
}

// extractPEM is pem_carver: it carves the PEM at off, up to its end marker,
// to the file name.
func extractPEM(data []byte, off int, out *walk.Output, name string) walk.Extraction {
	size, ok := pemSize(data, off)
	if !ok {
		return walk.Extraction{}
	}
	return walk.Extraction{OK: out == nil || out.CarveFile(name, data, off, size), Size: size, HasSize: true}
}

var pemPublicKeyMagic = [][]byte{
	[]byte("-----BEGIN PUBLIC KEY-----"),
	[]byte("-----BEGIN RSA PUBLIC KEY-----"),
	[]byte("-----BEGIN DSA PUBLIC KEY-----"),
	[]byte("-----BEGIN ECDSA PUBLIC KEY-----"),
}

var pemPrivateKeyMagic = [][]byte{
	[]byte("-----BEGIN PRIVATE KEY-----"),
	[]byte("-----BEGIN EC PRIVATE KEY-----"),
	[]byte("-----BEGIN RSA PRIVATE KEY-----"),
	[]byte("-----BEGIN DSA PRIVATE KEY-----"),
	[]byte("-----BEGIN OPENSSH PRIVATE KEY-----"),
	[]byte("-----BEGIN ANY PRIVATE KEY-----"),
	[]byte("-----BEGIN ENCRYPTED PRIVATE KEY-----"),
	[]byte("-----BEGIN TSS2 PRIVATE KEY-----"),
}

var pemCertificateMagic = [][]byte{
	[]byte("-----BEGIN CERTIFICATE-----"),
}

// pemMinLen is MIN_PEM_LEN: enough bytes of a magic to tell a certificate,
// a public key and a private key apart.
const pemMinLen = 26

// pemHasPrefix reports whether one of the magics starts with the first
// pemMinLen bytes of b.
func pemHasPrefix(magics [][]byte, b []byte) bool {
	for _, m := range magics {
		if bytes.Equal(m[:pemMinLen], b) {
			return true
		}
	}
	return false
}

// parsePEM is pem_parser. The description depends on the bytes at off, not
// on the signature that found the candidate.
func parsePEM(data []byte, off int) (walk.Match, error) {
	if off+pemMinLen > len(data) {
		return walk.Match{}, walk.ErrNoMatch
	}
	m := walk.Match{Offset: off, Confidence: walk.High}
	b := data[off : off+pemMinLen]
	switch {
	case pemHasPrefix(pemPublicKeyMagic, b):
		m.Description = "PEM public key"
	case pemHasPrefix(pemPrivateKeyMagic, b):
		m.Description = "PEM private key"
	case pemHasPrefix(pemCertificateMagic, b):
		m.Description = "PEM certificate"
	default:
		return walk.Match{}, walk.ErrNoMatch
	}
	r := extractPEM(data, off, nil, "")
	if !r.OK || !r.HasSize || !pemDecodes(data[off:off+r.Size]) {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := r.Size
	m.Size = size
	m.Declined = off == 0 && size == len(data)
	return m, nil
}

// pemEnds are the end markers that get_pem_size looks for.
var pemEnds = [][]byte{
	[]byte("-----END PUBLIC KEY-----"),
	[]byte("-----END CERTIFICATE-----"),
	[]byte("-----END PRIVATE KEY-----"),
	[]byte("-----END EC PRIVATE KEY-----"),
	[]byte("-----END RSA PRIVATE KEY-----"),
	[]byte("-----END DSA PRIVATE KEY-----"),
	[]byte("-----END OPENSSH PRIVATE KEY-----"),
}

// pemSize is get_pem_size of extractors/pem.rs: the bytes from off to the end
// of the first end marker, with the CR and LF bytes that follow it. binwalk
// takes the first marker that ends. No marker can start inside another, so
// the first that ends is also the first that starts.
func pemSize(data []byte, off int) (int, bool) {
	rest := data[off:]
	first, end := -1, 0
	for _, e := range pemEnds {
		if i := bytes.Index(rest, e); i >= 0 && (first < 0 || i < first) {
			first, end = i, i+len(e)
		}
	}
	if first < 0 {
		return 0, false
	}
	for end < len(rest) && (rest[end] == '\r' || rest[end] == '\n') {
		end++
	}
	return end, true
}

// pemDecodes is decode_pem_data: it reports whether the lines between the
// first two lines that start with "--" are valid base64.
func pemDecodes(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	var sb strings.Builder
	delims := 0
	for _, line := range rustLines(string(b)) {
		if strings.HasPrefix(line, "--") {
			delims++
			if delims == 2 {
				break
			}
			continue
		}
		sb.WriteString(line)
	}
	s := sb.String()
	// The base64 crate of Rust rejects a CR or LF in its input, and Go
	// skips them, so reject them here.
	if s == "" || strings.ContainsAny(s, "\r\n") {
		return false
	}
	_, err := base64.StdEncoding.Strict().DecodeString(s)
	return err == nil
}

// rustLines is str::lines of Rust: the lines of s, split at LF, each without
// the CR before its LF. A last line with no LF counts, and an empty end does
// not.
func rustLines(s string) []string {
	var lines []string
	for s != "" {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			lines = append(lines, s)
			break
		}
		lines = append(lines, strings.TrimSuffix(s[:i], "\r"))
		s = s[i+1:]
	}
	return lines
}
