//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/xo/magic/walk"
)

// noise returns n random bytes from a fixed seed.
func noise(n int) []byte {
	r := rand.New(rand.NewPCG(1, uint64(n)))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// walkOne walks data, and returns the one entry of format, and its members
// as "name mode [link|content]" lines.
func walkOne(t *testing.T, data []byte, format string) (*walk.Entry, []string) {
	t.Helper()
	ctx := context.Background()
	var entry *walk.Entry
	for e, err := range walk.WalkBytes(ctx, data, walk.WithFormats(format)) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if e.Format == format {
			if entry != nil {
				t.Fatalf("expected one %s, got two", format)
			}
			entry = e
		}
	}
	if entry == nil {
		t.Fatalf("expected %q, got: %q", format, "nothing")
	}
	var lines []string
	for m, err := range entry.Members(ctx) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		line := fmt.Sprintf("%s %v", m.Name, m.Mode)
		switch {
		case m.Mode&fs.ModeSymlink != 0:
			line += " -> " + m.Link
		case m.Mode.IsRegular():
			rc, err := m.Open()
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			b, _ := io.ReadAll(rc)
			line += " " + string(b)
		}
		lines = append(lines, line)
	}
	return entry, lines
}

// check compares the lines of the members with exp.
func check(t *testing.T, lines []string, exp ...string) {
	t.Helper()
	if got, want := strings.Join(lines, "\n"), strings.Join(exp, "\n"); got != want {
		t.Errorf("expected %q, got: %q", want, got)
	}
}

func TestZIP(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(h *zip.FileHeader, content string) {
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
	dir := &zip.FileHeader{Name: "d/"}
	dir.SetMode(fs.ModeDir | 0o755)
	add(dir, "")
	add(&zip.FileHeader{Name: "d/a.txt", Method: zip.Deflate}, strings.Repeat("a", 100))
	run := &zip.FileHeader{Name: "./run.sh", Method: zip.Store}
	run.SetMode(0o755)
	add(run, "#!/bin/sh\n")
	link := &zip.FileHeader{Name: "ln"}
	link.SetMode(fs.ModeSymlink | 0o777)
	add(link, "d/a.txt")
	// An encrypted member is skipped.
	add(&zip.FileHeader{Name: "secret", Flags: 1}, "xxxx")
	add(&zip.FileHeader{Name: "../up.txt"}, "up")
	if err := zw.Close(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	data := append(noise(1000), buf.Bytes()...)
	data = append(data, noise(300)...)
	e, lines := walkOne(t, data, "zip")
	if e.Offset != 1000 || e.Size != int64(buf.Len()) {
		t.Errorf("expected %q, got: %q", fmt.Sprint(1000, buf.Len()), fmt.Sprint(e.Offset, e.Size))
	}
	exp := fmt.Sprintf("ZIP archive, version: 2.0, file count: 6, total size: %d bytes", buf.Len())
	if e.Description != exp {
		t.Errorf("expected %q, got: %q", exp, e.Description)
	}
	check(t, lines,
		"d drwxr-xr-x",
		"d/a.txt -rw-r--r-- "+strings.Repeat("a", 100),
		"run.sh -rw-r--r-x #!/bin/sh\n",
		"ln Lrwxrwxrwx -> d/a.txt",
		"up.txt -rw-r--r-- up",
	)
}

// zipLocalHeader returns a local file header of a stored member.
func zipLocalHeader(name, content string) []byte {
	b := binary.LittleEndian.AppendUint32(nil, 0x04034b50)
	b = binary.LittleEndian.AppendUint16(b, 10)
	b = append(b, make([]byte, 8)...) // flags, method, time, date
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(content)))
	b = binary.LittleEndian.AppendUint32(b, uint32(len(content)))
	b = binary.LittleEndian.AppendUint16(b, uint16(len(name)))
	b = binary.LittleEndian.AppendUint16(b, 0)
	b = append(b, name...)
	return append(b, content...)
}

func TestZIPWithoutEOCD(t *testing.T) {
	t.Parallel()
	z := append(zipLocalHeader("a", "hello"), zipLocalHeader("b/c", "world")...)
	data := append(noise(64), z...)
	data = append(data, bytes.Repeat([]byte{0xff}, 100)...)
	e, lines := walkOne(t, data, "zip")
	exp := fmt.Sprintf("ZIP archive, version: 1.0, missing end-of-central-directory header, total size: %d bytes", len(z))
	if e.Description != exp {
		t.Errorf("expected %q, got: %q", exp, e.Description)
	}
	check(t, lines,
		"a -rw-r--r-- hello",
		"b drwxr-xr-x",
		"b/c -rw-r--r-- world",
	)
}

