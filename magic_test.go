package magic

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestVersion(t *testing.T) {
	t.Logf("version: %s", Version())
}

func TestMime(t *testing.T) {
	t.Parallel()
	m, err := New(MimeType)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	for _, test := range mimeTests() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mime, err := m.Buffer([]byte(test.s))
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if mime != test.exp {
				t.Errorf("expected %q, got: %q", test.exp, mime)
			}
		})
	}
}

// TestOracle cross-checks the embedded database against the system file(1).
//
// The expectations in mimeTests are libmagic's own output, so on their own
// they would only prove the wrapper is self-consistent. Comparing against an
// independent build of the same libmagic version proves it is *correct* --
// that the embedded magic.mgc, the cgo glue and the flag handling all agree
// with the reference implementation. Skipped unless a matching file(1) is on
// PATH, so it guards development without constraining CI.
func TestOracle(t *testing.T) {
	t.Parallel()
	exe, err := exec.LookPath("file")
	if err != nil {
		t.Skip("file(1) not on PATH")
	}
	out, err := exec.Command(exe, "--version").Output()
	if err != nil {
		t.Skipf("file --version: %v", err)
	}
	if want := "file-" + Version(); !strings.HasPrefix(string(out), want) {
		t.Skipf("system file(1) is not %s", want)
	}

	m, err := New(MimeType)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer m.Close()

	dir := t.TempDir()
	for i, test := range mimeTests() {
		if len(test.s) == 0 {
			continue
		}
		name := filepath.Join(dir, fmt.Sprintf("case%d", i))
		if err := os.WriteFile(name, []byte(test.s), 0o644); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		out, err := exec.Command(exe, "--mime-type", "-b", name).Output()
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		sys := strings.TrimSpace(string(out))
		got, err := m.Buffer([]byte(test.s))
		if err != nil {
			t.Fatalf("%s: expected no error, got: %v", test.name, err)
		}
		if got != sys {
			t.Errorf("%s: wrapper says %q, file(1) says %q", test.name, got, sys)
		}
	}
}

