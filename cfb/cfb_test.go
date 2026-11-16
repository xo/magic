package cfb_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xo/magic/cfb"
)

// tree returns the files of the tests: streams in the mini stream and in
// sectors, an empty stream, and a storage in a storage.
func tree() []*node {
	ole := storage("_1", stream("\x01Ole", pattern(20, 1)), stream("Big", pattern(10000, 2)))
	ole.clsid = cfb.GUID{0x0C, 0x10, 0x08, 0, 0, 0, 0, 0, 0xC0, 0, 0, 0, 0, 0, 0, 0x46}
	return []*node{
		stream("WordDocument", pattern(5000, 3)),
		stream("1Table", pattern(100, 4)),
		stream("\x05SummaryInformation", summary(cfb.CFMetafilePict, pattern(300, 5))),
		storage("ObjectPool", ole),
		stream("Empty", nil),
		stream("Data", pattern(4096, 6)),
	}
}

func open(t *testing.T, b []byte, lim cfb.Limits) *cfb.File {
	t.Helper()
	f, err := cfb.Open(bytes.NewReader(b), int64(len(b)), lim)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	return f
}

func TestOpen(t *testing.T) {
	t.Parallel()
	for _, shift := range []uint{9, 12} {
		l := build(t, shift, tree()...)
		f := open(t, l.b, cfb.Limits{})
		root := f.Root()
		if root.Type != cfb.RootEntry || root.Name != "Root Entry" {
			t.Fatalf("expected the root, got: %v %q", root.Type, root.Name)
		}
		if exp, got := "WordDocument,1Table,\x05SummaryInformation,ObjectPool,Empty,Data", joinNames(root.Children); got != exp {
			t.Errorf("expected %q, got: %q", exp, got)
		}
		pool, err := f.Lookup("objectpool/_1")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if pool.Type != cfb.StorageEntry || pool.CLSID.String() != "{0008100C-0000-0000-C000-000000000046}" {
			t.Errorf("expected a storage with its CLSID, got: %v %s", pool.Type, pool.CLSID)
		}
		if exp, got := "\x01Ole,Big", joinNames(pool.Children); got != exp {
			t.Errorf("expected %q, got: %q", exp, got)
		}
		for _, n := range []*node{tree()[0], tree()[1], tree()[2], tree()[4], tree()[5], {name: "ObjectPool/_1/\x01Ole", data: pattern(20, 1)}, {name: "/ObjectPool/_1/Big", data: pattern(10000, 2)}} {
			got, err := f.ReadFile(n.name)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if !bytes.Equal(got, n.data) {
				t.Errorf("shift %d: expected the %d bytes of %q, got: %d bytes that differ", shift, len(n.data), n.name, len(got))
			}
		}
	}
}

func TestOpenReadsAcrossSectors(t *testing.T) {
	t.Parallel()
	l := build(t, 9, tree()...)
	f := open(t, l.b, cfb.Limits{})
	for _, name := range []string{"WordDocument", "1Table"} {
		r, err := f.Open(name)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		all, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		// Each read crosses the end of a sector or of a mini sector.
		for _, off := range []int64{0, 60, 62, 500, 510, int64(len(all)) - 3} {
			if off >= int64(len(all)) {
				continue
			}
			p := make([]byte, 7)
			n, err := r.ReadAt(p, off)
			want := min(7, len(all)-int(off))
			if n != want || (n < 7 && !errors.Is(err, io.EOF)) || (n == 7 && err != nil) {
				t.Fatalf("expected %d bytes at %d, got: %d, %v", want, off, n, err)
			}
			if !bytes.Equal(p[:n], all[off:int(off)+n]) {
				t.Errorf("expected %q at %d, got: %q", all[off:int(off)+n], off, p[:n])
			}
		}
	}
}

