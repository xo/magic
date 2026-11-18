package adc

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// update writes the file of testdata again, from the compressor of the tests.
var update = flag.Bool("update", false, "write the file of testdata again")

// The disk of testdata/sample.dmg: 400 sectors of 512 bytes, in chunks of 64
// sectors.
const (
	sectorSize   = 512
	diskSectors  = 400
	chunkSectors = 64
)

// The types of chunk of the table of blocks of a DMG file.
const (
	chunkZero = 0x00000000
	chunkRaw  = 0x00000001
	chunkADC  = 0x80000004
	chunkEnd  = 0xFFFFFFFF
)

var be = binary.BigEndian

// disk returns the bytes of the disk of testdata/sample.dmg. Its third chunk
// is all zeros.
func disk() []byte {
	b := sample(diskSectors*sectorSize, 5)
	clear(b[2*chunkSectors*sectorSize : 3*chunkSectors*sectorSize])
	return b
}

// dmg returns a DMG file, a UDIF image, that holds data in one table of
// blocks. A chunk of zeros is a chunk with no data. The second chunk is raw,
// and the others are ADC.
func dmg(data []byte) []byte {
	var fork, chunks []byte
	n := 0
	for sector := 0; sector*sectorSize < len(data); sector += chunkSectors {
		count := min(chunkSectors, len(data)/sectorSize-sector)
		part := data[sector*sectorSize : (sector+count)*sectorSize]
		typ := uint32(chunkADC)
		var enc []byte
		switch {
		case bytes.Count(part, []byte{0}) == len(part):
			typ = chunkZero
		case n == 1:
			typ, enc = chunkRaw, part
		default:
			enc = compress(part)
		}
		chunks = be.AppendUint32(chunks, typ)
		chunks = be.AppendUint32(chunks, 0)
		chunks = be.AppendUint64(chunks, uint64(sector))
		chunks = be.AppendUint64(chunks, uint64(count))
		chunks = be.AppendUint64(chunks, uint64(len(fork)))
		chunks = be.AppendUint64(chunks, uint64(len(enc)))
		fork = append(fork, enc...)
		n++
	}
	// The last chunk ends the table, at the end of the disk.
	chunks = be.AppendUint32(chunks, chunkEnd)
	chunks = be.AppendUint32(chunks, 0)
	chunks = be.AppendUint64(chunks, uint64(len(data)/sectorSize))
	chunks = be.AppendUint64(chunks, 0)
	chunks = be.AppendUint64(chunks, uint64(len(fork)))
	chunks = be.AppendUint64(chunks, 0)
	n++

	// The table of blocks: the header "mish", then the chunks.
	mish := make([]byte, 204, 204+len(chunks))
	copy(mish, "mish")
	be.PutUint32(mish[4:], 1)
	be.PutUint64(mish[16:], uint64(len(data)/sectorSize))
	be.PutUint32(mish[32:], 2*chunkSectors)
	be.PutUint32(mish[200:], uint32(n))
	mish = append(mish, chunks...)

	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>resource-fork</key>
	<dict>
		<key>blkx</key>
		<array>
			<dict>
				<key>Attributes</key>
				<string>0x0050</string>
				<key>CFName</key>
				<string>whole disk (Apple_HFS : 0)</string>
				<key>Data</key>
				<data>%s</data>
				<key>ID</key>
				<string>0</string>
				<key>Name</key>
				<string>whole disk (Apple_HFS : 0)</string>
			</dict>
		</array>
	</dict>
</dict>
</plist>
`, base64.StdEncoding.EncodeToString(mish))

	// The trailer "koly" closes the file.
	koly := make([]byte, 512)
	copy(koly, "koly")
	be.PutUint32(koly[4:], 4)
	be.PutUint32(koly[8:], 512)
	be.PutUint32(koly[12:], 1)
	be.PutUint64(koly[32:], uint64(len(fork)))
	be.PutUint32(koly[56:], 1)
	be.PutUint32(koly[60:], 1)
	be.PutUint64(koly[216:], uint64(len(fork)))
	be.PutUint64(koly[224:], uint64(len(xml)))
	be.PutUint32(koly[488:], 1)
	be.PutUint64(koly[492:], uint64(len(data)/sectorSize))

	out := make([]byte, 0, len(fork)+len(xml)+len(koly))
	out = append(out, fork...)
	out = append(out, xml...)
	return append(out, koly...)
}

// TestDMG reads testdata/sample.dmg, a DMG file with chunks of ADC that the
// compressor of the tests wrote, and decodes its disk. 7z and dmg2img extract
// the same disk from it. With -update, the test writes it again.
func TestDMG(t *testing.T) {
	t.Parallel()
	want := disk()
	path := filepath.Join("testdata", "sample.dmg")
	if *update {
		if err := os.WriteFile(path, dmg(want), 0o644); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	koly := b[len(b)-512:]
	if string(koly[:4]) != "koly" {
		t.Fatalf("expected the trailer koly, got: %q", koly[:4])
	}
	xml := string(b[be.Uint64(koly[216:]):][:be.Uint64(koly[224:])])
	start, end := strings.Index(xml, "<data>"), strings.Index(xml, "</data>")
	mish, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(xml[start+6:end]), ""))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	got := make([]byte, int(be.Uint64(mish[16:]))*sectorSize)
	kinds := make(map[uint32]int)
	for c := mish[204:]; be.Uint32(c) != chunkEnd; c = c[40:] {
		typ := be.Uint32(c)
		kinds[typ]++
		sector, count := be.Uint64(c[8:]), be.Uint64(c[16:])
		off, n := be.Uint64(c[24:]), be.Uint64(c[32:])
		dst := got[sector*sectorSize : (sector+count)*sectorSize]
		src := b[off : off+n]
		switch typ {
		case chunkZero:
		case chunkRaw:
			copy(dst, src)
		case chunkADC:
			out, err := Decode(dst[:0], src, len(dst))
			if err != nil {
				t.Fatalf("sector %d: expected no error, got: %v", sector, err)
			}
			if len(out) != len(dst) {
				t.Fatalf("sector %d: expected %d bytes, got: %d", sector, len(dst), len(out))
			}
		default:
			t.Fatalf("sector %d: expected a chunk of zeros, raw or ADC, got: %#x", sector, typ)
		}
	}
	if kinds[chunkZero] != 1 || kinds[chunkRaw] != 1 || kinds[chunkADC] == 0 {
		t.Errorf("expected a chunk of zeros, a raw chunk and chunks of ADC, got: %v", kinds)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the disk differs from the sample at byte %d", diff(got, want))
	}
}
