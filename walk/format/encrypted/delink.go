//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates the crate delink of the author of binwalk, at commit
// 4735669, which binwalk uses to decrypt the firmware of D-Link: src/aes.rs,
// src/openssl.rs, src/sha1_hmac.rs, src/decryptor.rs, src/r95.rs,
// src/shrs.rs, src/mh01.rs, src/dlk.rs, src/encimg.rs, src/tlv.rs,
// src/dap1610.rs and src/encrpted.rs. delink is under the MIT license.

package encrypted

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"slices"
)

// decrypt is decrypt of decryptor.rs: it tries each decryptor in turn, and
// returns what the first that works gives.
func decrypt(data []byte) ([]byte, bool) {
	for _, d := range []func([]byte) ([]byte, bool){
		decryptR95, decryptSHRS, decryptMH01, decryptDLK, decryptEncimg,
		decryptTLV, decryptDAP1610, decryptEncrpted,
	} {
		if b, ok := d(data); ok {
			return b, true
		}
	}
	return nil, false
}

// aesCBC is aes_cbc_decrypt of aes.rs: AES of the key size, on the first
// size bytes of the key, in CBC mode, with or without the padding of
// PKCS#7. The data must be whole blocks.
func aesCBC(data, key, iv []byte, size int, padded bool) ([]byte, bool) {
	if len(key) < size || len(iv) != aes.BlockSize || len(data)%aes.BlockSize != 0 {
		return nil, false
	}
	if padded && len(data) == 0 {
		return nil, false
	}
	block, err := aes.NewCipher(key[:size])
	if err != nil {
		return nil, false
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	if !padded {
		return out, true
	}
	n := int(out[len(out)-1])
	if n == 0 || n > aes.BlockSize {
		return nil, false
	}
	for _, c := range out[len(out)-n:] {
		if int(c) != n {
			return nil, false
		}
	}
	return out[:len(out)-n], true
}

// openSSL is decrypt of openssl.rs: data that starts with "Salted__" and a
// salt, whose key and IV come from the password and the salt, as
// EVP_BytesToKey of OpenSSL makes them, with one round of the digest. An IV
// that is not nil takes the place of the one that it makes.
func openSSL(data []byte, password string, size int, newHash func() hash.Hash, iv []byte) ([]byte, bool) {
	if len(data) < 16 || string(data[:8]) != "Salted__" {
		return nil, false
	}
	passSalt := append([]byte(password), data[8:16]...)
	sum := func(b []byte) []byte {
		h := newHash()
		h.Write(b)
		return h.Sum(nil)
	}
	h := sum(passSalt)
	material := slices.Clone(h)
	for len(material) < 48 {
		h = sum(append(h, passSalt...))
		material = append(material, h...)
	}
	if iv == nil {
		iv = material[32:48]
	}
	return aesCBC(data[16:], material[:32], iv, size, true)
}

// decryptR95 is decrypt of r95.rs.
func decryptR95(data []byte) ([]byte, bool) {
	off := 0
	if len(data) >= 520 && string(data[512:520]) == "Salted__" {
		off = 512
	}
	for _, pw := range []string{
		"CAD1C42B11F1982FFA94B6A24C260A43", "A11E331C15CE73ABA8E06171A11D2FB6",
		"BE81AE1B6F523AC7164C4FD67B6BD8FD", "91A9A3AF2218F4EA60AC37D5835EB318",
	} {
		if b, ok := openSSL(data[off:], pw, 32, sha256.New, nil); ok && bytes.HasPrefix(b, []byte("\xd0\x0d\xfe\xed")) {
			return b, true
		}
	}
	return nil, false
}

// decryptSHRS is decrypt of shrs.rs.
func decryptSHRS(data []byte) ([]byte, bool) {
	const start = 0x6DC
	key := []byte("\xC0\x5F\xBF\x19\x36\xC9\x94\x29\xCE\x2A\x07\x81\xF0\x8D\x6A\xD8")
	if len(data) < 0x1C || string(data[:4]) != "SHRS" {
		return nil, false
	}
	n := int(binary.BigEndian.Uint32(data[8:12]))
	if start+n > len(data) {
		return nil, false
	}
	b, ok := aesCBC(data[start:start+n], key, data[0x0C:0x1C], 16, false)
	if !ok || !bytes.HasPrefix(b, []byte("\x27\x05\x19\x56")) {
		return nil, false
	}
	return b, true
}

// dap1610 is decrypt of dap1610.rs.
func decryptDAP1610(data []byte) ([]byte, bool) {
	b, ok := openSSL(data, "2c3b6fa78bd60b41bb0796fef4b058b0", 32, sha256.New, nil)
	if !ok || len(b) < 0x106 || string(b[0x101:0x106]) != "ustar" {
		return nil, false
	}
	return b, true
}

// decryptEncrpted is decrypt of encrpted.rs: blocks of 128 KiB, each
// decrypted on its own with the same key and IV.
func decryptEncrpted(data []byte) ([]byte, bool) {
	const blockSize = 131072
	iv := []byte("\x4a\x25\x31\x69\x51\x6c\x38\x24\x3d\x6c\x6d\x2d\x3b\x38\x41\x45")
	key := []byte("he9-4+M!)d6=m~we1,q2a3d1n&2*Z^%8")
	if !bytes.HasPrefix(data, []byte("encrpted_img")) || len(data) < 16 {
		return nil, false
	}
	var out []byte
	for c := range slices.Chunk(data[16:], blockSize) {
		b, ok := aesCBC(c, key, iv, 32, false)
		if !ok {
			return nil, false
		}
		out = append(out, b...)
	}
	if !bytes.HasPrefix(out, []byte("UBI")) {
		return nil, false
	}
	return out, true
}

// decryptTLV is decrypt of tlv.rs: the key is the HMAC-SHA1, in hexadecimal,
// of the model name with the board ID as its key.
func decryptTLV(data []byte) ([]byte, bool) {
	const headerSize = 0x74
	if len(data) < headerSize || !bytes.HasPrefix(data, []byte("\x64\x80\x19\x40")) {
		return nil, false
	}
	cstr := func(b []byte) []byte {
		if i := bytes.IndexByte(b, 0); i >= 0 {
			return b[:i]
		}
		return b
	}
	model, board := cstr(data[4:0x24]), cstr(data[0x24:0x44])
	mac := hmac.New(sha1.New, board)
	mac.Write(model)
	return openSSL(data[headerSize:], hex.EncodeToString(mac.Sum(nil)), 32, md5.New, nil)
}

// mh01Keys are known_keys of mh01.rs: the password of each model, in the
// order of their names, as mh01 sorts them.
var mh01Keys = []struct{ name, key string }{
	{"E15 > v1.20", "927fc5786df1a9557524a0289e1e3f3b"},
	{"E15 v1.00 - v1.20", "044b4e59846ecee953662ff2238fcc23"},
	{"E30 > v1.10", "238a29b9432f688e30b701548c753146"},
	{"E30 v1.00 - v1.10", "4d5ee2c8b5d0fdd9a9a2d351ba897752"},
	{"M18", "1ae6c79be7d069ca74df7670bdfc4952"},
	{"M30 > v1.10", "05c79b73cf88619d7b9725505cfd718f"},
	{"M30 v1.02 - v1.10", "b4517d9b98e04d9f075f5e78c743e097"},
	{"M32 > v1.10", "1bfb1004e29f9eb76dbe26eb0dd87cd1"},
	{"M32 v1.00 - 1.10", "6b29f1d663a21b35fb45b69a42649f5e"},
	{"M60 > v1.10", "6420da70a975455e4ddd6b8fa5b652e7"},
	{"M60 v1.10", "c5f8a1e22f808abc84f2e4a6fa5f10bb"},
	{"R12 > v1.10", "6b5a65dbc1ebc492ac6d8efbbb59ae09"},
	{"R12 v1.00 - v1.10", "a4f7c17c3e0aa4532c2024ce6ac5f17c"},
	{"R15 > v1.20", "7b4df82f7f042b9d0b40971be0ff53c4"},
	{"R15 v1.00 - v1.20", "70070e579f97548a96a7794d4d779376"},
	{"R18", "6276ccf4c1d8d6f54b481095e78ff97f"},
}

// decryptMH01 is decrypt of mh01.rs.
func decryptMH01(data []byte) ([]byte, bool) {
	const headerSize = 0x41
	if len(data) < 64 || string(data[:4]) != "MH01" {
		return nil, false
	}
	n := int(binary.LittleEndian.Uint32(data[0x18:0x1C]))
	iv, err := hex.DecodeString(string(data[32:64]))
	if err != nil || headerSize+n > len(data) {
		return nil, false
	}
	for _, k := range mh01Keys {
		if b, ok := openSSL(data[headerSize:headerSize+n], k.key, 16, sha256.New, iv); ok && bytes.HasPrefix(b, []byte("MH01")) {
			return b, true
		}
	}
	return nil, false
}

// decryptDLK is decrypt of dlk.rs: a second header after the signature,
// then blocks that each start with their IV. delink tries the keys of MH01
// in the order of a HashMap of Rust, which is random. The walker tries them
// in the order of their names. A wrong key fails its padding in almost
// every case, so the order changes no answer.
func decryptDLK(data []byte) ([]byte, bool) {
	const (
		headerSize = 0x50
		ivSize     = 16
	)
	if len(data) < 0x30 || string(data[:3]) != "DLK" {
		return nil, false
	}
	next := headerSize + int(binary.LittleEndian.Uint32(data[0x2C:0x30]))
	if next+headerSize > len(data) || string(data[next:next+3]) != "DLK" {
		return nil, false
	}
	blockSize := int(binary.LittleEndian.Uint32(data[next+0x10:])) + 0x20
	total := int(binary.LittleEndian.Uint32(data[next+0x2C:]))
	start := next + headerSize
	if start+total > len(data) || blockSize <= 0 {
		return nil, false
	}
	blocks := data[start : start+total]
	for _, k := range mh01Keys {
		var out []byte
		done := 0
		for c := range slices.Chunk(blocks, blockSize) {
			if len(c) <= ivSize {
				break
			}
			b, ok := aesCBC(c[ivSize:], []byte(k.key), c[:ivSize], 32, true)
			if !ok {
				break
			}
			done += len(c)
			out = append(out, b...)
		}
		if done == total {
			return out, true
		}
	}
	return nil, false
}

// encimgSigns are the image signatures of the firmware that encimg.rs
// knows. The last reads its signature from the data, at offset 0.
var encimgSigns = []struct {
	sign   string
	fromAt bool
	start  int
}{
	{sign: "wapac25_dlink.2015_dap1665"},
	{sign: "wrgac43s_dlink.2015_dir822c1"},
	{sign: "wrgac65_dlink.2015_dir842"},
	{sign: "wrgac05_dlob.hans_dir850l"},
	{sign: "wrgac25_dlink.2013gui_dir850l"},
	{fromAt: true, start: 0xA0},
}

// encimgBoards are the prog_board_fws of encimg.rs, from which keygen makes
// the keys and the IVs.
var encimgBoards = []string{
	"5gHW13MScSB4Xqqr8Mg8xl0zlQXCfykXEfCHXytwsC6F0zsedwZc+9vDbCjE3ge4Ts0682B35XQG\nP2tuxxuLMlvCJ266ZlnggPy917jwESpnfXmMiZRNcSviifjxTlg",
	"vzoLuJSCIFc3UwLZ6Is4Tyu95dFg9MssBIuS1CVMEQG+0pUeE99jnR+vLlLd9unrlvhwEvRdn99R\nEYmbe6y0HeABq/NtIXwf3+odwHhmJL1ceW16UsU3xgR7QH0CO9c",
	"k5NI1+bvWEfZ6ohtpUOwynOdUcivqwEZqQehHMEmEPQ5izL+cabn8bNHZXHjkp6WCl9yn9CIkiI1\nmTFu21TEEPo66JBFv9BMmb+IKQgnO8OuF4bz4frGPdN67gYLuOs",
	"db6zOuf7GJWGI64bm0DXpZ1rn4hFmPTxoVhq0hvXHdfaGFLdubM4/QvuVHdKee7vh6tC/sBL2t8h\n9GtlNghPDnf9wPrYOLk0BO5nlYankuVBe4sWaltHEHh7NToCSdq",
	"2q02Oz+DDDKjLmMENiZN+3M8VucG4rYfKNpsEntCcsep1jdFIs3wnXySKRGNCGmfzYHzJEPD3GbX\ne/AF4zbvpjuPlmq58fHuph587JdKHrtAUlrli4/FkiKXBfDFbn2",
	"XYWFilP+ZyydvsXAJSgKeF/p15q05g68xQYoRZeD726UAbRb846kO7TeNw8eZa6ucKxYrhxNbzjP\nbpgFJ7Yxa6sBeujdJ7fzufEbNF3kUafxFiESBRQI6qQbszYOvJI",
}

// encimgXOR is encrypt_xor of encimg.rs.
func encimgXOR(sign, data []byte) []byte {
	out := make([]byte, len(data))
	x, j := byte(1), 0
	for i, c := range data {
		out[i] = c ^ x ^ sign[j]
		if x++; x > 0xFB {
			x = 0
		}
		if j++; j >= len(sign) {
			j = 0
		}
	}
	return out
}

// decryptEncimg is decrypt of encimg.rs.
func decryptEncimg(data []byte) ([]byte, bool) {
	magics := []string{"\x5E\xA3\xA4\x17", "\xD0\x0D\xFE\xED", "\x5D\x00\x00\x80"}
	for _, fw := range encimgSigns {
		sign := []byte(fw.sign)
		if fw.fromAt {
			sign = data
			if i := bytes.IndexByte(sign, 0); i >= 0 {
				sign = sign[:i]
			}
		}
		if len(sign) == 0 {
			// delink stops here, and tries no other firmware.
			return nil, false
		}
		if fw.start > len(data) {
			continue
		}
		enc := data[fw.start:]
		for _, board := range encimgBoards {
			key := encimgXOR(sign, []byte(board[0x20:0x40]))
			iv := encimgXOR(sign, []byte(board[0x60:0x70]))
			b, ok := aesCBC(enc, key, iv, 32, false)
			if ok && len(b) >= 4 && slices.Contains(magics, string(b[:4])) {
				return b, true
			}
		}
	}
	return nil, false
}