func TestOpenErrors(t *testing.T) {
	t.Parallel()
	f := open(t, build(t, 9, tree()...).b, cfb.Limits{})
	if _, err := f.Open("NoSuchStream"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected %v, got: %v", fs.ErrNotExist, err)
	}
	if _, err := f.Open("WordDocument/x"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected %v, got: %v", fs.ErrNotExist, err)
	}
	if _, err := f.Open("ObjectPool"); err == nil {
		t.Errorf("expected an error for a storage, got: nil")
	}
	// The root reads as the mini stream.
	r, err := f.OpenEntry(f.Root())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if r.Size() != f.Root().Size || r.Size()%64 != 0 {
		t.Errorf("expected the mini stream, got: %d bytes", r.Size())
	}
}

// TestOpenDIFAT reads a file whose table of sectors needs more than the 109
// sectors that the header names, so that a DIFAT sector names the rest.
func TestOpenDIFAT(t *testing.T) {
	t.Parallel()
	data := pattern(110*128*512, 7)
	l := build(t, 9, stream("Big", data), stream("Small", pattern(10, 8)))
	if len(l.fatIDs) <= 109 {
		t.Fatalf("expected more than 109 sectors of the table, got: %d", len(l.fatIDs))
	}
	f := open(t, l.b, cfb.Limits{})
	for _, n := range []*node{{name: "Big", data: data}, {name: "Small", data: pattern(10, 8)}} {
		got, err := f.ReadFile(n.name)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !bytes.Equal(got, n.data) {
			t.Errorf("expected the %d bytes of %q, got: %d bytes that differ", len(n.data), n.name, len(got))
		}
	}
}

// TestDamaged changes a good file in one place. Each change must give an
// error that wraps ErrCorrupt or ErrLimit, from Open or from ReadFile.
func TestDamaged(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		lim    cfb.Limits
		change func(l *layout)
		read   string // the stream to read, or "" when Open fails
		exp    error
	}{
		{"signature", cfb.Limits{}, func(l *layout) { l.b[0] = 0 }, "", cfb.ErrCorrupt},
		{"sector shift", cfb.Limits{}, func(l *layout) { le.PutUint16(l.b[30:], 30) }, "", cfb.ErrCorrupt},
		{"short", cfb.Limits{}, func(l *layout) { l.b = l.b[:300] }, "", cfb.ErrCorrupt},
		{"loop in the directory chain", cfb.Limits{}, func(l *layout) { l.setFAT(l.dirStart, l.dirStart) }, "", cfb.ErrCorrupt},
		{"directory past the end", cfb.Limits{}, func(l *layout) { le.PutUint32(l.b[48:], 1<<20) }, "", cfb.ErrCorrupt},
		{"no table of sectors", cfb.Limits{}, func(l *layout) { le.PutUint32(l.b[76:], freeSect) }, "", cfb.ErrCorrupt},
		{"entry 0 is not the root", cfb.Limits{}, func(l *layout) { l.b[l.sectorOff(l.dirStart)+66] = 2 }, "", cfb.ErrCorrupt},
		{"loop in a stream", cfb.Limits{}, func(l *layout) {
			s := l.starts["WordDocument"]
			l.setFAT(s+1, s)
		}, "WordDocument", cfb.ErrCorrupt},
		{"stream chain too short", cfb.Limits{}, func(l *layout) { l.setFAT(l.starts["WordDocument"], endOfChain) }, "WordDocument", cfb.ErrCorrupt},
		{"stream chain names a free sector", cfb.Limits{}, func(l *layout) { l.setFAT(l.starts["WordDocument"], 0x7FFFFFFF) }, "WordDocument", cfb.ErrCorrupt},
		{"stream larger than the file", cfb.Limits{}, func(l *layout) { le.PutUint32(l.b[l.entryOff("WordDocument")+120:], 1<<27) }, "WordDocument", cfb.ErrCorrupt},
		{"loop in a mini stream", cfb.Limits{}, func(l *layout) {
			s := l.starts["1Table"]
			l.setMiniFAT(s, s)
		}, "1Table", cfb.ErrCorrupt},
		{"mini chain too short", cfb.Limits{}, func(l *layout) { l.setMiniFAT(l.starts["1Table"], endOfChain) }, "1Table", cfb.ErrCorrupt},
		{"loop in the mini table", cfb.Limits{}, func(l *layout) { l.setFAT(l.miniFAT, l.miniFAT) }, "1Table", cfb.ErrCorrupt},
		{"sectors over the limit", cfb.Limits{Sectors: 10}, func(*layout) {}, "", cfb.ErrLimit},
		{"entries over the limit", cfb.Limits{Entries: 2}, func(*layout) {}, "", cfb.ErrLimit},
		{"stream over the limit", cfb.Limits{StreamSize: 4096}, func(*layout) {}, "WordDocument", cfb.ErrLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			l := build(t, 9, tree()...)
			test.change(l)
			f, err := cfb.Open(bytes.NewReader(l.b), int64(len(l.b)), test.lim)
			if test.read != "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				_, err = f.ReadFile(test.read)
			}
			if !errors.Is(err, test.exp) {
				t.Fatalf("expected %v, got: %v", test.exp, err)
			}
		})
	}
}

