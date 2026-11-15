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
	_ "github.com/xo/magic/walk/format/all"

	// The walker decodes xz, lzma and zstd with the decoders of compress,
	// as magic does with WithDecompress.
	_ "github.com/xo/magic/compress/xz"
	_ "github.com/xo/magic/compress/zstd"
)

// TestWalkMatchesBinwalk is the gate of the walker (D49). On each input of
// the walk corpus, binwalk and the walker look for the formats that the
// walker has, and give the same matches: the offset, the size, the name, the
// confidence and the description of each.
//
// Every eighth input is also compared with the short signatures tried at
// every offset, as the option -a of binwalk.
//
// binwalk keeps a random one of two matches of the same confidence at one
// offset, because it sorts them by a random identifier. When the answers
// differ, the test asks binwalk again, up to three times.
func TestWalkMatchesBinwalk(t *testing.T) {
	t.Parallel()
	bw := oracle.OpenBinwalk(t)
	inputs, err := corpus.Walk()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	var names []string
	for _, s := range walk.Signatures() {
		names = append(names, s.Name)
	}
	dir := t.TempDir()

	var mu sync.Mutex
	var fails []string
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Go(func() {
			for i := range jobs {
				for _, all := range []bool{false, true} {
					if all && i%8 != 0 {
						continue
					}
					if msg := walkCompare(bw, dir, i, inputs[i], names, all); msg != "" {
						mu.Lock()
						fails = append(fails, msg)
						mu.Unlock()
					}
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
	t.Logf("%d inputs, %d formats, %d differ", len(inputs), len(names), len(fails))
}

// walkCompare compares the walker with binwalk on one input, and returns how
// they differ, or "".
func walkCompare(bw *oracle.Binwalk, dir string, i int, in corpus.Input, names []string, all bool) string {
	name := in.Name
	opts := []walk.Option{walk.WithFormats(names...)}
	if all {
		name += " (-a)"
		opts = append(opts, walk.WithSearchAll())
	}
	path := in.Path
	if path == "" {
		path = filepath.Join(dir, fmt.Sprintf("input%05d-%t", i, all))
		if err := os.WriteFile(path, in.Data, 0o600); err != nil {
			return fmt.Sprintf("%s: writing the input: %v", name, err)
		}
		defer func() { _ = os.Remove(path) }()
	}
	var got []string
	for e, err := range walk.WalkBytes(context.Background(), in.Data, opts...) {
		if err != nil {
			return fmt.Sprintf("%s: expected no error, got: %v", name, err)
		}
		got = append(got, fmt.Sprintf("%d+%d %s %d %q", e.Offset, e.Size, e.Format, e.Confidence, e.Description))
	}
	var want []string
	for range 3 {
		ms, err := bw.Scan(path, names, all)
		if err != nil {
			// binwalk panics on some damaged inputs, where the walker
			// rejects the candidate.
			if strings.Contains(err.Error(), "panicked") {
				return ""
			}
			return fmt.Sprintf("%s: %v", name, err)
		}
		want = want[:0]
		for _, m := range ms {
			want = append(want, fmt.Sprintf("%d+%d %s %d %q", m.Offset, m.Size, m.Name, m.Confidence, m.Description))
		}
		if slices.Equal(got, want) {
			return ""
		}
	}
	return fmt.Sprintf("%s:\nexpected %q,\n     got: %q", name, want, got)
}
