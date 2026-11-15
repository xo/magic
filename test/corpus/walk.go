package corpus

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Walk returns the inputs of the walker (D49): the originals of the corpus,
// the samples of binwalk-ng in the submodule of its checkout, the samples in test/corpus/testdata/walk, copies of them with
// bytes changed, and images that hold several originals at random offsets
// between random bytes, as a firmware image holds its files.
func Walk() ([]Input, error) {
	orig, err := Originals()
	if err != nil {
		return nil, err
	}
	// The samples of binwalk, and the samples made for the walker. Each
	// gets copies with bytes changed, below.
	for prefix, pattern := range map[string]string{
		"binwalk/": filepath.Join(root(), "upstream", "binwalk-ng", "tests", "testdata", "samples", "*"),
		"sample/":  filepath.Join(root(), "test", "corpus", "testdata", "walk", "*"),
	} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("listing %s: %w", pattern, err)
		}
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", p, err)
			}
			orig = append(orig, Input{Name: prefix + filepath.Base(p), Data: b, Path: p})
		}
	}
	slices.SortFunc(orig, func(a, b Input) int { return strings.Compare(a.Name, b.Name) })

	rng := rand.New(rand.NewPCG(0x77616c6b, 0x696d616765))
	out := slices.Clone(orig)
	for _, in := range orig {
		if !strings.HasPrefix(in.Name, "binwalk/") && !strings.HasPrefix(in.Name, "sample/") || len(in.Data) == 0 {
			continue
		}
		// Copies with bytes changed anywhere, and cut at a random length.
		for i := range flips {
			b := slices.Clone(in.Data)
			for range 1 + rng.IntN(16) {
				b[rng.IntN(len(b))] = byte(rng.Uint32())
			}
			out = append(out, Input{Name: fmt.Sprintf("%s~%d", in.Name, i), Data: b})
		}
		n := rng.IntN(len(in.Data))
		out = append(out, Input{Name: fmt.Sprintf("%s[:%d]", in.Name, n), Data: in.Data[:n:n]})
	}

	// The images: random bytes, with 2 to 6 originals of at most 1 MiB.
	var small []Input
	for _, in := range orig {
		if len(in.Data) > 0 && len(in.Data) <= 1<<20 {
			small = append(small, in)
		}
	}
	for i := range 200 {
		var b []byte
		var parts []string
		for range 2 + rng.IntN(5) {
			pad := make([]byte, rng.IntN(4096))
			for j := range pad {
				// Bytes that are mostly 0xFF or 0, as in the flash of a
				// device, and otherwise random.
				switch rng.IntN(4) {
				case 0:
					pad[j] = 0xFF
				case 1:
					pad[j] = 0
				default:
					pad[j] = byte(rng.Uint32())
				}
			}
			in := small[rng.IntN(len(small))]
			b = append(b, pad...)
			parts = append(parts, fmt.Sprintf("%s@%d", filepath.Base(in.Name), len(b)))
			b = append(b, in.Data...)
		}
		out = append(out, Input{Name: fmt.Sprintf("image%03d(%s)", i, strings.Join(parts, ",")), Data: b})
	}
	return out, nil
}
