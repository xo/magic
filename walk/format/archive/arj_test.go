//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package archive

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"slices"
	"testing"

	"github.com/xo/magic/walk"
)

// arjHeader returns a header of an ARJ archive whose first header is 30
// bytes.
func arjHeader(version, host, flags, method, typ byte, name string, date, csize, osize uint32) []byte {
	first := []byte{30, version, 1, host, flags, method, typ, 0}
	first = binary.LittleEndian.AppendUint32(first, date)
	first = binary.LittleEndian.AppendUint32(first, csize)
	first = binary.LittleEndian.AppendUint32(first, osize)
	first = append(first, make([]byte, 30-len(first))...)
	body := slices.Concat(first, []byte(name), []byte{0, 0})
	b := []byte{0x60, 0xea}
	b = binary.LittleEndian.AppendUint16(b, uint16(len(body)))
	b = slices.Concat(b, body)
	b = binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(body))
	return append(b, 0, 0)
}

// TestARJ holds the descriptions of binwalk-ng, and that only the main
// header is not declined. The descriptions are those that binwalk-ng gives.
func TestARJ(t *testing.T) {
	t.Parallel()
	main := arjHeader(11, 2, 0x10, 0, 2, "archive.arj", 0x5a216000, 0, 0)
	file := arjHeader(11, 0, 0x25, 1, 0, "file.txt", 0xffffffff, 1234, 5678)
	data := slices.Concat(noise(40), main, file)
	var got []string
	for e, err := range walk.WalkBytes(context.Background(), data, walk.WithFormats("arj")) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if e.Format == "arj" {
			got = append(got, fmt.Sprint(e.Offset, " ", e.Size, " ", e.Confidence, " ", e.Extractable(), " ", e.Description))
		}
	}
	check(t, got,
		fmt.Sprint(40, " ", len(main)-2)+" high false ARJ archive data, header size: 51, version 11, minimum version to extract: 1, "+
			"flags: no password|slash-switched, compression method: stored, file type: comment header, "+
			"original name: archive.arj, original file date: 2025-01-01 12:00:00, compressed file size: 0, "+
			"uncompressed file size: 0, os: UNIX",
		fmt.Sprint(40+len(main), " ", len(file)-2)+" high false ARJ archive data, header size: 48, version 11, minimum version to extract: 1, "+
			"flags: password|multi-volume|backup, compression method: compressed most, file type: binary, "+
			"original name: file.txt, original file date: 2106-02-07 06:28:15, compressed file size: 1234, "+
			"uncompressed file size: 5678, os: MS-DOS",
	)
	m, err := parseARJ(data, 40)
	if err != nil || m.Declined {
		t.Errorf("expected %q, got: %q", "not declined", fmt.Sprint(m.Declined, err))
	}
	m, err = parseARJ(data, 40+len(main))
	if err != nil || !m.Declined {
		t.Errorf("expected %q, got: %q", "declined", fmt.Sprint(m.Declined, err))
	}
	for name, b := range map[string][]byte{
		"version":  arjHeader(0, 0, 0, 0, 0, "a", 0, 0, 0),
		"host":     arjHeader(11, 12, 0, 0, 0, "a", 0, 0, 0),
		"method":   arjHeader(11, 0, 0, 5, 0, "a", 0, 0, 0),
		"type":     arjHeader(11, 0, 0, 0, 7, "a", 0, 0, 0),
		"negative": arjHeader(11, 0, 0, 0, 0, "a", 0, 0x80000000, 0),
		"crc":      slices.Concat(arjHeader(11, 0, 0, 0, 0, "a", 0, 0, 0)[:36], []byte{0, 0, 0, 0, 0, 0}),
	} {
		if _, err := parseARJ(b, 0); err == nil {
			t.Errorf("%s: expected %q, got: %q", name, walk.ErrNoMatch, "a match")
		}
	}
}

// TestRAR holds the size and the descriptions of RAR 4 and RAR 5, and of an
// archive whose end binwalk does not find.
func TestRAR(t *testing.T) {
	t.Parallel()
	rar4 := slices.Concat([]byte("Rar!\x1a\x07\x00"), noise(20), rarEOF[4])
	rar5 := slices.Concat([]byte("Rar!\x1a\x07\x01\x00"), noise(20), rarEOF[5])
	for _, c := range []struct {
		data []byte
		exp  string
	}{
		{rar4, fmt.Sprintf("%d medium RAR archive, version: 4, total size: %d bytes", len(rar4), len(rar4))},
		{rar5, fmt.Sprintf("%d medium RAR archive, version: 5, total size: %d bytes", len(rar5), len(rar5))},
		{rar5[:20], "0 low RAR archive, version: 5, total size: 0 bytes (failed to locate RAR EOF)"},
	} {
		m, err := parseRAR(slices.Concat(noise(9), c.data), 9)
		if got := fmt.Sprint(m.Size, " ", m.Confidence, " ", m.Description); err != nil || got != c.exp {
			t.Errorf("expected %q, got: %q", c.exp, got)
		}
	}
	// RAR 1.3 has no marker of the end.
	rar13 := slices.Concat(noise(9), []byte("RE~^"), noise(20))
	m, err := parseRAR(rar13, 9)
	exp := "0 low RAR archive, version: 13, total size: 0 bytes (failed to locate RAR EOF)"
	if got := fmt.Sprint(m.Size, " ", m.Confidence, " ", m.Description); err != nil || got != exp {
		t.Errorf("expected %q, got: %q", exp, got)
	}
	if _, err := parseRAR([]byte("Rar!\x1a\x07\x02"), 0); err == nil {
		t.Errorf("expected %q, got: %q", walk.ErrNoMatch, "a match")
	}
}
