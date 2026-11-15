//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Package doc registers the documents and the text that binwalk finds: PDF,
// HP PJL, copyright text, Pcap-NG captures and Motorola S-records.
package doc

import "github.com/xo/magic/walk"

func init() {
	walk.Register(PDF, SRecord, SRecordGeneric, Copyright, PJL, PcapNG)
}
