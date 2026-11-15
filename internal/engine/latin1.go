package engine

import (
	"bytes"
	"sort"
	"unicode/utf8"
)

// A regular expression of a rule reads a region that starts at the offset
// of the rule and holds at most regexMax bytes. The rules for text run many
// regular expressions at the same few offsets of one buffer, so a detection
// converts the bytes at an offset to Latin-1 once, and keeps the result for
// the next rule (D44).

// window is the bytes from one start in a buffer, at most regexMax of them,
// read as Latin-1. It scans and converts only as far as a rule reads.
type window struct {
	raw     []byte  // the bytes
	nul     int     // the index of the first NUL, or len(raw)
	scanned int     // the bytes that are checked for a byte above 0x7f
	high    int     // the index of the first byte above 0x7f, or -1 if none is scanned
	u       []byte  // the bytes as UTF-8, one character for each byte
	off     []int32 // off[i] is where byte i starts in u, for each converted byte and one more
}

// maxWindows is how many windows a detection keeps. The rules for text read
// at a few offsets, and a new window takes the place of the oldest.
const maxWindows = 8

// windows are the windows of a detection, newest first.
type windows struct {
	w [maxWindows]*window
	n int
}

// window returns the window that starts at s[0]. s runs to the end of its
// buffer, and is not empty.
func (st *state) window(s []byte) *window {
	key := &s[0]
	for _, w := range st.windows.w[:st.windows.n] {
		if &w.raw[0] == key {
			return w
		}
	}
	raw := s[:min(len(s), regexMax)]
	w := &window{raw: raw, nul: len(raw), high: -1}
	if i := bytes.IndexByte(raw, 0); i >= 0 {
		w.nul = i
	}
	ws := &st.windows
	copy(ws.w[1:], ws.w[:min(ws.n, maxWindows-1)])
	ws.w[0] = w
	ws.n = min(ws.n+1, maxWindows)
	return w
}

// ascii reports whether the first end bytes of w are all ASCII.
func (w *window) ascii(end int) bool {
	if w.high >= 0 {
		return end <= w.high
	}
	for ; w.scanned < end; w.scanned++ {
		if w.raw[w.scanned] >= utf8.RuneSelf {
			w.high = w.scanned
			return false
		}
	}
	return true
}

// find runs rx on the first end bytes of w, as find does on those bytes.
func (w *window) find(rx *regex, end int) (int, int, bool) {
	if w.ascii(end) {
		loc := rx.re.FindIndex(w.raw[:end])
		if loc == nil {
			return 0, 0, false
		}
		return loc[0], loc[1], true
	}
	if w.off == nil {
		w.u = make([]byte, 0, 2*end)
		w.off = make([]int32, 1, end+1)
	}
	for i := len(w.off) - 1; i < end; i++ {
		w.u = utf8.AppendRune(w.u, rune(w.raw[i]))
		w.off = append(w.off, int32(len(w.u)))
	}
	loc := rx.re.FindIndex(w.u[:w.off[end]])
	if loc == nil {
		return 0, 0, false
	}
	return w.byteAt(loc[0]), w.byteAt(loc[1]), true
}

// byteAt returns the index of the byte whose character starts at p of u.
func (w *window) byteAt(p int) int {
	return sort.Search(len(w.off), func(i int) bool { return int(w.off[i]) >= p })
}