// TestFile checks the File path alongside Buffer, since it takes a different
// route through libmagic.
func TestFile(t *testing.T) {
	t.Parallel()
	m, err := New(MimeType)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer m.Close()
	for _, test := range []struct{ file, exp string }{
		{"gif.gif", "image/gif"},
		{"utf8.txt", "text/plain"},
		{"xlsx.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"tar.tar", "application/x-tar"},
	} {
		got, err := m.File(filepath.Join("testdata", test.file))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if got != test.exp {
			t.Errorf("%s: expected %q, got: %q", test.file, test.exp, got)
		}
	}
}

// TestTypes covers the flag-swapping API: several views of one input from a
// single handle.
func TestTypes(t *testing.T) {
	t.Parallel()
	m, err := New(MimeType)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer m.Close()

	flags := []int{None, MimeType, MimeEncoding, Mime, Extension}
	exp := []string{
		"GIF image data, version 89a, 1375 x 825",
		"image/gif",
		"binary",
		"image/gif; charset=binary",
		"gif",
	}

	got, err := m.FileTypes(filepath.Join("testdata", "gif.gif"), flags...)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(got) != len(exp) {
		t.Fatalf("expected %d results, got: %d", len(exp), len(got))
	}
	for i := range exp {
		if got[i] != exp[i] {
			t.Errorf("flag %d: expected %q, got: %q", i, exp[i], got[i])
		}
	}

	buf, err := os.ReadFile(filepath.Join("testdata", "gif.gif"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	got, err = m.BufferTypes(buf, flags...)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	for i := range exp {
		if got[i] != exp[i] {
			t.Errorf("buffer flag %d: expected %q, got: %q", i, exp[i], got[i])
		}
	}

	// No flags means the handle's own.
	one, err := m.BufferTypes(buf)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(one) != 1 || one[0] != "image/gif" {
		t.Errorf("expected [image/gif], got: %q", one)
	}
}

// TestFlagsRestored checks that borrowing the cookie for other flags leaves
// the handle's configured flags alone.
func TestFlagsRestored(t *testing.T) {
	t.Parallel()
	m, err := New(MimeType)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer m.Close()
	name := filepath.Join("testdata", "gif.gif")

	if _, err := m.FileTypes(name, None, Extension); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got := m.Flags(); got != MimeType {
		t.Errorf("expected flags %d, got: %d", MimeType, got)
	}
	if got, err := m.File(name); err != nil || got != "image/gif" {
		t.Errorf("expected image/gif, got: %q (%v)", got, err)
	}

	if _, err := m.FileWith(None, name); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got, err := m.File(name); err != nil || got != "image/gif" {
		t.Errorf("after FileWith: expected image/gif, got: %q (%v)", got, err)
	}

	// SetFlags is sticky, unlike the With/Types borrows.
	if err := m.SetFlags(None); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got := m.Flags(); got != None {
		t.Errorf("expected flags %d, got: %d", None, got)
	}
	if got, err := m.File(name); err != nil || !strings.HasPrefix(got, "GIF image data") {
		t.Errorf("expected a GIF description, got: %q (%v)", got, err)
	}
}

// TestClosed checks that every method reports a closed handle instead of
// handing a nil cookie to C.
func TestClosed(t *testing.T) {
	t.Parallel()
	m, err := New(MimeType)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Errorf("double Close: expected no error, got: %v", err)
	}

	name := filepath.Join("testdata", "gif.gif")
	if _, err := m.Buffer([]byte("GIF89a")); err == nil {
		t.Error("Buffer: expected an error")
	}
	if _, err := m.File(name); err == nil {
		t.Error("File: expected an error")
	}
	if _, err := m.BufferWith(None, []byte("GIF89a")); err == nil {
		t.Error("BufferWith: expected an error")
	}
	if _, err := m.FileWith(None, name); err == nil {
		t.Error("FileWith: expected an error")
	}
	if _, err := m.BufferTypes([]byte("GIF89a"), None); err == nil {
		t.Error("BufferTypes: expected an error")
	}
	if _, err := m.FileTypes(name, None); err == nil {
		t.Error("FileTypes: expected an error")
	}
	if err := m.SetFlags(None); err == nil {
		t.Error("SetFlags: expected an error")
	}
}

// TestEmptyBuffer checks the zero-length guard, which exists because
// magic_buffer would otherwise be handed &buf[0] of an empty slice.
func TestEmptyBuffer(t *testing.T) {
	t.Parallel()
	m, err := New(MimeType)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer m.Close()
	if _, err := m.Buffer(nil); err == nil {
		t.Error("expected an error")
	}
	if _, err := m.Buffer([]byte{}); err == nil {
		t.Error("expected an error")
	}
}

// TestConcurrent hammers one handle from many goroutines. Run under -race,
// this is what backs the claim that a Magic is safe to share.
func TestConcurrent(t *testing.T) {
	t.Parallel()
	m, err := New(MimeType)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer m.Close()

	buf, err := os.ReadFile(filepath.Join("testdata", "gif.gif"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	name := filepath.Join("testdata", "gif.gif")

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for range 25 {
				var err error
				switch i % 5 {
				case 0:
					var s string
					s, err = m.Buffer(buf)
					if err == nil && s != "image/gif" {
						t.Errorf("expected image/gif, got: %q", s)
					}
				case 1:
					var got []string
					got, err = m.BufferTypes(buf, None, MimeType, Extension)
					if err == nil && got[1] != "image/gif" {
						t.Errorf("expected image/gif, got: %q", got[1])
					}
				case 2:
					_, err = m.FileTypes(name, Mime, Extension)
				case 3:
					_, err = m.BufferWith(None, buf)
				case 4:
					_, err = m.FileWith(MimeEncoding, name)
				}
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

// mimeTest is one detection case: feed s to libmagic and expect exp.
//
// exp records what libmagic 5.48 actually reports, cross-checked against the
// system file(1) of the same version. Several values are less specific than
// the MIME type the input "is" (a bare "PK\x03\x04" is application/octet-stream,
// not application/zip) because libmagic wants more than a signature prefix
// before it commits. That is the library's behaviour, not a gap in the test.
type mimeTest struct {
	name string
	s    string
	exp  string
}

func mimeTests() []mimeTest {
	return []mimeTest{
		{"3gpp2", "\x00\x00\x00\x18ftyp3g24", "video/3gpp2"},
		{"3gpp2 without ftyp", "\x00\x00\x00\x18mtyp3g24", "application/octet-stream"},
		{"3gp", "\x00\x00\x00\x18ftyp3gp1", "video/3gpp"},
		{
			"3mf",
			`<?xml version="1.0"?><model xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">`,
			"model/3mf",
		},
		{"7z", "\x37\x7A\xBC\xAF\x27\x1C", "application/octet-stream"},
		{"a", "\x21\x3C\x61\x72\x63\x68\x3E", "text/plain"},
		{"aac 1", "\xFF\xF1", "audio/x-hx-aac-adts"},
		{"aac 2", "\xFF\xF9", "audio/x-hx-aac-adts"},
		{"accdb", offset(4, "Standard ACE DB"), "application/x-msaccess"}, // shares a MIME with mdb
		{"aiff", "\x46\x4F\x52\x4D\x00\x00\x00\x00\x41\x49\x46\x46\x00", "audio/x-aiff"},
		{"amf", `<?xml version="1.0"?><amf>`, "application/x-amf"},
		{"amr", "\x23\x21\x41\x4D\x52", "audio/AMR"},
		{"ape", "\x4D\x41\x43\x20\x96\x0F\x00\x00\x34\x00\x00\x00\x18\x00\x00\x00\x90\xE3", "audio/x-ape"},
		{"apng", "\x89\x50\x4E\x47\x0D\x0A\x1A\x0A" + offset(29, "acTL"), "application/octet-stream"},
		{"asf", "\x30\x26\xB2\x75\x8E\x66\xCF\x11\xA6\xD9\x00\xAA\x00\x62\xCE\x6C", "video/x-ms-asf"},
		{"atom", `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom">`, "text/xml"},
		{"au", "\x2E\x73\x6E\x64", "text/troff"},
		{"avi", "RIFF\x00\x00\x00\x00AVI LIST\x00", "video/x-msvideo"},
		{"avif", "\x00\x00\x00\x18ftypavif", "image/avif"},
		{"avis", "\x00\x00\x00\x18ftypavis", "image/avif"},
		{"bmp", "\x42\x4D", "text/plain"},
		{"bpg", "\x42\x50\x47\xFB", "image/bpg"},
		{"bz2", "\x42\x5A\x68", "application/x-bzip2"},
		{"cab", "MSCF\x00\x00\x00\x00", "text/plain"},
		{"cab.is", "ISc(\x00\x00\x00\x01", "application/x-installshield"},
		{"chm", "ITSF\003\000\000\000\x60\000\000\000", "application/vnd.ms-htmlhelp"},
		{"class", "\xCA\xFE\xBA\xBE\x00\x00\x00\xFF", "application/x-java-applet"},
		{
			"crx",
			"Cr24\x00\x00\x00\x00\x01\x00\x00\x00\x0F\x00\x00\x00" + offset(16, "") + "\x50\x4B\x03\x04",
			"application/x-chrome-extension",
		},
		{
			"csv",
			`1,2
"abc","def"
a,"b`,
			"text/csv",
		},
		{
			`csv with \r\n`,
			"1,2\r\n3,4\r\na,b",
			"text/csv",
		},
		{"cpio 7", "070707", "application/x-cpio"},
		{"cpio 1", "070701", "application/x-cpio"},
		{"cpio 2", "070702", "application/x-cpio"},
		{"dae", `<?xml version="1.0"?><COLLADA xmlns="http://www.collada.org/2005/11/COLLADASchema">`, "model/vnd.collada+xml"},
		{"dbf", "\x03\x5f\x07\x1a\x96\x0f\x00\x00\xc1\x00\xa3\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x6f\x73\x6d\x5f\x69\x64\x00\x00\x00\x00\x00\x43\x00\x00\x00\x00\x0a\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x63\x6f\x64\x65", "application/x-dbf"},
		{"dcm", offset(128, "\x44\x49\x43\x4D"), "application/dicom"},
		{"deb", "\x21\x3c\x61\x72\x63\x68\x3e\x0a\x64\x65\x62\x69\x61\x6e\x2d\x62\x69\x6e\x61\x72\x79", "application/vnd.debian.binary-package"},
		{"djvu", "\x41\x54\x26\x54\x46\x4F\x52\x4D\x00\x00\x00\x00DJVU", "image/vnd.djvu"},
		{"djvuM", "\x41\x54\x26\x54\x46\x4F\x52\x4D\x00\x00\x00\x00DJVM", "image/vnd.djvu"},
		{"djvuI", "\x41\x54\x26\x54\x46\x4F\x52\x4D\x00\x00\x00\x00DJVI", "image/vnd.djvu"},
		{"djvuTHUM", "\x41\x54\x26\x54\x46\x4F\x52\x4D\x00\x00\x00\x00THUM", "image/vnd.djvu"},
		{"doc", fromDisk("doc.doc"), "application/x-ole-storage"},
		{"docx", fromDisk("docx.docx"), "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"rpm 1", "\xed\xab\xee\xdb", "application/x-rpm"},
		{"rpm 2", "drpm", "application/x-rpm"},
		{"dwg", "\x41\x43\x31\x30\x32\x34", "image/vnd.dwg"},
		{"eot", "\xbe\x45\x00\x00\xfa\x44\x00\x00\x02\x00\x02\x00\x04\x00\x00\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01\x00\x90\x01\x00\x00\x00\x00\x4c\x50", "application/octet-stream"},
		{"epub", "\x50\x4B\x03\x04" + offset(26, "mimetypeapplication/epub+zip"), "application/octet-stream"},
		{"exe", "\x4D\x5A", "text/plain"},
		{"fdf", "%FDF", "text/plain"},
		{"fits", "\x53\x49\x4d\x50\x4c\x45\x20\x20\x3d\x20\x20\x20\x20\x20\x20\x20\x20\x20\x20\x20\x20\x20\x20\x20\x20\x20\x20\x20\x20\x54", "text/plain"},
		{"flac", "\x66\x4C\x61\x43\x00\x00\x00\x22", "audio/flac"},
		{"flv", "\x46\x4C\x56\x01", "video/x-flv"},
		{"gbr", offset(20, "GIMP"), "image/x-gimp-gbr"},
		{"geojson", `{"type":"Feature"}`, "application/json"},
		{"geojson with space", `{ "type" : "Feature" }`, "application/json"},
		{"gltf1", `{"asset":{"version":"1.0"}}`, "application/json"},
		{"gltf2", `{"asset":{"version":"2.0"}}`, "application/json"},
		{"gif 87", "GIF87a", "image/gif"},
		{"gif 89", "GIF89a", "image/gif"},
		{"glb 1", "\x67\x6C\x54\x46\x02\x00\x00\x00", "application/octet-stream"},
		{"glb 2", "\x67\x6C\x54\x46\x01\x00\x00\x00", "application/octet-stream"},
		{"gml", `<?xml version="1.0"?><any xmlns:gml="http://www.opengis.net/gml">`, "text/xml"},
		{"gml3.2", `<?xml version="1.0"?><any xmlns:gml="http://www.opengis.net/gml/3.2">`, "text/xml"},
		{"gml3.3", `<?xml version="1.0"?><any xmlns:gml="http://www.opengis.net/gml/3.3/exr">`, "text/xml"},
		{"gpx", `<?xml version="1.0"?><gpx xmlns="http://www.topografix.com/GPX/1/1">`, "text/xml"},
		{"gz", "\x1F\x8B", "application/octet-stream"},
		{"har", `{"log":{ "version": "1.2"}}`, "application/json"},
		{"hdr", "#?RADIANCE\n", "image/vnd.radiance"},
		{"heic", "\x00\x00\x00\x18ftypheic", "image/heic"},
		{"heix", "\x00\x00\x00\x18ftypheix", "image/heic"},
		{"heif mif1", "\x00\x00\x00\x18ftypmif1", "image/heif"},
		{"heif heim", "\x00\x00\x00\x18ftypheim", "image/heif"},
		{"heif heis", "\x00\x00\x00\x18ftypheis", "image/heif"},
		{"heif avic", "\x00\x00\x00\x18ftypavic", "image/heif"},
		{"html", `<HtMl><bOdY>blah blah blah</body></html>`, "text/html"},
		{"html empty", `<HTML></HTML>`, "text/html"},
		{"html just header", `   <!DOCTYPE HTML>...`, "text/html"},
		{"line ending before html", "\r\n<html>...", "text/html"},
		{
			"html with encoding",
			`<html><head><meta http-equiv="Content-Type" content="text/html; charset=iso-8859-1">`,
			"text/html",
		},
		{
			"html with comment prefix",
			`<!-- this comment should not affect --><html><head>`,
			"text/html",
		},
		{"ico 01", "\x00\x00\x01\x00", "application/octet-stream"},
		{"ico 02", "\x00\x00\x02\x00", "application/octet-stream"},
		{"ics", "BEGIN:VCALENDAR\n00", "text/x-vcalendar"},
		{"ics dos", "BEGIN:VCALENDAR\r\n00", "text/x-vcalendar"},
		{"txt iso88591", "\x0a\xe6\xf8\xe6\xf8\xe5\xe6\xf8\xe5\xe5\x0a", "text/plain"},
		{"jar", fromDisk("jar.jar"), "application/java-archive"},
		{"jar executable", "PK\x03\x04" + offset(0x1A, "\xFE\xCA"), "application/java-archive"},
		{"jar in zip #639", fromDisk("jar_in_zip.zip"), "application/zip"},
		{"jp2", "\x00\x00\x00\x0c\x6a\x50\x20\x20\x0d\x0a\x87\x0a\x00\x00\x00\x14\x66\x74\x79\x70\x6a\x70\x32\x20", "image/jp2"},
		{"jpf", "\x00\x00\x00\x0c\x6a\x50\x20\x20\x0d\x0a\x87\x0a\x00\x00\x00\x1c\x66\x74\x79\x70\x6a\x70\x78\x20", "image/jpx"},
		{"jpg", "\xFF\xD8\xFF", "text/plain"},
		{"jpm", "\x00\x00\x00\x0c\x6a\x50\x20\x20\x0d\x0a\x87\x0a\x00\x00\x00\x14\x66\x74\x79\x70\x6a\x70\x6d\x20", "image/jpm"},
		{"jxl 1", "\xFF\x0A", "image/jxl"},
		{"jxl 2", "\x00\x00\x00\x0cJXL\x20\x0d\x0a\x87\x0a", "image/jxl"},
		{"jxr", "\x49\x49\xBC\x01", "application/octet-stream"},
		{"xpm", "\x2F\x2A\x20\x58\x50\x4D\x20\x2A\x2F", "text/plain"},
		{"js", "#!/bin/node ", "text/javascript"},
		{"json", `{"a":"b", "c":[{"a":"b"},1,true,false,"abc"]}`, "application/json"},
		{"json issue#239", "{\x0A\x09\x09\"key\":\"val\"}\x0A", "application/json"},
		// json.{int,string}.txt contain a single JSON value. They are valid JSON
		// documents but they should not be detected as application/json. This mimics
		// the behaviour of the file utility and seems the correct thing to do.
		{"json.int.txt", "1", "application/octet-stream"},
		{"json.float.txt", "1.5", "text/plain"},
		{"json.string.txt", `"some string"`, "text/plain"},
		{"kml 2.2", `<?xml version="1.0"?><kml xmlns="http://www.opengis.net/kml/2.2">`, "text/xml"},
		{"kml 2.0", `<?xml version="1.0"?><kml xmlns="http://earth.google.com/kml/2.0">`, "text/xml"},
		{"kml 2.1", `<?xml version="1.0"?><kml xmlns="http://earth.google.com/kml/2.1">`, "text/xml"},
		{"kml 2.2", `<?xml version="1.0"?><kml xmlns="http://earth.google.com/kml/2.2">`, "text/xml"},
		{"kmz", "\x50\x4b\x03\x04\x14\x00\x00\x00\x08\x00\xe6\x6c\x04\x5b\xfd\xf4\xf2\x45\x41\x00\x00\x00\x43\x00\x00\x00\x07\x00\x1c\x00doc.kml", "application/vnd.google-earth.kmz"},
		{"lit", "ITOLITLS", "text/plain"},
		{"lotus1", "\x00\x00\x02\x00456\x00" + offset(13, ""), "application/vnd.lotus-1-2-3"},
		{"lotus2", "\x00\x00\x1a\x00" + offset(16, "\x01"), "application/vnd.lotus-1-2-3"},
		{"lua", "#!/usr/bin/lua\n", "text/x-lua"},
		{"lua space", "#! /usr/bin/lua", "text/x-lua"},
		{"lz", "\x4c\x5a\x49\x50", "application/x-lzip"},
		{"m3u", "#EXTM3U", "audio/x-mpegurl"},
		{"m4a", "\x00\x00\x00\x18ftypM4A ", "audio/x-m4a"},
		{"audio mp4 F4A", "\x00\x00\x00\x18ftypF4A ", "audio/mp4"},
		{"audio mp4 F4B", "\x00\x00\x00\x18ftypF4B ", "audio/mp4"},
		{"audio mp4 M4B", "\x00\x00\x00\x18ftypM4B ", "audio/mp4"},
		{"audio mp4 M4P", "\x00\x00\x00\x18ftypM4P ", "video/mp4"},
		{"audio mp4 MSNV", "\x00\x00\x00\x18ftypMSNV", "audio/mp4"},
		{"audio mp4 NDAS", "\x00\x00\x00\x18ftypNDAS", "audio/mp4"},
		{"lnk", "\x4C\x00\x00\x00\x01\x14\x02\x00", "application/octet-stream"},
		{"mdb", offset(4, "Standard Jet DB"), "application/x-msaccess"},
		{"midi", "\x4D\x54\x68\x64", "audio/midi"},
		{"mkv", "\x1a\x45\xdf\xa3\x01\x00\x00\x00\x00\x00\x00\x23\x42\x86\x81\x01\x42\xf7\x81\x01\x42\xf2\x81\x04\x42\xf3\x81\x08\x42\x82\x88\x6d\x61\x74\x72\x6f\x73\x6b\x61", "video/x-matroska"},
		{"mobi", offset(60, "BOOKMOBI"), "application/x-mobipocket-ebook"},
		{"mov", "\x00\x00\x00\x14\x66\x74\x79\x70\x71\x74\x20\x20", "video/quicktime"},
		{"mp3", "\x49\x44\x33\x03", "application/octet-stream"},
		{"mp3 v1 notag", "\xff\xfb\xc8\x00", "audio/mpeg"},
		{"mp3 v2.5 notag", "\xff\xe3\x18\xc4", "audio/mpeg"},
		{"mp3 v2 notag", "\xff\xf3\x82\xc4", "audio/mpeg"},
		{"mp4 1", "\x00\x00\x00\x18ftyp0000", "application/octet-stream"},
		{"mpc", "MPCK", "audio/x-musepack"},
		{"mpeg", "\x00\x00\x01\xba", "video/mpeg"},
		{"mqv", "\x00\x00\x00\x18ftypmqt ", "video/quicktime"},
		{"mrc", "00057     2200037   4500245001900000\x1e", "application/octet-stream"},
		{"msi", fromDisk("msi.msi"), "application/x-ole-storage"},
		{"msg", fromDisk("msg.msg"), "application/x-ole-storage"},
		{"ndjson", `{"key":"val"}` + "\n" + `{"key":"val"}`, "application/x-ndjson"},
		{"ndjson spaces", `{ "key" : "val" }` + "\n" + ` { "key" : "val" }`, "application/x-ndjson"},
		{"nes", "NES\x1a", "application/x-nes-rom"},
		{"elfobject", "\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01\x00", "application/x-object"},
		{"odf", "PK\x03\x04\x14\x00\x00\x08\x00\x00\xb1Z\xa8N\x07\x8a\xa8[*\x00\x00\x00*\x00\x00\x00\x08\x00\x00\x00mimetypeapplication/vnd.oasis.opendocument.formula", "application/vnd.oasis.opendocument.formula"},
		{"sxc", "PK\x03\x04\x14\x00\x00\x08\x00\x00\xbb\x03\x5eGE\xbc\x13\x94\x1c\x00\x00\x00\x1c\x00\x00\x00\x08\x00\x00\x00mimetypeapplication/vnd.sun.xml.calc", "application/vnd.sun.xml.calc"},
		{"odg", "PK\x03\x04\x14\x00\x00\x08\x00\x00\xcbY\xa8N\x9f\x03.\xc4\x2b\x00\x00\x00\x2b\x00\x00\x00\x08\x00\x00\x00mimetypeapplication/vnd.oasis.opendocument.graphics", "application/vnd.oasis.opendocument.graphics"},
		{"odp", "PK\x03\x04\x14\x00\x00\x08\x00\x00\xbdX\xa8N3&\xac\xa8/\x00\x00\x00/\x00\x00\x00\x08\x00\x00\x00mimetypeapplication/vnd.oasis.opendocument.presentation", "application/vnd.oasis.opendocument.presentation"},
		{"ods", "PK\x03\x04\x14\x00\x00\x08\x00\x00\x14V\xa8N\x85l9\x8a.\x00\x00\x00.\x00\x00\x00\x08\x00\x00\x00mimetypeapplication/vnd.oasis.opendocument.spreadsheet", "application/vnd.oasis.opendocument.spreadsheet"},
		{"odt", "PK\x03\x04\x14\x00\x00\x08\x00\x00\xbbP\xa8N\x5e\xc62\n'\x00\x00\x00'\x00\x00\x00\x08\x00\x00\x00mimetypeapplication/vnd.oasis.opendocument.text", "application/vnd.oasis.opendocument.text"},
		{"ogg", "OggS\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\xce\xc6AI\x00\x00\x00\x00py\xf3\x3d\x01\x1e\x01vorbis\x00\x00", "audio/ogg"},
		{"ogg", "OggS\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\x80\xbc\x81_\x00\x00\x00\x00\xd0\xfbP\x84\x01@fishead\x00\x03", "video/ogg"},
		{"ogg spx oga", "OggS\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\xc7w\xaa\x15\x00\x00\x00\x00V&\x88\x89\x01PSpeex   1", "audio/ogg"},
		{"one", "\xe4\x52\x5c\x7b\x8c\xd8\xa7\x4d\xae\xb1\x53\x78\xd0\x29\x96\xd3", "application/onenote"},
		{"otf", "OTTO\x00", "application/vnd.ms-opentype"},
		{"otg", "PK\x03\x04\x14\x00\x00\x08\x00\x00\xd1Y\xa8N\xdf%\xad\xe94\x00\x00\x004\x00\x00\x00\x08\x00\x00\x00mimetypeapplication/vnd.oasis.opendocument.graphics-template", "application/vnd.oasis.opendocument.graphics-template"},
		{"otp", "PK\x03\x04\x14\x00\x00\x08\x00\x00\xc4X\xa8N\xef\n\x14:8\x00\x00\x008\x00\x00\x00\x08\x00\x00\x00mimetypeapplication/vnd.oasis.opendocument.presentation-template", "application/vnd.oasis.opendocument.presentation-template"},
		{"ots", "PK\x03\x04\x14\x00\x00\x08\x00\x00\x1bV\xa8N{\x96\xa3N7\x00\x00\x007\x00\x00\x00\x08\x00\x00\x00mimetypeapplication/vnd.oasis.opendocument.spreadsheet-template", "application/vnd.oasis.opendocument.spreadsheet-template"},
		{"ott", "PK\x03\x04\x14\x00\x00\x08\x00\x00\xcfP\xa8N\xe4\x11\x92)0\x00\x00\x000\x00\x00\x00\x08\x00\x00\x00mimetypeapplication/vnd.oasis.opendocument.text-template", "application/vnd.oasis.opendocument.text-template"},
		{"odc", "PK\x03\x04\x14\x00\x00\x08\x00\x00zp2R\xab\xb8\xb2l(\x00\x00\x00(\x00\x00\x00\x08\x00\x00\x00mimetypeapplication/vnd.oasis.opendocument.chart", "application/vnd.oasis.opendocument.chart"},
		{"owl", `<?xml version="1.0"?><Ontology xmlns="http://www.w3.org/2002/07/owl#">`, "text/xml"},
		{"pat", "\x00\x00\x00\x1c\x00\x00\x00\x01\x00\x00\x00\x01\x00\x00\x00\x01\x00\x00\x00\x03GPAT", "image/x-gimp-pat"},
		{"pdf", "%PDF-", "application/pdf"},
		{"php", "#!/usr/bin/env php\n", "text/plain"},
		{"pl", "#!/usr/bin/perl\n", "text/x-perl"},
		{"png", "\x89PNG\x0d\x0a\x1a\x0a", "text/plain"},
		{"ppt", fromDisk("ppt.ppt"), "application/x-ole-storage"},
		{"pptx", fromDisk("pptx.pptx"), "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		{"ps", "%!PS-Adobe-", "application/postscript"},
		{"psd", "8BPS", "text/plain"},
		{"p7s_pem", "-----BEGIN PKCS7", "text/plain"},
		{"p7s_der", "\x30\x82\x01\x26\x06\x09\x2a\x86\x48\x86\xf7\x0d\x01\x07\x02\xa0\x82\x01\x17\x30", "application/octet-stream"},
		{"pub", fromDisk("pub.pub"), "application/x-ole-storage"},
		{"pam", fromDisk("pam.pam"), "image/x-portable-arbitrarymap"},
		{"pbm", fromDisk("pbm.pbm"), "image/x-portable-bitmap"},
		{"pfm", fromDisk("pfm.pfm"), "application/octet-stream"},
		{"pgm", fromDisk("pgm.pgm"), "image/x-portable-graymap"},
		{"ppm", fromDisk("ppm.ppm"), "image/x-portable-pixmap"},
		{"py", "#!/usr/bin/python\n", "text/x-script.python"},
		{"py3", "#!/usr/bin/env python3", "text/x-script.python"},
		{"qcp", "RIFF\xc0\xcf\x00\x00QLCMf", "application/octet-stream"},
		{"rar", "Rar!\x1a\x07\x01\x00", "application/vnd.rar"},
		{"rb", "#!/usr/local/bin/ruby\n", "text/x-ruby"},
		{"rmvb", ".RMF", "text/troff"},
		{"rpm", "\xed\xab\xee\xdb", "application/x-rpm"},
		{"rss", "\x3c\x3f\x78\x6d\x6c\x20\x76\x65\x72\x73\x69\x6f\x6e\x3d\x22\x31\x2e\x30\x22\x20\x65\x6e\x63\x6f\x64\x69\x6e\x67\x3d\x22\x55\x54\x46\x2d\x38\x22\x3f\x3e\x0a\x3c\x72\x73\x73", "text/xml"},
		{"rtf", "{\\rtf", "text/plain"},
		{"sh", "#!/bin/sh\n", "text/x-shellscript"},
		{"shp", fromDisk("shp.shp"), "application/octet-stream"},
		{"shx", "\x00\x00\x27\x0a", "application/octet-stream"},
		{"so", "\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x03\x00", "application/x-sharedlib"},
		{"sqlite", "SQLite format 3\x00", "application/octet-stream"},
		{"srt", "1\n00:02:16,612 --\x3e 00:02:19,376\nS", "application/x-subrip"},
		{"svg no xml header", `<svg xmlns="http://www.w3.org/2000/svg"`, "image/svg+xml"},
		{
			"svg xml header",
			`<?xml version="1.0" encoding="UTF-8" standalone="no"?>
<svg width="391" height="391" viewBox="-70.5 -70.5 391 391" xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink">
    <rect fill="#fff" stroke="#000" x="-70" y="-70" width="390" height="390"/>
</svg>
`,
			"image/svg+xml",
		},
		{
			"svg with comment prefix",
			`<svg xmlns="http://www.w3.org/2000/svg"`,
			"image/svg+xml",
		},

		{"swf", "CWS", "text/plain"},
		{"tar", fromDisk("tar.tar"), "application/x-tar"},
		{"tcl", "#!/usr/bin/tcl\n", "text/x-tcl"},
		{"tcx", `<?xml version="1.0"?><TrainingCenterDatabase xmlns="http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2">`, "text/xml"},
		{"tiff", "II*\x00", "image/tiff"},
		{"tsv", "a\t\"b\"\tc\n1\t2\t3", "text/plain"},
		{"ttc", "ttcf\x00\x01\x00\x00", "font/ttf"},
		{"ttf", "\x00\x01\x00\x00", "application/octet-stream"},
		{"tzfile", fromDisk("tzfile"), "application/octet-stream"},
		{"utf16bebom txt", "\xfe\xff\x00\x74\x00\x68\x00\x69\x00\x73", "text/plain"},
		{"utf16lebom txt", "\xff\xfe\x74\x00\x68\x00\x69\x00\x73\x00", "text/plain"},
		{"utf32bebom txt", "\x00\x00\xfe\xff\x00\x00\x00\x74\x00\x00\x00\x68\x00\x00\x00\x69\x00\x00\x00\x73", "text/plain"},
		{"utf32lebom txt", "\xff\xfe\x00\x00\x74\x00\x00\x00\x68\x00\x00\x00\x69\x00\x00\x00\x73\x00\x00\x00", "text/plain"},
		{"utf8 txt", fromDisk("utf8.txt"), "text/plain"},
		{"utf8ctrlchars", "\xef\xbf\xbd\xef\xbf\xbd\xef\xbf\xbd\xef\xbf\xbd\xef\xbf\xbd\x10", "application/octet-stream"},
		{"vcf", "BEGIN:VCARD\nV", "text/vcard"},
		{"vcf dos", "BEGIN:VCARD\r\nV", "text/vcard"},
		{"visio", "\x50\x4b\x03\x04\x14\x00\x00\x00\x00\x00\x83\x93\x11\x5b\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x06\x00\x20\x00visio/", "application/vnd.ms-visio.drawing.main+xml"},
		{"voc", "Creative Voice File", "audio/x-unknown"},
		{"vtt", "WEBVTT", "text/plain"},
		{"warc", "WARC/1.1", "application/warc"},
		{"wasm", "\x00asm", "application/octet-stream"},
		{"wav", "RIFF\xba\xa5\x04\x00WAVEf", "audio/x-wav"},
		{"webm", "\x1aE\xdf\xa3\x01\x00\x00\x00\x00\x00\x00\x1fB\x86\x81\x01B\xf7\x81\x01B\xf2\x81\x04B\xf3\x81\x08B\x82\x84webm", "video/webm"},
		{"webp", "RIFFhv\x00\x00WEBPV", "image/webp"},
		{"woff", "wOFF", "font/woff"},
		{"woff2", "wOF2", "font/woff2"},
		{"x3d", `<?xml version="1.0"?><X3D xmlns:xsd="http://www.w3.org/2001/XMLSchema-instance">`, "text/xml"},
		{"xar", "xar!", "application/x-xar"},
		{"xcf", "gimp xcf", "image/x-xcf"},
		{"xfdf", `<?xml version="1.0"?><xfdf xmlns="http://ns.adobe.com/xfdf/">`, "text/xml"},
		{"xhtml1", `<?xml version="1.0"?><!DOCTYPE html`, "application/xhtml+xml"},
		{"xhtml2", `<?xml version="1.0"?><HtMl 	XMLNS=`, "application/xhtml+xml"},
		{"xlf", `<?xml version="1.0"?><xliff xmlns="urn:oasis:names:tc:xliff:document:1.2">`, "text/xml"},
		{"xls", fromDisk("xls.xls"), "application/x-ole-storage"},
		{"xlsx", fromDisk("xlsx.xlsx"), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"xml", "<?xml ", "text/xml"},
		{"xml withbr", "\x0D\x0A<?xml ", "text/plain"},
		{"xz", "\xfd7zXZ\x00", "application/x-xz"},
		{"zip", "PK\x03\x04", "application/octet-stream"},
		{"zst", "(\xb5/\xfd", "application/zstd"},
		{"zst skippable frame", "\x50\x2A\x4D\x18", "application/octet-stream"},
	}
}

// offset prepends n nul bytes to s.
func offset(n int, s string) string {
	prepend := make([]byte, n)
	return string(prepend) + s
}

func fromDisk(path string) string {
	data, err := os.ReadFile("testdata/" + path)
	if err != nil {
		panic(err)
	}
	return string(data)
}
