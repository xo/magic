package diff

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
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

// internal are the signatures that binwalk-ng extracts with its own code
// and that the walker extracts. binwalk-ng also extracts rar and qnx_ifs
// with its own code, through crates, which the walker does not (D60). The
// walker extracts lzfse with the decoder of compress/lzfse, and the chunks of
// minifs with the decoders of compress/xz (D68).
var internal = []string{
	"android_sparse", "arcadyan", "autel", "bmp", "bzip2", "csman",
	"dahua_zip", "dkbs", "dlink_fw", "dms", "dlink_tlv", "dlke", "dtb", "dxbc",
	"encfw", "encrpted_img", "eva", "gif", "gpg_signed", "gzip", "jboot_sch2",
	"jpeg", "lz4", "lzfse", "lzma", "matter_ota", "mbr", "mh01", "minifs",
	"openssl", "pcapng", "pem_certificate", "pem_private_key",
	"pem_public_key", "png",
	"program_store", "riff", "romfs", "shrs", "srecord", "srecord_generic",
	"svg", "tarball", "trx", "uimage", "vxworks_symtab", "wince", "xz",
	"zlib", "zstd",
}

// external are the signatures that binwalk-ng extracts with a program that
// it finds on PATH, and the program. The gate compares them where the program
// is installed. A program writes the target of a link as it is, and the
// walker keeps it inside the output, so the gate compares the file that a
// link of these formats names.
var external = map[string]string{
	"squashfs":  "sasquatch",
	"7zip":      "7z",
	"apfs":      "7z",
	"arj":       "7z",
	"compressd": "7z",
	"cpio":      "7z",
	"cramfs":    "7z",
	"efigpt":    "7z",
	"iso9660":   "7z",
	"zip":       "7z",
	"cab":       "cabextract",
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
		if s.Extract == nil {
			continue
		}
		if slices.Contains(internal, s.Name) {
			names = append(names, s.Name)
		} else if tool, ok := external[s.Name]; ok {
			if _, err := exec.LookPath(tool); err == nil {
				names = append(names, s.Name)
			}
		}
	}
	if len(names) == 0 {
		t.Skip("no format of the walker has an extractor")
	}
	dir := t.TempDir()

	var mu sync.Mutex
	failed := map[int]bool{}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Go(func() {
			for i := range jobs {
				if msg := extractCompare(bw, dir, i, inputs[i], names); msg != "" {
					mu.Lock()
					failed[i] = true
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

	// sasquatch fails more often under load. The inputs that differ run
	// again one at a time, and only a difference that stays counts.
	var fails []string
	for _, i := range slices.Sorted(maps.Keys(failed)) {
		if msg := extractCompare(bw, dir, i, inputs[i], names); msg != "" {
			fails = append(fails, msg)
		}
	}

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
	resolve := map[string]bool{} // the offsets of matches of an external format
	partial := map[string]bool{} // the offsets of matches that 7z can decode in part
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
		if !failed || partialLZMA(in.Name, e.Format) {
			key := strconv.FormatInt(e.Offset, 16)
			partial[key] = partialLZMA(in.Name, e.Format)
			if _, ok := external[e.Format]; ok {
				resolve[key] = true
				tree = resolveLinks(tree, "")
			}
			slices.Sort(tree)
			got[key] = tree
		}
	}

	// sasquatch can give other files on another run: whether it stops at a
	// block that it cannot decompress depends on the timing of its threads
	// (see skip in squashfs3.go). When the files of an external format
	// differ, the gate runs binwalk again, and takes a run that matches.
	var msg string
	for range 6 {
		var retry bool
		if msg, retry = binwalkCompare(bw, work, path, in.Name, names, got, resolve, partial); !retry {
			break
		}
	}
	return msg
}

// binwalkCompare extracts the input with binwalk, and returns how the
// walker differs from it, or "". It reports whether the files of an external
// format differ.
func binwalkCompare(bw *oracle.Binwalk, work, path, name string, names []string, got map[string][]string, resolve, partial map[string]bool) (string, bool) {
	out := filepath.Join(work, "out")
	if err := bw.Extract(path, out, names); err != nil {
		if strings.Contains(err.Error(), "panicked") {
			return "", false
		}
		return fmt.Sprintf("%s: %v", name, err), false
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
			return fmt.Sprintf("%s: %v", name, err), false
		}
		key := strconv.FormatInt(off, 16)
		if resolve[key] {
			tree = resolveLinks(tree, filepath.ToSlash(filepath.Join(base, o.Name())))
			slices.Sort(tree)
		}
		want[key] = tree
	}

	var diffs []string
	retry := false
	for _, k := range sortedKeys(want, got) {
		if partial[k] && subset(got[k], want[k]) {
			continue
		}
		if !slices.Equal(want[k], got[k]) {
			diffs = append(diffs, fmt.Sprintf("  at 0x%s: expected %q,\n            got: %q", strings.ToUpper(k), want[k], got[k]))
			retry = retry || resolve[k]
		}
	}
	if len(diffs) > 0 {
		return fmt.Sprintf("%s:\n%s", name, strings.Join(diffs, "\n")), retry
	}
	return "", false
}

// partialLZMA reports whether binwalk-ng can give a part of the files of a
// match of format in the input name, where the walker gives fewer. For a
// damaged copy of the corpus, binwalk-ng runs 7z on 7-Zip data and on a bad
// xz stream. 7z writes what it decoded from LZMA data before the error, in
// steps of its own. The decoders of the walker give nothing for a stream
// with an error, and D60 adds no decoder.
func partialLZMA(name, format string) bool {
	return (format == "7zip" || format == "xz") && strings.ContainsAny(name, "~[")
}

// subset reports whether every line of a is in b. Both are sorted.
func subset(a, b []string) bool {
	for _, l := range a {
		if _, ok := slices.BinarySearch(b, l); !ok {
			return false
		}
	}
	return true
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
		case !d.Type().IsRegular():
			// A FIFO, a device or a socket that a program made. The walker
			// makes none, and reading a FIFO would block.
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

// resolveLinks rewrites each link of a tree as the path, from the root of
// the extraction, of the file that it names.
//
// 7-Zip 26 writes an absolute target as a path under its output folder,
// such as /tmp/x/input.extracted/0/bin/data. With root, the folder of the
// match on the disk, such a target becomes relative to it.
func resolveLinks(tree []string, root string) []string {
	out := make([]string, len(tree))
	for i, line := range tree {
		name, target, ok := strings.Cut(line, " -> ")
		if !ok {
			out[i] = line
			continue
		}
		if rest, ok := strings.CutPrefix(target, root+"/"); root != "" && ok {
			target = strings.TrimPrefix(path.Clean("/"+rest), "/")
		} else if strings.HasPrefix(target, "/") {
			// The program writes the file system under one folder, such as
			// squashfs-root, and an absolute target names a file from it.
			top, _, _ := strings.Cut(name, "/")
			target = path.Join(top, strings.TrimPrefix(path.Clean(target), "/"))
		} else {
			target = strings.TrimPrefix(path.Clean(path.Join("/", path.Dir(name), target)), "/")
		}
		out[i] = name + " => " + target
	}
	return out
}
