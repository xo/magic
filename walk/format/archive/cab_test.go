//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package archive

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xo/magic/walk"
)

// cabTestFile is a file of a cabinet that the tests make.
type cabTestFile struct {
	name string
	data string
}

// makeCAB returns a cabinet of one folder that holds files. The method is
// 0 for stored and 1 for MSZIP. With reserve set, the cabinet has reserved
// bytes in its header, its folder and each block.
func makeCAB(t *testing.T, files []cabTestFile, method uint16, reserve bool) []byte {
	t.Helper()
	var all []byte
	for _, f := range files {
		all = append(all, f.data...)
	}
	var data []byte
	var prev []byte
	for p := 0; p < len(all); p += cabFrameSize {
		chunk := all[p:min(p+cabFrameSize, len(all))]
		body := chunk
		if method == cabMethodMSZIP {
			buf := bytes.NewBufferString("CK")
			w, err := flate.NewWriterDict(buf, 9, prev)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if _, err := w.Write(chunk); err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			body, prev = buf.Bytes(), chunk
		}
		h := make([]byte, 8)
		binary.LittleEndian.PutUint16(h[4:], uint16(len(body)))
		binary.LittleEndian.PutUint16(h[6:], uint16(len(chunk)))
		binary.LittleEndian.PutUint32(h, cabChecksum(h[4:8], cabChecksum(body, 0)))
		data = append(data, h...)
		if reserve {
			data = append(data, 0xAA, 0xBB)
		}
		data = append(data, body...)
	}
	h := make([]byte, 36)
	copy(h, "MSCF")
	h[24], h[25] = 3, 1
	binary.LittleEndian.PutUint16(h[26:], 1)
	binary.LittleEndian.PutUint16(h[28:], uint16(len(files)))
	if reserve {
		binary.LittleEndian.PutUint16(h[30:], cabFlagReserve)
		h = append(h, 3, 0, 1, 2, 0x11, 0x22, 0x33)
	}
	folder := make([]byte, 8)
	binary.LittleEndian.PutUint16(folder[4:], uint16((len(all)+cabFrameSize-1)/cabFrameSize))
	binary.LittleEndian.PutUint16(folder[6:], method)
	if reserve {
		folder = append(folder, 0x44)
	}
	var entries []byte
	off := 0
	for _, f := range files {
		e := binary.LittleEndian.AppendUint32(nil, uint32(len(f.data)))
		e = binary.LittleEndian.AppendUint32(e, uint32(off))
		e = append(e, make([]byte, 8)...)
		e = append(e, f.name...)
		entries = append(entries, append(e, 0)...)
		off += len(f.data)
	}
	binary.LittleEndian.PutUint32(h[16:], uint32(len(h)+len(folder)))
	binary.LittleEndian.PutUint32(folder, uint32(len(h)+len(folder)+len(entries)))
	b := slices.Concat(h, folder, entries, data)
	binary.LittleEndian.PutUint32(b[8:], uint32(len(b)))
	return b
}

// cabTestFiles are the files of the cabinets of the tests. The large file
// takes two blocks, so the second block of MSZIP refers to the first.
func cabTestFiles() []cabTestFile {
	var big strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&big, "line %d of the cabinet\n", i*7919%10007)
	}
	return []cabTestFile{
		{"readme.txt", "hello cab\n"},
		{`dir\sub\big.txt`, big.String()},
		{"empty", ""},
		{"caf\xe9.txt", "latin1\n"},
	}
}

