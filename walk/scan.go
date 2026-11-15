// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates BinwalkBuilder::build, Binwalk::scan and
// Binwalk::resume_offset_after_zero_run of src/binwalk_ng.rs of binwalk-ng.

package walk

import (
	"bytes"
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

	// maxLeadingZeros is the most zero bytes before the first byte that is
	// not zero, in any magic that has such a byte. A match can start at most
	// that far before the end of a run of zeros.
	maxLeadingZeros int
	// zeroRun is the length of the pattern of zeros that the matcher also
	// finds, to skip a run of zeros, or 0 when no run can be skipped. Its
	// index in the matcher is the last.
	zeroRun int
}

// scanners caches a scanner for each set of options, because the matcher of
// every format takes some time to build.
var scanners sync.Map

// newScanner is BinwalkBuilder::build, without its files and folders. A
// short signature is tried at offset 0 only, unless all is set.
//
// The matcher also finds a run of zeros that is longer than every run of
// zeros in a magic, so that the scan skips the padding of a firmware image.
// A magic of only zeros stops the skip, unless its signature names the byte
// before the magic that its parser requires to be not zero.
func newScanner(sigs []*Signature, all bool) *scanner {
	sc := new(scanner)
	var patterns [][]byte
	zeroRun, canSkip := 0, true
	for _, s := range sigs {
		for _, p := range s.Magic {
			if s.Short && !all {
				sc.short = append(sc.short, s)
				break
			}
			var matchable int
			if lead := slices.IndexFunc(p, func(b byte) bool { return b != 0 }); lead >= 0 {
				sc.maxLeadingZeros = max(sc.maxLeadingZeros, lead)
				matchable = longestZeroRun(p)
			} else if s.ZeroMagicNonZero > 0 {
				matchable = len(p) + s.ZeroMagicNonZero
			} else {
				canSkip = false
			}
			zeroRun = max(zeroRun, matchable+1)
			sc.patterns = append(sc.patterns, s)
			patterns = append(patterns, p)
		}
	}
	if canSkip && len(patterns) > 0 {
		sc.zeroRun = zeroRun
		patterns = append(patterns, make([]byte, zeroRun))
	}
	sc.m = newMatcher(patterns)
	return sc
}

// longestZeroRun is longest_zero_run: the longest run of zero bytes in p.
func longestZeroRun(p []byte) int {
	n, run := 0, 0
	for _, b := range p {
		if b == 0 {
			run++
			n = max(n, run)
		} else {
			run = 0
		}
	}
	return n
}

// resumeAfterZeros is resume_offset_after_zero_run: where the scan resumes
// after a run of zeros that ends at end. A magic can start at most
// maxLeadingZeros bytes before the next byte that is not zero.
func (sc *scanner) resumeAfterZeros(data []byte, end int) int {
	i := slices.IndexFunc(data[end:], func(b byte) bool { return b != 0 })
	if i < 0 {
		return len(data)
	}
	return end + i - sc.maxLeadingZeros
}

// scan is Binwalk::scan. It returns the matches of data, sorted by offset,
// with the overlaps removed, and with the size of each match that had none.
// known reports which matches had a size of their own.
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
			if len(data) >= end && bytes.Equal(data[start:end], magic) {
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
	// the scan starts again at its end, and after a run of zeros, at the
	// next byte that is not zero, less the leading zeros of a magic.
	var err error
	tried := 0
	zeroIndex := len(sc.patterns)
	for offsetSafe(available, next, previous) {
		previous = next
		sc.m.find(data, next, func(off, p int) bool {
			if tried++; tried%256 == 0 {
				if err = ctx.Err(); err != nil {
					return false
				}
			}
			if sc.zeroRun > 0 && p == zeroIndex {
				next = sc.resumeAfterZeros(data, off+sc.zeroRun)
				return false
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

	// Remove the overlaps. A match whose size passes the end goes. Of two
	// matches at one offset, the one of higher confidence stays, or the
	// first, and the end of the one that goes no longer counts. A match
	// inside an earlier one of medium or high confidence goes.
	kept := fileMap[:0:0]
	var horizons []int
	horizon := 0
	for _, f := range fileMap {
		c := f.m
		if c.Offset > len(data) || c.Size < 0 || c.Size > len(data)-c.Offset {
			continue
		}
		if n := len(kept); n > 0 {
			if prev := kept[n-1].m; c.Offset == prev.Offset {
				if c.Confidence <= prev.Confidence {
					continue
				}
				kept = kept[:n-1]
				horizon = horizons[len(horizons)-1]
				horizons = horizons[:len(horizons)-1]
			} else if c.Offset < horizon {
				continue
			}
		}
		horizons = append(horizons, horizon)
		if c.Confidence >= Medium {
			horizon = c.Offset + c.Size
		}
		kept = append(kept, f)
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