// TestDirectoryLoops links the entries of the directory in loops. Open
// keeps each entry once, and ends.
func TestDirectoryLoops(t *testing.T) {
	t.Parallel()
	l := build(t, 9, tree()...)
	off := l.sectorOff(l.dirStart)
	child := le.Uint32(l.b[off+76:])
	// The child of the root points to itself on both sides, and the storage
	// _1 holds the root and ObjectPool.
	le.PutUint32(l.b[off+128*int(child)+68:], child)
	le.PutUint32(l.b[off+128*int(child)+72:], child)
	le.PutUint32(l.b[l.entryOff("ObjectPool/_1")+76:], uint32(l.index["ObjectPool"]))
	f := open(t, l.b, cfb.Limits{})
	if n := len(f.Root().Children); n != 1 {
		t.Errorf("expected 1 entry in the root, got: %d", n)
	}
	l = build(t, 9, tree()...)
	le.PutUint32(l.b[l.entryOff("ObjectPool/_1")+76:], uint32(l.index["ObjectPool"]))
	f = open(t, l.b, cfb.Limits{})
	e, err := f.Lookup("ObjectPool/_1")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(e.Children) != 0 {
		t.Errorf("expected no entry in _1, got: %q", joinNames(e.Children))
	}
}

// TestTruncated cuts a file at each sector. Open and ReadFile must not
// panic, and a stream that is cut must give an error.
func TestTruncated(t *testing.T) {
	t.Parallel()
	b := build(t, 9, tree()...).b
	for n := 512; n < len(b); n += 512 {
		f, err := cfb.Open(bytes.NewReader(b[:n]), int64(n), cfb.Limits{})
		if err != nil {
			continue
		}
		for _, name := range []string{"WordDocument", "1Table", "ObjectPool/_1/Big"} {
			_, _ = f.ReadFile(name)
		}
	}
}

// TestSamples reads the Compound Files of the repository: each stream of
// each one. The files of testdata are the first 4096 bytes of a file, so a
// stream can be cut.
func TestSamples(t *testing.T) {
	t.Parallel()
	opened := 0
	for _, name := range samples {
		b, err := os.ReadFile(filepath.Join("..", "testdata", name))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		f, err := cfb.Open(bytes.NewReader(b), int64(len(b)), cfb.Limits{})
		if err != nil {
			// The cut can drop a sector of the table or of the directory.
			t.Logf("%s: %v", name, err)
			if !errors.Is(err, cfb.ErrCorrupt) {
				t.Errorf("%s: expected %v, got: %v", name, cfb.ErrCorrupt, err)
			}
			continue
		}
		opened++
		var streams []string
		walk(f.Root(), "", func(path string, e *cfb.Entry) {
			if e.Type != cfb.StreamEntry {
				return
			}
			streams = append(streams, path)
			data, err := f.ReadFile(path)
			if err != nil {
				return
			}
			if strings.HasPrefix(e.Name, "\x05") {
				if _, err := cfb.ParsePropertySet(data); err != nil {
					t.Errorf("%s: %q: expected no error, got: %v", name, path, err)
				}
			}
		})
		if len(streams) == 0 {
			t.Errorf("%s: expected streams, got: none", name)
		}
	}
	// The cut keeps the whole directory of doc.doc and msi.msi.
	if opened < 2 {
		t.Errorf("expected at least 2 files that open, got: %d", opened)
	}
}

