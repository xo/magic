package diff

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/xo/magic/test/corpus"
	"github.com/xo/magic/test/oracle"
	"github.com/xo/magic/walk"
)

// internal are the signatures that binwalk extracts with its own code. The
// others need a program of the system, so binwalk cannot be the oracle of
// their files.
var internal = []string{
	"android_sparse", "arcadyan", "autel", "bmp", "bzip2", "csman",
	"dahua_zip", "dkbs", "dlink_tlv", "dlke", "dms", "dtb", "dxbc",
	"encfw", "encrpted_img", "gif", "gpg_signed", "gzip", "jboot_sch2", "jpeg",
	"matter_ota", "mbr", "mh01", "openssl", "pcapng", "pem_certificate",
	"pem_private_key", "pem_public_key", "png", "riff", "romfs", "shrs", "svg",
	"trx", "uimage", "vxworks_symtab", "wince", "xz", "zlib",
}

// TestWalkExtractsAsBinwalk is the gate of the extractors (D49). On each
// input of the walk corpus, binwalk extracts with -e, and the walker gives
// the members of each match. For each offset, the files are the same: the
// path, the type, the bytes, the target of a link, and the bit of
// execution. It compares the signatures that binwalk extracts with its own
// code and that the walker can extract.
func TestWalkExtractsAsBinwalk(t *testing.T) {
	t.Parallel()
	bw := oracle.OpenBinwalk(t)
	inputs, err := corpus.Walk()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	var names []string
	for _, s := range walk.Signatures() {
		if s.Extract != nil && slices.Contains(internal, s.Name) {
			names = append(names, s.Name)
		}
	}
	if len(names) == 0 {
		t.Skip("no format of the walker has an extractor")
	}
	dir := t.TempDir()

	var mu sync.Mutex
	var fails []string
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Go(func() {
			for i := range jobs {
				if msg := extractCompare(bw, dir, i, inputs[i], names); msg != "" {
					mu.Lock()
					fails = append(fails, msg)
					mu.Unlock()
				}
			}
		})
	}
	for i := range inputs {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	slices.Sort(fails)
	for i, msg := range fails {
		if i == 20 {
			t.Errorf("and %d more", len(fails)-i)
			break
		}
		t.Error(msg)
	}
	t.Logf("%d inputs, %d extractors, %d differ", len(inputs), len(names), len(fails))
}

// extractCompare compares the extraction of one input, and returns how the
// walker differs from binwalk, or "".
func extractCompare(bw *oracle.Binwalk, dir string, i int, in corpus.Input, names []string) string {
	work := filepath.Join(dir, fmt.Sprintf("x%05d", i))
	if err := os.MkdirAll(work, 0o755); err != nil {
		return fmt.Sprintf("%s: %v", in.Name, err)
	}
	defer func() { _ = os.RemoveAll(work) }()
	path := filepath.Join(work, "input")
	if err := os.WriteFile(path, in.Data, 0o600); err != nil {
		return fmt.Sprintf("%s: %v", in.Name, err)
	}

	got := map[string][]string{}
	for e, err := range walk.WalkBytes(context.Background(), in.Data, walk.WithFormats(names...)) {
		if err != nil {
			return fmt.Sprintf("%s: expected no error, got: %v", in.Name, err)
		}
		if !e.Extractable() {
			continue
		}
		var tree []string
		failed := false
		for m, err := range e.Members(context.Background()) {
			if err != nil {
				failed = true
				break
			}
			tree = append(tree, memberLine(m))
		}
		if !failed {
			slices.Sort(tree)
			got[strconv.FormatInt(e.Offset, 16)] = tree
		}
	}

	out := filepath.Join(work, "out")
	if err := bw.Extract(path, out, names); err != nil {
		if strings.Contains(err.Error(), "panicked") {
			return ""
		}
		return fmt.Sprintf("%s: %v", in.Name, err)
	}
	want := map[string][]string{}
	base := filepath.Join(out, "input.extracted")
	offsets, _ := os.ReadDir(base)
	for _, o := range offsets {
		off, err := strconv.ParseInt(o.Name(), 16, 64)
		if err != nil {
			continue
		}
		tree, err := diskTree(filepath.Join(base, o.Name()))
		if err != nil {
			return fmt.Sprintf("%s: %v", in.Name, err)
		}
		want[strconv.FormatInt(off, 16)] = tree
	}

	var diffs []string
	for _, k := range sortedKeys(want, got) {
		if !slices.Equal(want[k], got[k]) {
			diffs = append(diffs, fmt.Sprintf("  at 0x%s: expected %q,\n            got: %q", strings.ToUpper(k), want[k], got[k]))
		}
	}
	if len(diffs) > 0 {
		return fmt.Sprintf("%s:\n%s", in.Name, strings.Join(diffs, "\n"))
	}
	return ""
}

// memberLine describes a member of the walker as diskTree describes a file.
func memberLine(m *walk.Member) string {
	switch {
	case m.Mode.IsDir():
		return m.Name + "/"
	case m.Mode&fs.ModeSymlink != 0:
		return m.Name + " -> " + m.Link
	}
	r, _ := m.Open()
	b, _ := io.ReadAll(r)
	return fileLine(m.Name, b, m.Mode&0o001 != 0)
}

// fileLine describes a regular file by its name, size, hash, and bit of
// execution for others, which is the bit that binwalk sets.
func fileLine(name string, b []byte, exec bool) string {
	x := ""
	if exec {
		x = " +x"
	}
	return fmt.Sprintf("%s %d %x%s", name, len(b), sha256.Sum256(b), x)
}

// diskTree describes the files under dir, as memberLine does.
func diskTree(dir string) ([]string, error) {
	var tree []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			l, err := os.Readlink(p)
			if err != nil {
				return err
			}
			tree = append(tree, rel+" -> "+filepath.ToSlash(l))
		case d.IsDir():
			tree = append(tree, rel+"/")
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			tree = append(tree, fileLine(rel, b, fi.Mode()&0o001 != 0))
		}
		return nil
	})
	slices.Sort(tree)
	return tree, err
}

// sortedKeys returns the keys of both maps, sorted.
func sortedKeys(a, b map[string][]string) []string {
	var keys []string
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}
