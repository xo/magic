package diff

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/adler32"
	"hash/crc32"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/xo/magic/walk"
)

// TestWalkLZ4 extracts frames that the program lz4 makes, with linked
// blocks, checksums of blocks and the size of the content. binwalk-ng
// extracts LZ4 with the crate lz4_flex, and writes decompressed.bin. It
// skips without lz4.
func TestWalkLZ4(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("lz4"); err != nil {
		t.Skip("no lz4: install lz4")
	}
	exp := lzText(t, 300<<10)
	for _, flags := range [][]string{
		{"-B4"},
		{"-B4", "-BD"},
		{"-B4", "-BX", "--content-size"},
		{"-B5", "-BD", "--no-frame-crc"},
		{"-B7", "-1"},
	} {
		t.Run(fmt.Sprint(flags), func(t *testing.T) {
			t.Parallel()
			cmd := exec.Command("lz4", append([]string{"-q", "-c"}, flags...)...)
			cmd.Stdin = bytes.NewReader(exp)
			src, err := cmd.Output()
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			lzExpect(t, "lz4", src, 0x1A3, "decompressed.bin", exp)
		})
	}
}

// TestWalkLZOP extracts lzop files, with blocks that are stored and blocks
// of LZO1X, and with each checksum. binwalk extracts them with lzop -p -N
// -d, which names the file after the name in the header. The files come
// from lzop when it is installed, and from lzopFile.
func TestWalkLZOP(t *testing.T) {
	t.Parallel()
	exp := lzText(t, 100<<10)
	// binwalk does not count the checksum of the compressed data, nor the
	// CRC-32 of the data, so it finds only files with Adler-32 of the data.
	for _, tt := range []struct {
		name  string
		flags uint32
		file  string
	}{
		{"", lzopAdler32D, "lzop_123"},
		{"walk.txt", lzopAdler32D, "walk.txt"},
		{"dir/walk.txt", lzopAdler32D | lzopHCRC32, "walk.txt"},
	} {
		lzExpect(t, "lzop", lzopFile(exp, tt.name, tt.flags, 16<<10), 0x123, tt.file, exp)
	}
	if _, err := exec.LookPath("lzop"); err == nil {
		dir := t.TempDir()
		in := filepath.Join(dir, "walk.txt")
		if err := os.WriteFile(in, exp, 0o644); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if out, err := exec.Command("lzop", "-q", in).CombinedOutput(); err != nil {
			t.Fatalf("expected no error, got: %v: %s", err, out)
		}
		src, err := os.ReadFile(in + ".lzo")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		lzExpect(t, "lzop", src, 0x123, "walk.txt", exp)
	}
}

// TestWalkCompressd extracts the LZW data of compress, which compressZ
// writes, with a code that clears the table and with a full table of codes
// of 16 bits. binwalk extracts it with 7zz, which writes
// compressd_<offset>. The data is compared with what gzip -d writes, when
// gzip is installed.
func TestWalkCompressd(t *testing.T) {
	t.Parallel()
	text := lzText(t, 300<<10)
	r := rand.New(rand.NewChaCha8([32]byte{'Z'}))
	noise := make([]byte, 600<<10)
	for i := range noise {
		noise[i] = 'a' + byte(r.IntN(16))
	}
	for _, tt := range []struct {
		data  []byte
		every int
	}{{text, 0}, {text, 5000}, {noise, 0}} {
		exp := tt.data
		src := compressZ(exp, tt.every)
		if _, err := exec.LookPath("gzip"); err == nil {
			cmd := exec.Command("gzip", "-d", "-c")
			cmd.Stdin = bytes.NewReader(src)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if !bytes.Equal(out, exp) {
				t.Fatalf("expected gzip to decode %d bytes, got: %d", len(exp), len(out))
			}
		}
		// The signature is short, so binwalk looks for it at offset 0, and
		// the data runs to the end of the file.
		lzExpect(t, "compressd", src, 0, "compressd_0", exp)
	}
}

