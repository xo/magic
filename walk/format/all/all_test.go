//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package all_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xo/magic/walk"
	_ "github.com/xo/magic/walk/format/all"
)

// FuzzWalk walks hostile bytes with every format, and extracts and walks
// each match to depth 2. It fails on a panic, and on a walk that does not
// end. The seeds are the samples of the walker, in test/corpus/testdata/walk.
// Run it longer with:
//
//	go test -fuzz FuzzWalk -fuzztime 10m ./walk/format/all
func FuzzWalk(f *testing.F) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "test", "corpus", "testdata", "walk", "*"))
	if err != nil {
		f.Fatalf("expected no error, got: %v", err)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			f.Fatalf("expected no error, got: %v", err)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		ctx := context.Background()
		start := time.Now()
		defer func() {
			if d := time.Since(start); d > 5*time.Second {
				t.Errorf("expected a walk of at most 5s, got: %v", d)
			}
		}()
		opts := []walk.Option{walk.WithDepth(2), walk.WithMaxOutput(16 << 20), walk.WithSearchAll()}
		for e, err := range walk.WalkBytes(ctx, data, opts...) {
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			for _, err := range e.Members(ctx) {
				_ = err
			}
		}
	})
}
