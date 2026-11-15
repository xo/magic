// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates Binwalk::configure and Binwalk::scan of src/binwalk.rs
// of binwalk.

package walk

import (
	"context"
	"slices"
	"sync"
)

// scanner is a configured binwalk: the short signatures, and the matcher of
// the magic of the others.
type scanner struct {
	short    []*Signature // tried at offset 0 only
	patterns []*Signature // the signature of each pattern of the matcher
	m        *matcher
}

// scanners caches a scanner for each set of options, because the matcher of
// every format takes some time to build.
var scanners sync.Map

// newScanner is Binwalk::configure, without its files and folders. A short
// signature is tried at offset 0 only, unless all is set.
func newScanner(sigs []*Signature, all bool) *scanner {
	sc := new(scanner)
	var patterns [][]byte
	for _, s := range sigs {
		for _, p := range s.Magic {
			if s.Short && !all {
				sc.short = append(sc.short, s)
				break
			}
			sc.patterns = append(sc.patterns, s)
			patterns = append(patterns, p)
		}
	}
	sc.m = newMatcher(patterns)
	return sc
}

// scan is Binwalk::scan. It returns the matches of data, sorted by offset,
// with the overlaps removed, and with the size of each match that had none.
// known reports which matches had a size of their own.
//
// binwalk sorts the matches by offset and then by a random identifier, so it
// keeps a random one of two matches of the same confidence at one offset.
// scan keeps the one that it found first.
func (sc *scanner) scan(ctx context.Context, data []byte) ([]Match, []*Signature, []bool, error) {
	type found struct {
		m   Match
		sig *Signature
	}
	var fileMap []found
	available := len(data)
	next, previous := 0, -1

	// The short signatures, at offset 0 only.
	for _, s := range sc.short {
		for _, magic := range s.Magic {
			start := s.MagicOffset
			end := start + len(magic)
			if len(data) > end && string(data[start:end]) == string(magic) {
				if r, err := s.Parse(data, start); err == nil {
					fileMap = append(fileMap, found{r, s})
					if r.Confidence >= High {
						next = r.Offset + r.Size
					}
					// Only one magic of a signature can match at offset 0.
					break
				}
			}
		}
	}

	// The other signatures, at every offset. After a match of known size,
	// the scan starts again at its end.
	var err error
	tried := 0
	for offsetSafe(available, next, previous) {
		previous = next
		sc.m.find(data, next, func(off, p int) bool {
			if tried++; tried%256 == 0 {
				if err = ctx.Err(); err != nil {
					return false
				}
			}
			s := sc.patterns[p]
			r, perr := s.Parse(data, off)
			if perr != nil {
				return true
			}
			end := r.Offset + r.Size
			if end > available {
				return true
			}
			fileMap = append(fileMap, found{r, s})
			if r.Confidence >= Medium && r.Size > 0 {
				next = end
				return false
			}
			return true
		})
		if err != nil {
			return nil, nil, nil, err //nolint:wrapcheck // walkData wraps it
		}
	}

	slices.SortStableFunc(fileMap, func(a, b found) int {
		return a.m.Offset - b.m.Offset
	})

	// Remove the overlaps: two matches at one offset keep the one of higher
	// confidence, or the first, and a match inside an earlier one of medium
	// or high confidence goes.
	kept := fileMap[:0:0]
	next = 0
	for _, f := range fileMap {
		this := f.m
		remaining := len(data) - this.Offset
		if n := len(kept); n > 0 && this.Offset == kept[n-1].m.Offset {
			prev := kept[n-1].m
			if this.Confidence == prev.Confidence || this.Confidence < prev.Confidence {
				continue
			}
			kept = kept[:n-1]
		} else if this.Offset < next {
			continue
		}
		if this.Size > remaining || this.Offset+this.Size < 0 {
			continue
		}
		kept = append(kept, f)
		if this.Confidence >= Medium {
			next = this.Offset + this.Size
		}
	}

	// A match of unknown size ends at the next match of medium or high
	// confidence, or at the end of the data.
	matches := make([]Match, len(kept))
	sigs := make([]*Signature, len(kept))
	known := make([]bool, len(kept))
	for i, f := range kept {
		matches[i], sigs[i], known[i] = f.m, f.sig, f.m.Size != 0
	}
	for i := range matches {
		if known[i] {
			continue
		}
		end := len(data)
		for _, m := range matches[i+1:] {
			if m.Confidence >= Medium {
				end = m.Offset
				break
			}
		}
		matches[i].Size = end - matches[i].Offset
	}
	return matches, sigs, known, nil
}

// offsetSafe is is_offset_safe of src/common.rs, with -1 for no previous
// offset.
func offsetSafe(available, next, previous int) bool {
	if previous >= 0 && previous >= next {
		return false
	}
	return next < available
}
