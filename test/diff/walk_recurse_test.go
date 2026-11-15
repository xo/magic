package diff

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/xo/magic/test/corpus"
	"github.com/xo/magic/test/oracle"
	"github.com/xo/magic/walk"
)

// TestWalkRecursesAsBinwalk is the gate of the walk of members (D49). On
// each input of the walk corpus, binwalk runs with -M, and the walker with
// WithDepth: each file that an extraction writes is walked again. For each
// file, binwalk and the walker find the same matches. It walks for the
// signatures that binwalk extracts with its own code and that the walker
// can extract.
//
// The client of the oracle runs binwalk on one file at a time and walks the
// extractions itself, because binwalk -M can lose the analysis of its last
// file.
func TestWalkRecursesAsBinwalk(t *testing.T) {
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
	dir := t.TempDir()
	var mu sync.Mutex
	var fails []string
	nested := 0
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Go(func() {
			for i := range jobs {
				msg, n := recurseCompare(bw, dir, i, inputs[i], names)
				mu.Lock()
				nested += n
				if msg != "" {
					fails = append(fails, msg)
				}
				mu.Unlock()
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
	t.Logf("%d inputs, %d nested files with matches, %d differ", len(inputs), nested, len(fails))
}

// recurseCompare compares the walk of one input, and returns how the walker
// differs from binwalk, or "", and the count of nested files with matches.
func recurseCompare(bw *oracle.Binwalk, dir string, i int, in corpus.Input, names []string) (string, int) {
	work := filepath.Join(dir, fmt.Sprintf("r%05d", i))
	if err := os.MkdirAll(work, 0o755); err != nil {
		return fmt.Sprintf("%s: %v", in.Name, err), 0
	}
	defer func() { _ = os.RemoveAll(work) }()
	path := filepath.Join(work, "input")
	if err := os.WriteFile(path, in.Data, 0o600); err != nil {
		return fmt.Sprintf("%s: %v", in.Name, err), 0
	}

	// The path that binwalk gives each file: the input, and then
	// <file>.extracted/<HEX offset>/<member> for each level.
	got := map[string][]string{}
	files := map[*walk.Entry]string{}
	for e, err := range walk.WalkBytes(context.Background(), in.Data, walk.WithFormats(names...), walk.WithDepth(16)) {
		if err != nil {
			return fmt.Sprintf("%s: expected no error, got: %v", in.Name, err), 0
		}
		file := "input"
		if e.Parent != nil {
			file = fmt.Sprintf("%s.extracted/%X/%s", files[e.Parent], e.Parent.Offset, e.Member)
		}
		files[e] = file
		got[file] = append(got[file], fmt.Sprintf("%d+%d %s %q", e.Offset, e.Size, e.Format, e.Description))
	}

	ms, err := bw.Recurse(path, filepath.Join(work, "out"), names, 16)
	if err != nil {
		if strings.Contains(err.Error(), "panicked") {
			return "", 0
		}
		return fmt.Sprintf("%s: %v", in.Name, err), 0
	}
	want := map[string][]string{}
	for file, fm := range ms {
		for _, m := range fm {
			want[file] = append(want[file], fmt.Sprintf("%d+%d %s %q", m.Offset, m.Size, m.Name, m.Description))
		}
	}
	n := 0
	for k := range got {
		if k != "input" {
			n++
		}
	}
	var diffs []string
	for _, k := range sortedKeys(want, got) {
		// A file that 7z decoded in part from a bad xz stream of a damaged
		// copy, which the walker does not extract (see partialLZMA).
		if len(got[k]) == 0 && strings.Contains(k, "/xz_") && strings.ContainsAny(in.Name, "~[") {
			continue
		}
		if !slices.Equal(want[k], got[k]) {
			diffs = append(diffs, fmt.Sprintf("  %s: expected %q,\n      got: %q", k, want[k], got[k]))
		}
	}
	if len(diffs) > 0 {
		return fmt.Sprintf("%s:\n%s", in.Name, strings.Join(diffs, "\n")), n
	}
	return "", n
}
