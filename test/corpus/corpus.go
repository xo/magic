// Package corpus holds the inputs of the differential test of D30: the test
// files of file at the commit of the oracle, the sample files of this
// repository, and cut and damaged copies of each.
//
// The copies come from a fixed seed, so each run tests the same bytes, and a
// failure names the input that a person can rebuild.
package corpus

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
)

// Input is one input. Path is the file on disk for an original, and empty for
// a copy, which exists only in Data.
type Input struct {
	Name string
	Data []byte
	Path string
}

// cuts are the lengths that each original is cut to. A rule reads at a fixed
// offset, so short prefixes reach the paths where a read falls off the end.
var cuts = []int{1, 2, 3, 4, 8, 16, 32, 64, 128, 256, 512, 1024}

// flips is how many copies of each original have random bytes changed.
const flips = 4

// root is the root of the repository.
func root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// Originals returns the files that the corpus starts from: the test files of
// file at the base commit, from the tree that test/oracle/build.sh exported,
// the files in testdata/ of this repository, and the compressed files in
// test/corpus/testdata, for the flag Compress.
func Originals() ([]Input, error) {
	var paths []string
	for _, pattern := range []string{
		filepath.Join(root(), "test", ".oracle", "src", "tests", "*.testfile"),
		filepath.Join(root(), "testdata", "*"),
		filepath.Join(root(), "test", "corpus", "testdata", "*"),
	} {
		m, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("listing %s: %w", pattern, err)
		}
		paths = append(paths, m...)
	}
	slices.Sort(paths)
	var out []Input
	for _, p := range paths {
		// test/corpus/testdata/walk holds the samples of the walker.
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", p, err)
		}
		rel, err := filepath.Rel(root(), p)
		if err != nil {
			rel = p
		}
		out = append(out, Input{Name: filepath.ToSlash(rel), Data: b, Path: p})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("listing the corpus: no file found. Run test/oracle/build.sh")
	}
	return out, nil
}

// All returns the originals and the copies made from them.
func All() ([]Input, error) {
	orig, err := Originals()
	if err != nil {
		return nil, err
	}
	rng := rand.New(rand.NewPCG(0x6d61676963, 0x636f72707573))
	out := slices.Clone(orig)
	for _, in := range orig {
		for _, n := range cuts {
			if n >= len(in.Data) {
				break
			}
			out = append(out, Input{
				Name: fmt.Sprintf("%s[:%d]", in.Name, n),
				Data: in.Data[:n:n],
			})
		}
		if len(in.Data) == 0 {
			continue
		}
		for i := range flips {
			b := slices.Clone(in.Data)
			// Change up to eight bytes in the first 4 KiB, where most rules
			// read.
			span := min(len(b), 4096)
			for range 1 + rng.IntN(8) {
				b[rng.IntN(span)] = byte(rng.Uint32())
			}
			out = append(out, Input{Name: fmt.Sprintf("%s~%d", in.Name, i), Data: b})
		}
	}
	// Copies compressed with gzip and zlib, for the flag Compress. They come
	// from the standard library, so a person can rebuild them.
	for _, in := range orig {
		if len(in.Data) == 0 || len(in.Data) > 1<<20 {
			continue
		}
		var gz, zl bytes.Buffer
		gw := gzip.NewWriter(&gz)
		gw.Write(in.Data)
		gw.Close()
		zw := zlib.NewWriter(&zl)
		zw.Write(in.Data)
		zw.Close()
		g := gz.Bytes()
		out = append(out,
			Input{Name: in.Name + ".gz", Data: g},
			Input{Name: in.Name + ".zlib", Data: zl.Bytes()},
			Input{Name: in.Name + ".gz[:half]", Data: g[:len(g)/2]},
		)
	}
	return out, nil
}
