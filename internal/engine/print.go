package engine

import (
	"strings"

	"github.com/xo/magic/internal/rules"
)

// mprint is mprint of softmagic.c: it prints the description of m with its
// value. Phase 2 needs only the outputs that print no description, so this
// prints the description with its format left out. Phase 3 brings the
// formats of C (docs/PLAN.md).
func (st *state) mprint(m *rules.Entry) int {
	desc := cstr(m.Desc[:])
	if x, ok := st.varexpand(desc, 512); ok {
		desc = x
	}
	st.printf("%s", fmtC(string(desc), ""))
	return 0
}

// fmtC puts arg in the place of the one printf format in desc.
func fmtC(desc, arg string) string {
	i := strings.IndexByte(desc, '%')
	if i < 0 {
		return desc
	}
	j := i + 1
	for j < len(desc) && strings.IndexByte("-+ #0123456789.lhq", desc[j]) >= 0 {
		j++
	}
	if j < len(desc) {
		j++
	}
	return desc[:i] + arg + desc[j:]
}