// TestWalkCompressdAs7Zip extracts the LZW data of compress with 7-Zip and
// with the walker, and compares the files. The data is followed by random
// bytes, which 7-Zip decodes up to the first code that is not in the table.
// It needs 7zz or 7z, and skips without them.
func TestWalkCompressdAs7Zip(t *testing.T) {
	t.Parallel()
	sz, err := exec.LookPath("7zz")
	if err != nil {
		if sz, err = exec.LookPath("7z"); err != nil {
			t.Skip("no 7zz: install 7-Zip")
		}
	}
	r := rand.New(rand.NewChaCha8([32]byte{'7'}))
	for i, every := range []int{0, 3000} {
		data := compressZ(lzText(t, 100<<10), every)
		for range 100 * i {
			data = append(data, byte(r.Uint32()))
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "compressd_0.bin"), data, 0o644); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		cmd := exec.Command(sz, "x", "-y", "-o"+dir, filepath.Join(dir, "compressd_0.bin"))
		if out, err := cmd.CombinedOutput(); err != nil && cmd.ProcessState.ExitCode() != 2 {
			t.Fatalf("expected no error, got: %v: %s", err, out)
		}
		exp, err := os.ReadFile(filepath.Join(dir, "compressd_0"))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		lzExpect(t, "compressd", data, 0, "compressd_0", exp)
	}
}

// lzText is n bytes of text from the documents of the repository.
func lzText(t *testing.T, n int) []byte {
	t.Helper()
	var b []byte
	for _, name := range []string{"README.md", "AGENTS.md", filepath.Join("docs", "WALKER.md")} {
		d, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		b = append(b, d...)
	}
	return bytes.Repeat(b, n/len(b)+1)[:n]
}

// lzExpect places src at off between random bytes, and expects one match of
// format there, of the size of src, with one member, file, that holds exp.
func lzExpect(t *testing.T, format string, src []byte, off int, file string, exp []byte) {
	t.Helper()
	r := rand.New(rand.NewChaCha8([32]byte{byte(off)}))
	data := make([]byte, off, off+len(src)+64)
	for i := range data {
		data[i] = byte(r.Uint32())
	}
	data = append(data, src...)
	if format != "compressd" {
		for range 64 {
			data = append(data, byte(r.Uint32()))
		}
	}
	var got []string
	for e, err := range walk.WalkBytes(context.Background(), data, walk.WithFormats(format)) {
		if err != nil {
			t.Fatalf("%s: expected no error, got: %v", format, err)
		}
		if e.Offset != int64(off) {
			t.Errorf("%s: expected a match at %#x, got: %#x", format, off, e.Offset)
		}
		if format == "lzop" && e.Size != int64(len(src)) {
			t.Errorf("%s: expected %d bytes, got: %d", format, len(src), e.Size)
		}
		for m, err := range e.Members(context.Background()) {
			if err != nil {
				t.Fatalf("%s: expected no error, got: %v", format, err)
			}
			rc, err := m.Open()
			if err != nil {
				t.Fatalf("%s: expected no error, got: %v", format, err)
			}
			b, _ := io.ReadAll(rc)
			if !bytes.Equal(b, exp) {
				t.Errorf("%s: expected %d bytes in %s, got: %d", format, len(exp), m.Name, len(b))
			}
			got = append(got, m.Name)
		}
	}
	if !slices.Equal(got, []string{file}) {
		t.Errorf("%s: expected %q, got: %q", format, []string{file}, got)
	}
}

// The flags of lzop that lzopFile writes.
const (
	lzopAdler32D = 0x00000001
	lzopHCRC32   = 0x00001000
)

// lzopFile writes an lzop file of version 1.03, as lzop writes it, with
// blocks of size bytes. A block that LZO1X does not make smaller is stored.
func lzopFile(data []byte, name string, flags uint32, size int) []byte {
	var h []byte
	h = binary.BigEndian.AppendUint16(h, 0x1030) // the version of lzop
	h = binary.BigEndian.AppendUint16(h, 0x20a0) // the version of the library
	h = binary.BigEndian.AppendUint16(h, 0x0940) // the version to extract
	h = append(h, 1, 5)                          // LZO1X-1, level 5
	h = binary.BigEndian.AppendUint32(h, flags|0x03000000)
	h = binary.BigEndian.AppendUint32(h, 0o100644)
	h = binary.BigEndian.AppendUint32(h, 1700000000)
	h = binary.BigEndian.AppendUint32(h, 0)
	h = append(h, byte(len(name)))
	h = append(h, name...)
	out := append([]byte("\x89LZO\x00\r\n\x1a\n"), h...)
	if flags&lzopHCRC32 != 0 {
		out = binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(h))
	} else {
		out = binary.BigEndian.AppendUint32(out, adler32.Checksum(h))
	}
	for i := 0; i < len(data); i += size {
		d := data[i:min(i+size, len(data))]
		c := lzo1x(d)
		if i == 0 || len(c) >= len(d) {
			c = d // the first block is stored, to test both kinds
		}
		out = binary.BigEndian.AppendUint32(out, uint32(len(d)))
		out = binary.BigEndian.AppendUint32(out, uint32(len(c)))
		out = binary.BigEndian.AppendUint32(out, adler32.Checksum(d))
		out = append(out, c...)
	}
	return binary.BigEndian.AppendUint32(out, 0)
}