func TestCAB(t *testing.T) {
	t.Parallel()
	files := cabTestFiles()
	var exp []string
	for _, f := range files {
		exp = append(exp, strings.ReplaceAll(f.name, `\`, "/")+" -rw-r--r-- "+f.data)
	}
	exp = slices.Insert(exp, 1, "dir drwxr-xr-x", "dir/sub drwxr-xr-x")
	for _, method := range []uint16{cabMethodNone, cabMethodMSZIP} {
		for _, reserve := range []bool{false, true} {
			name := fmt.Sprint(method, reserve)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				c := makeCAB(t, files, method, reserve)
				e, lines := walkOne(t, slices.Concat(noise(300), c, noise(100)), "cab")
				if e.Offset != 300 || e.Size != int64(len(c)) {
					t.Errorf("expected %q, got: %q", fmt.Sprint(300, len(c)), fmt.Sprint(e.Offset, e.Size))
				}
				desc := fmt.Sprintf("Microsoft Cabinet archive, file count: 4, folder count: 1, header size: 40, total size: %d bytes", len(c))
				if e.Description != desc {
					t.Errorf("expected %q, got: %q", desc, e.Description)
				}
				check(t, lines, exp...)
				if want, ok := cabextract(t, c); ok {
					got := slices.Clone(lines)
					slices.Sort(got)
					if !slices.Equal(got, want) {
						t.Errorf("expected %q, got: %q", want, got)
					}
				}
			})
		}
	}
}

// cabextract extracts the cabinet c with cabextract, and describes the
// files as walkOne describes the members, in order. It reports false when
// cabextract is not on PATH.
func cabextract(t *testing.T, c []byte) ([]string, bool) {
	t.Helper()
	if _, err := exec.LookPath("cabextract"); err != nil {
		return nil, false
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "a.cab")
	if err := os.WriteFile(p, c, 0o644); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	out := filepath.Join(dir, "out")
	if b, err := exec.Command("cabextract", "-q", "-d", out, p).CombinedOutput(); err != nil {
		t.Fatalf("cabextract: %v: %s", err, b)
	}
	var lines []string
	err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == out {
			return err
		}
		rel, _ := filepath.Rel(out, p)
		if d.IsDir() {
			lines = append(lines, rel+" drwxr-xr-x")
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("reading %s: %w", p, err)
		}
		lines = append(lines, rel+" -rw-r--r-- "+string(b))
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	slices.Sort(lines)
	return lines, true
}

// TestCABFails holds that a cabinet with a bad checksum, a folder of
// Quantum, or a folder of LZX with a window that LZX does not have, gives no
// file. cabextract fails on the first and the last, and the walker has no
// decoder of Quantum.
func TestCABFails(t *testing.T) {
	t.Parallel()
	files := cabTestFiles()[:1]
	bad := makeCAB(t, files, cabMethodNone, false)
	bad[len(bad)-1] ^= 1
	quantum := makeCAB(t, files, cabMethodNone, false)
	quantum[36+6] = 2
	window := makeCAB(t, files, cabMethodNone, false)
	window[36+6], window[36+7] = cabMethodLZX, 22
	for name, c := range map[string][]byte{"checksum": bad, "quantum": quantum, "window": window} {
		e := walkEntry(t, slices.Concat(noise(64), c), "cab")
		var got error
		for _, err := range e.Members(context.Background()) {
			got = err
		}
		if !errors.Is(got, walk.ErrNotExtracted) {
			t.Errorf("%s: expected %q, got: %q", name, walk.ErrNotExtracted, fmt.Sprint(got))
		}
	}
}

// walkEntry walks data and returns the one entry of format.
func walkEntry(t *testing.T, data []byte, format string) *walk.Entry {
	t.Helper()
	var entry *walk.Entry
	for e, err := range walk.WalkBytes(context.Background(), data, walk.WithFormats(format)) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if e.Format == format {
			entry = e
		}
	}
	if entry == nil {
		t.Fatalf("expected %q, got: %q", format, "nothing")
	}
	return entry
}

// TestCABLZX extracts the cabinet of compress/lzx/testdata, which LZX
// compresses with E8 translation, and a cabinet of LZX that the test writes.
// Where cabextract is installed, the files are the files of cabextract.
func TestCABLZX(t *testing.T) {
	t.Parallel()
	sample, err := os.ReadFile(filepath.Join("..", "..", "..", "compress", "lzx", "testdata", "sample.cab"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	_, lines := walkOne(t, slices.Concat(noise(100), sample, noise(30)), "cab")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "sample.txt -rw-r--r-- ") {
		t.Fatalf("expected %q, got: %q", "sample.txt", lines)
	}
	const want = "29e93885f6482a2b291341559d30ea92ac2fc9b88c516970a7a4b20b46742e67"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimPrefix(lines[0], "sample.txt -rw-r--r-- ")))); got != want {
		t.Errorf("expected %q, got: %q", want, got)
	}
	if exp, ok := cabextract(t, sample); ok && !slices.Equal(lines, exp) {
		t.Errorf("expected the files of cabextract, got: %d bytes", len(lines[0]))
	}

	files := cabTestFiles()
	c := makeLZXCAB(files)
	_, lines = walkOne(t, slices.Concat(noise(64), c), "cab")
	var exp []string
	for _, f := range files {
		exp = append(exp, strings.ReplaceAll(f.name, `\`, "/")+" -rw-r--r-- "+f.data)
	}
	exp = slices.Insert(exp, 1, "dir drwxr-xr-x", "dir/sub drwxr-xr-x")
	check(t, lines, exp...)
	if want, ok := cabextract(t, c); ok {
		got := slices.Clone(lines)
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("expected %q, got: %q", want, got)
		}
	}
}

// lzxWriter writes the bits of LZX: 16-bit words, little endian, from the
// highest bit of each word.
type lzxWriter struct {
	out []byte
	acc uint16
	n   uint
}

func (w *lzxWriter) bits(v uint32, k uint) {
	for i := int(k) - 1; i >= 0; i-- {
		w.acc = w.acc<<1 | uint16(v>>uint(i)&1)
		if w.n++; w.n == 16 {
			w.out = binary.LittleEndian.AppendUint16(w.out, w.acc)
			w.acc, w.n = 0, 0
		}
	}
}

// makeLZXCAB returns a cabinet of one folder of LZX, with a window of
// 2^16 bytes, that holds files. The data is in uncompressed blocks of LZX.
// The first block has an odd size and ends in the first frame, so a byte of
// padding follows it. The second block holds the rest, over the other
// frames. The stream has E8 translation, and the data has no byte 0xE8.
func makeLZXCAB(files []cabTestFile) []byte {
	var all []byte
	for _, f := range files {
		all = append(all, f.data...)
	}
	first := 1001
	block := func(w *lzxWriter, size int) {
		w.bits(3, 3)
		w.bits(uint32(size), 24)
		w.bits(0, 16-w.n) // to the next word, or a whole word
		for range 3 {
			w.out = binary.LittleEndian.AppendUint32(w.out, 1)
		}
	}
	var data []byte
	var blocks int
	for p := 0; p < len(all); p += cabFrameSize {
		chunk := all[p:min(p+cabFrameSize, len(all))]
		w := &lzxWriter{}
		if p == 0 {
			w.bits(1, 1) // E8 translation
			w.bits(0, 16)
			w.bits(12000000, 16)
			block(w, first)
			w.out = append(w.out, chunk[:first]...)
			w.out = append(w.out, 0)
			block(w, len(all)-first)
			w.out = append(w.out, chunk[first:]...)
		} else {
			w.out = append(w.out, chunk...)
		}
		h := make([]byte, 8)
		binary.LittleEndian.PutUint16(h[4:], uint16(len(w.out)))
		binary.LittleEndian.PutUint16(h[6:], uint16(len(chunk)))
		binary.LittleEndian.PutUint32(h, cabChecksum(h[4:8], cabChecksum(w.out, 0)))
		data = append(data, h...)
		data = append(data, w.out...)
		blocks++
	}
	h := make([]byte, 36)
	copy(h, "MSCF")
	h[24], h[25] = 3, 1
	binary.LittleEndian.PutUint16(h[26:], 1)
	binary.LittleEndian.PutUint16(h[28:], uint16(len(files)))
	folder := make([]byte, 8)
	binary.LittleEndian.PutUint16(folder[4:], uint16(blocks))
	binary.LittleEndian.PutUint16(folder[6:], cabMethodLZX|16<<8)
	var entries []byte
	off := 0
	for _, f := range files {
		e := binary.LittleEndian.AppendUint32(nil, uint32(len(f.data)))
		e = binary.LittleEndian.AppendUint32(e, uint32(off))
		e = append(e, make([]byte, 8)...)
		e = append(e, f.name...)
		entries = append(entries, append(e, 0)...)
		off += len(f.data)
	}
	binary.LittleEndian.PutUint32(h[16:], uint32(len(h)+len(folder)))
	binary.LittleEndian.PutUint32(folder, uint32(len(h)+len(folder)+len(entries)))
	b := slices.Concat(h, folder, entries, data)
	binary.LittleEndian.PutUint32(b[8:], uint32(len(b)))
	return b
}
