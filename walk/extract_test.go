package walk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestOutputPath holds the cleaning of paths of binwalk.
func TestOutputPath(t *testing.T) {
	t.Parallel()
	o := newOutput(newMemSink(), 1<<20)
	for name, exp := range map[string]string{
		"a/b":              "a/b",
		"/abs/x":           "abs/x",
		"a/../b":           "b",
		"../../etc/passwd": "etc/passwd",
		"a//b/./c":         "a/b/c",
		"./x/./":           "x",
		"":                 "",
	} {
		if got := o.path(name); got != exp {
			t.Errorf("%q: expected %q, got: %q", name, exp, got)
		}
	}
}

// TestOutputSymlink holds the links of binwalk-ng: relative, and inside the
// output.
func TestOutputSymlink(t *testing.T) {
	t.Parallel()
	s := newMemSink()
	o := newOutput(s, 1<<20)
	if !o.CreateDirectory("usr/sbin") {
		t.Fatalf("expected the folder to be made")
	}
	for _, l := range [][3]string{
		{"usr/sbin/ls", "/bin/busybox", "../../bin/busybox"},
		{"link", "x", "x"},
		{"usr/up", "../../../../etc/passwd", "../etc/passwd"},
	} {
		if !o.CreateSymlink(l[0], l[1]) {
			t.Fatalf("%s: expected the link to be made", l[0])
		}
		if got := s.members[l[0]].Link; got != l[2] {
			t.Errorf("%s -> %s: expected %q, got: %q", l[0], l[1], l[2], got)
		}
	}
}

// TestOutputFollowsLinks holds that a path follows the links that the
// output holds, as resolve_in_chroot of binwalk-ng, and stays inside it.
func TestOutputFollowsLinks(t *testing.T) {
	t.Parallel()
	s := newMemSink()
	o := newOutput(s, 1<<20)
	if !o.CreateDirectory("real") || !o.CreateSymlink("dir", "real") || !o.CreateSymlink("file", "real/f") {
		t.Fatalf("expected the folder and the links to be made")
	}
	if !o.CreateFile("dir/a", []byte("a")) || s.members["real/a"] == nil {
		t.Errorf("expected dir/a to be written as real/a")
	}
	if !o.AppendFile("file", []byte("f")) || s.members["real/f"] == nil {
		t.Errorf("expected file to be written as real/f")
	}
	s.add(&Member{Name: "out", Mode: fs.ModeSymlink | 0o777, Link: "../x"})
	s.add(&Member{Name: "abs", Mode: fs.ModeSymlink | 0o777, Link: "/etc"})
	s.add(&Member{Name: "loop", Mode: fs.ModeSymlink | 0o777, Link: "loop"})
	for _, name := range []string{"out/a", "abs/a", "loop/a"} {
		if o.CreateFile(name, nil) {
			t.Errorf("%s: expected the write to fail", name)
		}
	}
	if !o.SetOwnership("out", 0, 0) || !o.SetMode("real/a", 0o600) || s.members["real/a"].Mode.Perm() != 0o600 {
		t.Errorf("expected the owner and the mode to be set")
	}
}

// TestOutputLimit holds that an output stops at its limit.
func TestOutputLimit(t *testing.T) {
	t.Parallel()
	o := newOutput(newMemSink(), 10)
	if !o.AppendFile("a", make([]byte, 6)) || o.AppendFile("a", make([]byte, 6)) || o.CreateFile("a", nil) {
		t.Errorf("expected 6 bytes to pass, then the limit and the existing file to fail")
	}
}

// extractSig returns a signature whose extractor writes a folder, a file
// that holds the match, a device and a link.
func extractSig(name, magic string) *Signature {
	s := sigAt(name, magic, High)
	s.Extract = func(data []byte, off int, out *Output) Extraction {
		if out == nil {
			return Extraction{OK: true, Size: 4, HasSize: true}
		}
		ok := out.CreateDirectory("d") &&
			out.CarveFile("d/inner.bin", data, off, 4) &&
			out.CreateCharDevice("dev", 1, 3) &&
			out.CreateSymlink("d/link", "/dev")
		return Extraction{OK: ok, Size: 4, HasSize: true}
	}
	return s
}

// TestMembersAndExtract extracts a match into memory and onto disk.
func TestMembersAndExtract(t *testing.T) {
	t.Parallel()
	Register(extractSig("extract_test", "XTRT"))
	data := []byte("..XTRT..")
	var e *Entry
	for got, err := range WalkBytes(context.Background(), data, WithFormats("extract_test")) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		e = got
	}
	if e == nil || !e.Extractable() {
		t.Fatalf("expected an extractable entry, got: %v", e)
	}
	var got []string
	for m, err := range e.Members(context.Background()) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		desc := fmt.Sprintf("%s %v", m.Name, m.Mode.Type())
		if m.Mode.IsRegular() {
			r, _ := m.Open()
			b, _ := io.ReadAll(r)
			desc += " " + string(b)
		}
		if m.Link != "" {
			desc += " -> " + m.Link
		}
		got = append(got, desc)
	}
	exp := []string{"d d---------", "d/inner.bin ---------- XTRT", "dev ---------- c 1 3", "d/link L--------- -> ../dev"}
	if !slices.Equal(got, exp) {
		t.Errorf("expected %q, got: %q", exp, got)
	}

	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer func() { _ = root.Close() }()
	if err := Extract(context.Background(), root, e); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "d", "inner.bin"))
	if err != nil || string(b) != "XTRT" {
		t.Errorf("expected %q, got: %q, %v", "XTRT", b, err)
	}
	if l, err := os.Readlink(filepath.Join(dir, "d", "link")); err != nil || filepath.ToSlash(l) != "../dev" {
		t.Errorf("expected %q, got: %q, %v", "../dev", l, err)
	}
	// A second extraction finds the files there, fails, and removes what it
	// wrote.
	if err := Extract(context.Background(), root, e); !errors.Is(err, ErrNotExtracted) {
		t.Errorf("expected %v, got: %v", ErrNotExtracted, err)
	}
}