// lzo1x compresses data to LZO1X with literal runs and matches of the kind
// M3 only, which reach 16 KiB back. It finds each match by the last place
// of its first 3 bytes.
func lzo1x(data []byte) []byte {
	var out []byte
	lit := 0 // the start of the literals that are not written
	last := -1
	put := func(end int) {
		n := end - lit
		switch {
		case n == 0:
		case n <= 3 && last >= 0:
			// The 2 low bits of the match before give 1 to 3 literals.
			out[last] |= byte(n)
		case n <= 238 && last < 0:
			out = append(out, byte(17+n))
		case n <= 18:
			out = append(out, byte(n-3))
		default:
			out = append(out, 0)
			k := n - 18
			for ; k > 255; k -= 255 {
				out = append(out, 0)
			}
			out = append(out, byte(k))
		}
		out = append(out, data[lit:end]...)
	}
	seen := map[[3]byte]int{}
	for i := 0; i+3 <= len(data); {
		key := [3]byte(data[i : i+3])
		j, ok := seen[key]
		seen[key] = i
		if !ok || i-j > 0x4000 {
			i++
			continue
		}
		n := 3
		for i+n < len(data) && data[j+n] == data[i+n] {
			n++
		}
		put(i)
		if n-2 <= 31 {
			out = append(out, byte(32|(n-2)))
		} else {
			out = append(out, 32)
			k := n - 33
			for ; k > 255; k -= 255 {
				out = append(out, 0)
			}
			out = append(out, byte(k))
		}
		last = len(out)
		out = binary.LittleEndian.AppendUint16(out, uint16(i-j-1)<<2)
		i += n
		lit = i
	}
	put(len(data))
	return append(out, 0x11, 0, 0)
}

// compressZ writes data as compress -b 16 writes it. When every is not 0,
// it clears the table after that many codes, as compress does when the
// ratio falls.
func compressZ(data []byte, every int) []byte {
	out := []byte{0x1F, 0x9D, 0x90}
	var acc uint64
	var nacc, group uint
	bits, maxCode, free := uint(9), uint32(511), uint32(257)
	emit := func(code uint32) {
		acc |= uint64(code) << nacc
		nacc += bits
		group += bits
		for ; nacc >= 8; nacc -= 8 {
			out = append(out, byte(acc))
			acc >>= 8
		}
	}
	// pad ends the group of bits bytes that compress writes at one size.
	pad := func() {
		nacc += (bits*8 - group%(bits*8)) % (bits * 8)
		for ; nacc >= 8; nacc -= 8 {
			out = append(out, byte(acc))
			acc >>= 8
		}
		group = 0
	}
	table := map[[2]uint32]uint32{}
	if len(data) == 0 {
		return out
	}
	ent, codes := uint32(data[0]), 0
	for _, c := range data[1:] {
		k := [2]uint32{ent, uint32(c)}
		if v, ok := table[k]; ok {
			ent = v
			continue
		}
		emit(ent)
		codes++
		if free > maxCode && bits < 16 {
			pad()
			bits++
			maxCode = 1<<bits - 1
			if bits == 16 {
				maxCode = 1 << 16
			}
		}
		if every != 0 && codes%every == 0 {
			emit(256)
			pad()
			bits, maxCode, free = 9, 511, 257
			clear(table)
			ent = uint32(c)
			continue
		}
		if free < 1<<16 {
			table[k] = free
			free++
		}
		ent = uint32(c)
	}
	emit(ent)
	if nacc > 0 {
		out = append(out, byte(acc))
	}
	return out
}