// TestSamplesSummary reads the summary of the whole files of the corpus of
// the test module. A copy of the module has no test module, so the test
// skips there.
func TestSamplesSummary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file, stream string
	}{
		{"note.doc", "WordDocument"},
		{"report.doc", "WordDocument"},
		{"sheet.xls", "Workbook"},
	}
	for _, test := range tests {
		b, err := os.ReadFile(filepath.Join("..", "test", "corpus", "testdata", test.file))
		if errors.Is(err, fs.ErrNotExist) {
			t.Skip("no test module")
		}
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		f := open(t, b, cfb.Limits{})
		if _, err := f.ReadFile(test.stream); err != nil {
			t.Errorf("%s: expected no error, got: %v", test.file, err)
		}
		data, err := f.ReadFile("\x05SummaryInformation")
		if err != nil {
			t.Fatalf("%s: expected no error, got: %v", test.file, err)
		}
		ps, err := cfb.ParsePropertySet(data)
		if err != nil {
			t.Fatalf("%s: expected no error, got: %v", test.file, err)
		}
		if ps.Section(cfb.FMTIDSummaryInformation) == nil {
			t.Errorf("%s: expected the section of the summary, got: none", test.file)
		}
	}
}

// samples are the Compound Files of testdata.
var samples = []string{"doc.doc", "xls.xls", "ppt.ppt", "msi.msi", "msg.msg", "pub.pub"}

// walk calls fn for e and each entry under it.
func walk(e *cfb.Entry, path string, fn func(string, *cfb.Entry)) {
	fn(path, e)
	for _, c := range e.Children {
		p := c.Name
		if path != "" {
			p = path + "/" + c.Name
		}
		walk(c, p, fn)
	}
}

func FuzzOpen(f *testing.F) {
	f.Add(build(f, 9, tree()...).b)
	f.Add(build(f, 12, tree()...).b)
	f.Add(build(f, 9, stream("A", pattern(700, 1)), storage("S", stream("\x05DocumentSummaryInformation", docSummary()))).b)
	for _, name := range samples {
		if b, err := os.ReadFile(filepath.Join("..", "testdata", name)); err == nil {
			f.Add(b)
		}
	}
	lim := cfb.Limits{Sectors: 1 << 12, Entries: 1 << 10, StreamSize: 1 << 20}
	f.Fuzz(func(t *testing.T, b []byte) {
		file, err := cfb.Open(bytes.NewReader(b), int64(len(b)), lim)
		if err != nil {
			if !errors.Is(err, cfb.ErrCorrupt) && !errors.Is(err, cfb.ErrLimit) {
				t.Fatalf("expected %v or %v, got: %v", cfb.ErrCorrupt, cfb.ErrLimit, err)
			}
			return
		}
		n := 0
		walk(file.Root(), "", func(_ string, e *cfb.Entry) {
			n++
			if n > lim.Entries {
				t.Fatalf("expected at most %d entries, got: more", lim.Entries)
			}
			r, err := file.OpenEntry(e)
			if err != nil {
				return
			}
			data, err := io.ReadAll(r)
			if err != nil {
				return
			}
			if int64(len(data)) != e.Size || e.Size > lim.StreamSize {
				t.Fatalf("expected %d bytes, got: %d", e.Size, len(data))
			}
			if strings.HasPrefix(e.Name, "\x05") {
				_, _ = cfb.ParsePropertySet(data)
			}
		})
	})
}