func TestTarball(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, h := range []*tar.Header{
		{Name: "./etc/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "./etc/passwd", Typeflag: tar.TypeReg, Mode: 0o644, Size: 5},
		{Name: "./bin/sh", Typeflag: tar.TypeReg, Mode: 0o755, Size: 3},
		{Name: "./bin/ls", Typeflag: tar.TypeSymlink, Linkname: "sh"},
		{Name: "./bin/ash", Typeflag: tar.TypeLink, Linkname: "./bin/sh"},
		{Name: "./dev/null", Typeflag: tar.TypeChar, Mode: 0o666, Devmajor: 1, Devminor: 3},
		{Name: "./dev/sda", Typeflag: tar.TypeBlock, Mode: 0o660, Devmajor: 8},
		{Name: "./dev/fifo", Typeflag: tar.TypeFifo, Mode: 0o600},
	} {
		h.Format = tar.FormatUSTAR
		if err := tw.WriteHeader(h); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if _, err := tw.Write([]byte("root:sh!!"[:h.Size])); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	data := append(noise(777), buf.Bytes()...)
	e, lines := walkOne(t, data, "tarball")
	// Eight headers, and one block of data for each of two files.
	if e.Offset != 777 || e.Size != 10*512 || e.Confidence != walk.Medium {
		t.Errorf("expected %q, got: %q", "777 5120 medium", fmt.Sprint(e.Offset, e.Size, e.Confidence))
	}
	if exp := "POSIX tar archive, file count: 8"; e.Description != exp {
		t.Errorf("expected %q, got: %q", exp, e.Description)
	}
	// binwalk-ng sets the mode of each file and folder. It makes a hard
	// link a symbolic link, whose target is relative to the folder of the
	// link. It writes no device and no FIFO.
	check(t, lines,
		"etc drwxr-xr-x",
		"etc/passwd -rw-r--r-- root:",
		"bin drwxr-xr-x",
		"bin/sh -rwxr-xr-x roo",
		"bin/ls Lrwxrwxrwx -> ../bin/sh",
		"bin/ash Lrwxrwxrwx -> ../bin/bin/sh",
	)
}

// cpioNewc returns an entry of a newc archive.
func cpioNewc(magic, name string, ino, mode, major, minor int, content string) []byte {
	b := fmt.Appendf(nil, "%s%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X",
		magic, ino, mode, 0, 0, 1, 0, len(content), 0, 0, major, minor, len(name)+1, 0)
	b = append(b, name...)
	b = append(b, 0)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	b = append(b, content...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func TestCPIO(t *testing.T) {
	t.Parallel()
	for _, magic := range []string{"070701", "070702"} {
		c := make([]byte, 0, 1024)
		for i, e := range []struct {
			name               string
			mode, major, minor int
			content            string
		}{
			{".", 0o040755, 0, 0, ""},
			{"bin", 0o040755, 0, 0, ""},
			{"bin/busybox", 0o100755, 0, 0, "ELF"},
			{"bin/sh", 0o120777, 0, 0, "busybox"},
			{"dev/console", 0o020600, 5, 1, ""},
			{"dev/loop0", 0o060600, 7, 0, ""},
			{"dev/initctl", 0o010600, 0, 0, ""},
			{"etc/issue", 0o100600, 0, 0, ""},
			{"etc/motd", 0o100644, 0, 0, "hi"},
			{cpioTrailer, 0, 0, 0, ""},
		} {
			// etc/issue is a hard link to etc/motd: it has the inode of
			// etc/motd, and no data.
			ino := i + 1
			if e.name == "etc/issue" {
				ino++
			}
			c = append(c, cpioNewc(magic, e.name, ino, e.mode, e.major, e.minor, e.content)...)
		}
		data := append(noise(333), c...)
		data = append(data, make([]byte, 512)...)
		e, lines := walkOne(t, data, "cpio")
		if e.Offset != 333 || e.Size != int64(len(c)) {
			t.Errorf("%s: expected %q, got: %q", magic, fmt.Sprint(333, len(c)), fmt.Sprint(e.Offset, e.Size))
		}
		if exp := "CPIO ASCII archive, file count: 9"; e.Description != exp {
			t.Errorf("%s: expected %q, got: %q", magic, exp, e.Description)
		}
		check(t, lines,
			"bin drwxr-xr-x",
			"bin/busybox -rw-r--r-x ELF",
			"bin/sh Lrwxrwxrwx -> ../bin/busybox",
			"dev drwxr-xr-x",
			"dev/console -rw-r--r-- ",
			"dev/loop0 -rw-r--r-- ",
			"dev/initctl -rw-r--r-- ",
			"etc drwxr-xr-x",
			"etc/issue -rw-r--r-- hi",
			"etc/motd -rw-r--r-- hi",
		)
	}
}

// TestCPIOOdc holds that binwalk does not find the old portable format,
// whose magic is "070707".
func TestCPIOOdc(t *testing.T) {
	t.Parallel()
	c := fmt.Appendf(nil, "070707%06o%06o%06o%06o%06o%06o%06o%011o%06o%011o%s\x00hi",
		0, 1, 0o100644, 0, 0, 1, 0, 0, 2, 2, "a")
	c = fmt.Appendf(c, "070707%06o%06o%06o%06o%06o%06o%06o%011o%06o%011o%s\x00",
		0, 0, 0, 0, 0, 1, 0, 0, 11, 0, cpioTrailer)
	for e, err := range walk.WalkBytes(context.Background(), append(noise(100), c...), walk.WithFormats("cpio")) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if e.Format == "cpio" {
			t.Errorf("expected %q, got: %q", "no cpio", e.Description)
		}
	}
}

// TestParseQuirks holds the readers of numbers of binwalk.
func TestParseQuirks(t *testing.T) {
	t.Parallel()
	for s, exp := range map[string]int{"0000000A": 10, "+000000a": 10, "+": -1, "++000001": -1, "-0000001": -1, "0000000g": -1} {
		n, ok := cpioHex([]byte(s))
		if !ok {
			n = -1
		}
		if n != exp {
			t.Errorf("%q: expected %q, got: %q", s, fmt.Sprint(exp), fmt.Sprint(n))
		}
	}
	for s, exp := range map[string]int{"0000000017\x00": 15, "17  \x00": 15, "\x00": 0, "": 0, "19\x00": -1, "7x7": -1, "1 7": -1} {
		n, ok := tarOctal([]byte(s))
		if !ok {
			n = -1
		}
		if n != exp {
			t.Errorf("%q: expected %q, got: %q", s, fmt.Sprint(exp), fmt.Sprint(n))
		}
	}
	// binwalk-ng divides in integers, so the last byte of 2^24+1 bytes
	// takes a block of its own.
	block := make([]byte, 512)
	copy(block[257:], "ustar")
	copy(block[124:], fmt.Sprintf("%011o", 1<<24+1))
	if n, _ := tarEntrySize(block); n != 2*512+1<<24 {
		t.Errorf("expected %q, got: %q", fmt.Sprint(2*512+1<<24), fmt.Sprint(n))
	}
}

// TestZIPDescriptor holds how 7z reads members whose sizes follow the data,
// in an archive without a central directory. The first member ends at the
// descriptor whose size is that of its data. The last has no such
// descriptor, so it runs to the end of the carved bytes.
func TestZIPDescriptor(t *testing.T) {
	t.Parallel()
	described := func(name, content string, descriptor bool) []byte {
		b := zipLocalHeader(name, "")
		binary.LittleEndian.PutUint16(b[6:], 8)
		b = append(b, content...)
		if descriptor {
			b = append(b, "PK\x07\x08"...)
			b = binary.LittleEndian.AppendUint32(b, 0)
			b = binary.LittleEndian.AppendUint32(b, uint32(len(content)))
			b = binary.LittleEndian.AppendUint32(b, uint32(len(content)))
		}
		return b
	}
	first := described("a", "hello", true)
	data := slices.Concat(noise(64), first, described("b", "world", false))
	// The parser counts no data for a member with a descriptor, so the
	// first match ends at the data of its member, and 7z gets the bytes to
	// the end of the input on its second run. The second member is a match
	// of its own too.
	var got []string
	for e, err := range walk.WalkBytes(context.Background(), data, walk.WithFormats("zip")) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		for m, err := range e.Members(context.Background()) {
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			rc, err := m.Open()
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			b, _ := io.ReadAll(rc)
			got = append(got, fmt.Sprint(e.Offset, " ", m.Name, " ", string(b)))
		}
	}
	check(t, got,
		"64 a hello",
		"64 b world",
		fmt.Sprint(64+len(first), " b world"),
	)
}

// TestZIPMismatch holds that 7z writes an empty file for a member whose
// local header does not match the central directory.
func TestZIPMismatch(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"a", "b"} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if _, err := io.WriteString(w, name+name); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	z := buf.Bytes()
	// Change the method of the local header of "b".
	i := bytes.LastIndex(z, []byte("PK\x03\x04"))
	z[i+8] = 8
	_, lines := walkOne(t, append(noise(10), z...), "zip")
	check(t, lines,
		"a -rw-r--r-- aa",
		"b -rw-r--r-- ",
	)
}