// TestWalkDepth walks the members of a match: a match inside the file that
// an extraction writes.
func TestWalkDepth(t *testing.T) {
	t.Parallel()
	outer := sigAt("depth_outer", "OUTR", High)
	outer.Extract = func(_ []byte, _ int, out *Output) Extraction {
		if out == nil {
			return Extraction{OK: true}
		}
		return Extraction{OK: out.CreateFile("inner", []byte("..INNR....")) && out.CreateFile("empty", nil)}
	}
	Register(outer, sigAt("depth_inner", "INNR", High))
	data := []byte("OUTR....")
	for depth, exp := range map[int][]string{
		0: {"0 depth_outer 0 "},
		1: {"0 depth_outer 0 ", "1 depth_inner 2 inner"},
	} {
		var got []string
		for e, err := range WalkBytes(context.Background(), data, WithFormats("depth_outer", "depth_inner"), WithDepth(depth)) {
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			got = append(got, fmt.Sprintf("%d %s %d %s", e.Depth, e.Format, e.Offset, e.Member))
		}
		if !slices.Equal(got, exp) {
			t.Errorf("depth %d: expected %q, got: %q", depth, exp, got)
		}
	}
}

// TestModes holds the modes of the members.
func TestModes(t *testing.T) {
	t.Parallel()
	s := newMemSink()
	o := newOutput(s, 100)
	if !o.CreateFile("x", []byte("1")) || !o.MakeExecutable("x") {
		t.Fatalf("expected the file to be made executable")
	}
	if m := s.members["x"].Mode; m != 0o645 {
		t.Errorf("expected %v, got: %v", fs.FileMode(0o645), m)
	}
}

// TestOutputSub holds that the links of a sub-output are relative to its
// folder, as with a second Chroot of binwalk.
func TestOutputSub(t *testing.T) {
	t.Parallel()
	s := newMemSink()
	o := newOutput(s, 100)
	sub := o.Sub("vol")
	ok := o.CreateDirectory("vol/links/bin") &&
		sub.CreateDirectory("bin") &&
		sub.CreateFile("bin/busybox", []byte("x")) &&
		sub.CreateSymlink("links/bin/sh", "/bin/busybox")
	if !ok {
		t.Fatalf("expected the files and the link to be made")
	}
	if got := s.members["vol/links/bin/sh"].Link; got != "../../bin/busybox" {
		t.Errorf("expected %q, got: %q", "../../bin/busybox", got)
	}
	if _, ok := s.members["vol/bin/busybox"]; !ok {
		t.Errorf("expected vol/bin/busybox to exist")
	}
}

// TestCarve holds that an extractor of a signature with Carve gets the data
// up to the end of the match only.
func TestCarve(t *testing.T) {
	t.Parallel()
	s := &Signature{
		Name:  "carve_test",
		Carve: true,
		Magic: [][]byte{[]byte("CRV")},
		Parse: func(_ []byte, off int) (Match, error) {
			return Match{Offset: off, Confidence: Low}, nil
		},
		Extract: func(data []byte, off int, out *Output) Extraction {
			return Extraction{OK: out == nil || out.CreateFile("rest", data[off:])}
		},
	}
	Register(s, sigAt("carve_next", "NXT", High))
	data := []byte("..CRV.....NXT.....")
	for e, err := range WalkBytes(context.Background(), data, WithFormats("carve_test", "carve_next")) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if e.Format != "carve_test" {
			continue
		}
		for m, err := range e.Members(context.Background()) {
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			r, _ := m.Open()
			b, _ := io.ReadAll(r)
			if string(b) != "CRV....." {
				t.Errorf("expected %q, got: %q", "CRV.....", b)
			}
		}
	}
}

// TestCarveRetry holds that a carved extraction that fails runs again on
// the data to the end of the input, as extract of binwalk-ng, and that the
// second run starts with an empty output.
func TestCarveRetry(t *testing.T) {
	t.Parallel()
	s := &Signature{
		Name:  "carve_retry",
		Carve: true,
		Magic: [][]byte{[]byte("RTY")},
		Parse: func(_ []byte, off int) (Match, error) {
			return Match{Offset: off, Size: 4, Confidence: Low}, nil
		},
		Extract: func(data []byte, off int, out *Output) Extraction {
			if out == nil {
				return Extraction{OK: true}
			}
			ok := out.CreateFile("rest", data[off:])
			return Extraction{OK: ok && len(data)-off > 4}
		},
	}
	Register(s)
	data := []byte("..RTY.....")
	for e, err := range WalkBytes(context.Background(), data, WithFormats("carve_retry")) {
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		for m, err := range e.Members(context.Background()) {
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			r, _ := m.Open()
			b, _ := io.ReadAll(r)
			if string(b) != "RTY....." {
				t.Errorf("expected %q, got: %q", "RTY.....", b)
			}
		}
	}
}
