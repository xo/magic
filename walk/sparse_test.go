package walk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOutputWriteAt writes at random offsets, appends and sets the length
// of a file, in memory and on disk, and compares the file with a slice that
// gets the same writes. Only the bytes of the writes count against the
// limit.
func TestOutputWriteAt(t *testing.T) {
	t.Parallel()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer func() { _ = root.Close() }()
	rng := rand.New(rand.NewPCG(1, 2))
	for run := range 200 {
		mem := newMemSink()
		disk := &rootSink{root: root}
		name := "f" + string(rune('a'+run%26)) + string(rune('a'+run/26))
		outs := []*Output{newOutput(mem, 1<<20), newOutput(disk, 1<<20)}
		var exp []byte
		written := int64(0)
		for _, o := range outs {
			if !o.CreateFile(name, nil) {
				t.Fatalf("expected the file to be made")
			}
		}
		for range 1 + rng.IntN(12) {
			b := make([]byte, rng.IntN(40))
			for i := range b {
				b[i] = byte(1 + rng.IntN(255))
			}
			var ok bool
			switch op := rng.IntN(4); op {
			case 0, 1:
				off := int64(rng.IntN(300))
				for _, o := range outs {
					ok = o.WriteFileAt(name, b, off)
				}
				if len(b) > 0 {
					if end := int(off) + len(b); end > len(exp) {
						exp = append(exp, make([]byte, end-len(exp))...)
					}
					copy(exp[off:], b)
				}
				written += int64(len(b))
			case 2:
				for _, o := range outs {
					ok = o.AppendFile(name, b)
				}
				exp = append(exp, b...)
				written += int64(len(b))
			case 3:
				size := rng.IntN(300)
				for _, o := range outs {
					ok = o.TruncateFile(name, int64(size))
				}
				if size > len(exp) {
					exp = append(exp, make([]byte, size-len(exp))...)
				}
				exp = exp[:size]
			}
			if !ok {
				t.Fatalf("run %d: expected the write to pass", run)
			}
		}
		for _, o := range outs {
			if o.budget.n != written {
				t.Errorf("run %d: expected %d bytes counted, got: %d", run, written, o.budget.n)
			}
		}
		mem.settle()
		m := mem.members[name]
		if m.Size != int64(len(exp)) {
			t.Errorf("run %d: expected size %d, got: %d", run, len(exp), m.Size)
		}
		r, err := m.Open()
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if b, err := io.ReadAll(r); err != nil || !bytes.Equal(b, exp) {
			t.Errorf("run %d: expected %x, got: %x, %v", run, exp, b, err)
		}
		if b := m.bytes(); !bytes.Equal(b, exp) {
			t.Errorf("run %d: expected %x, got: %x", run, exp, b)
		}
		// Small reads at each offset cross the holes and the extents.
		sr := io.NewSectionReader(memberReader{m}, 0, m.Size)
		for off := range len(exp) {
			p := make([]byte, 1+rng.IntN(16))
			n, _ := sr.ReadAt(p, int64(off))
			if !bytes.Equal(p[:n], exp[off:min(len(exp), off+len(p))]) {
				t.Errorf("run %d: at %d: expected %x, got: %x", run, off, exp[off:min(len(exp), off+len(p))], p[:n])
			}
		}
		if b, err := root.ReadFile(name); err != nil || !bytes.Equal(b, exp) {
			t.Errorf("run %d: on disk: expected %x, got: %x, %v", run, exp, b, err)
		}
	}
}

// TestOutputWriteAtFails holds that a write at an offset needs a regular
// file that exists, and an offset that is not negative, and that the limit
// counts the bytes that it writes.
func TestOutputWriteAtFails(t *testing.T) {
	t.Parallel()
	o := newOutput(newMemSink(), 10)
	if o.WriteFileAt("none", []byte("x"), 0) || o.TruncateFile("none", 1) {
		t.Errorf("expected a write to a file that does not exist to fail")
	}
	if !o.CreateDirectory("d") || o.WriteFileAt("d", []byte("x"), 0) || o.TruncateFile("d", 1) {
		t.Errorf("expected a write to a folder to fail")
	}
	if !o.CreateFile("f", nil) || o.WriteFileAt("f", []byte("x"), -1) || o.TruncateFile("f", -1) {
		t.Errorf("expected a negative offset to fail")
	}
	if !o.WriteFileAt("f", make([]byte, 6), 1<<40) || o.WriteFileAt("f", make([]byte, 6), 0) {
		t.Errorf("expected 6 bytes after a hole of 1 TiB to pass, then the limit to fail")
	}
}

