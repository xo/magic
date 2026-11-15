// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates blocks of src/entropy.rs of binwalk, and
// shannon_entropy of the crate entropy 0.4.3, which is under the MIT
// license.

package walk

import "math"

// Block is the entropy of one block of an input, in bits for each byte, from
// 0 to 8.
type Block struct {
	Start, End int64
	Entropy    float64
}

// Entropy returns the entropy of data in 2048 blocks of the same size, and a
// shorter last block, as the option -E of binwalk does. Data of fewer than
// 2048 bytes is one block. The walker draws no graph.
//
// binwalk computes in 32-bit floats, and so does Entropy. A value can differ
// from binwalk in its last bit, because Go has no logarithm of 32 bits.
func Entropy(data []byte) []Block {
	const count = 2048
	if len(data) == 0 {
		// binwalk panics on an empty input, in chunks(0).
		return nil
	}
	size := len(data)
	if size >= count {
		size = len(data) / count
	}
	var out []Block
	for start := 0; start < len(data); start += size {
		end := min(start+size, len(data))
		out = append(out, Block{Start: int64(start), End: int64(end), Entropy: float64(shannon(data[start:end]))})
	}
	return out
}

// shannon is shannon_entropy of the crate entropy.
func shannon(b []byte) float32 {
	var counts [256]int
	for _, c := range b {
		counts[c]++
	}
	ln2 := float32(math.Log(2))
	var h float32
	for _, n := range counts {
		if n == 0 {
			continue
		}
		p := float32(n) / float32(len(b))
		h -= p * (float32(math.Log(float64(p))) / ln2)
	}
	return h
}
