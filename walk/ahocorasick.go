package walk

// matcher finds many fixed patterns in one pass, as the Aho-Corasick matcher
// of the Rust crate aho-corasick that binwalk uses. It reports every match,
// overlapping ones too, in the order of their end. Matches that end at the
// same byte come longest first, and patterns of the same length in the order
// that they were added, as the crate reports them.
type matcher struct {
	next [][256]int32 // the next state for each state and byte
	out  [][]int32    // the patterns that end at each state, in order
	lens []int        // the length of each pattern
}

// newMatcher builds the matcher of patterns. Pattern i is reported as i.
func newMatcher(patterns [][]byte) *matcher {
	m := &matcher{lens: make([]int, len(patterns))}
	m.add() // the root
	for i, p := range patterns {
		m.lens[i] = len(p)
		s := int32(0)
		for _, c := range p {
			if m.next[s][c] == 0 {
				m.next[s][c] = m.add()
			}
			s = m.next[s][c]
		}
		m.out[s] = append(m.out[s], int32(i))
	}
	// Breadth first, set the failure of each state, append its matches, and
	// fill the missing moves from the moves of the failure.
	fail := make([]int32, len(m.next))
	var queue []int32
	for c := range 256 {
		if s := m.next[0][c]; s != 0 {
			queue = append(queue, s)
		}
	}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		m.out[s] = append(m.out[s], m.out[fail[s]]...)
		for c := range 256 {
			t := m.next[s][c]
			// The build fills the moves of s only here, so every move
			// that is not 0 is still an edge of the trie.
			if t == 0 {
				m.next[s][c] = m.next[fail[s]][c]
				continue
			}
			fail[t] = m.next[fail[s]][c]
			queue = append(queue, t)
		}
	}
	return m
}

// add adds a state and returns its number.
func (m *matcher) add() int32 {
	m.next = append(m.next, [256]int32{})
	m.out = append(m.out, nil)
	return int32(len(m.next) - 1)
}

// find calls yield for each match in data from start, with the offset of its
// first byte and its pattern, until yield returns false.
func (m *matcher) find(data []byte, start int, yield func(off, pattern int) bool) {
	s := int32(0)
	for i := start; i < len(data); i++ {
		s = m.next[s][data[i]]
		for _, p := range m.out[s] {
			if !yield(i+1-m.lens[p], int(p)) {
				return
			}
		}
	}
}