// holeSig returns a signature whose extractor writes a file of size bytes
// that holds only "INNR" at its end, after a hole.
func holeSig(name, magic string, size int64) *Signature {
	s := sigAt(name, magic, High)
	s.Extract = func(_ []byte, _ int, out *Output) Extraction {
		if out == nil {
			return Extraction{OK: true}
		}
		ok := out.CreateFile("sparse", nil) && out.WriteFileAt("sparse", []byte("INNR"), size-4)
		return Extraction{OK: ok}
	}
	return s
}

// TestSparseMember extracts a file of 64 MiB that holds 4 bytes, under a
// limit of 16 bytes: in memory, in the walk of members, and on disk as a
// sparse file. The walk does not read a member over the limit of the input.
func TestSparseMember(t *testing.T) {
	t.Parallel()
	const size = 64 << 20
	Register(holeSig("sparse_outer", "SPRS", size), sigAt("sparse_inner", "INNR", High))
	ctx := context.Background()
	data := []byte("SPRS....")
	formats := WithFormats("sparse_outer", "sparse_inner")
	var got []string
	var outer *Entry
	for e, err := range WalkBytes(ctx, data, formats, WithMaxOutput(16), WithDepth(1)) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if e.Format == "sparse_outer" {
			outer = e
		}
		got = append(got, e.Format+" "+e.Member)
	}
	if exp := []string{"sparse_outer ", "sparse_inner sparse"}; strings.Join(got, ",") != strings.Join(exp, ",") {
		t.Errorf("expected %q, got: %q", exp, got)
	}
	for m, err := range outer.Members(ctx) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if m.Size != size {
			t.Errorf("expected %d, got: %d", size, m.Size)
		}
		r, _ := m.Open()
		n, zeros, err := countZeros(r)
		if err != nil || n != size || zeros != size-4 {
			t.Errorf("expected %d bytes with %d zeros, got: %d with %d, %v", size, size-4, n, zeros, err)
		}
	}

	// A member over the limit of the input is not walked, with an error.
	var errs []error
	for _, err := range WalkBytes(ctx, data, formats, WithMaxOutput(16), WithDepth(1), WithMaxInput(1<<20)) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "above the limit") {
		t.Errorf("expected one error for the member, got: %v", errs)
	}

	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer func() { _ = root.Close() }()
	if err := Extract(ctx, root, outer, WithMaxOutput(16)); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	f, err := os.Open(filepath.Join(dir, "sparse"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer func() { _ = f.Close() }()
	n, zeros, err := countZeros(f)
	if err != nil || n != size || zeros != size-4 {
		t.Errorf("expected %d bytes with %d zeros, got: %d with %d, %v", size, size-4, n, zeros, err)
	}
	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	used, ok := allocated(fi)
	if !ok || used >= size {
		t.Skipf("the file system of %s does not report a sparse file", dir)
	}
	if used > 1<<20 {
		t.Errorf("expected at most %d bytes on disk, got: %d", 1<<20, used)
	}
}

// countZeros reads r to its end, and returns its bytes and its zeros.
func countZeros(r io.Reader) (int64, int64, error) {
	var n, zeros int64
	buf := make([]byte, 1<<20)
	for {
		k, err := r.Read(buf)
		n += int64(k)
		zeros += int64(k - countNonZero(buf[:k]))
		if errors.Is(err, io.EOF) {
			return n, zeros, nil
		}
		if err != nil {
			return n, zeros, fmt.Errorf("reading: %w", err)
		}
	}
}

// countNonZero returns the bytes of b that are not zero.
func countNonZero(b []byte) int {
	n := 0
	for _, c := range b {
		if c != 0 {
			n++
		}
	}
	return n
}
