//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package filesystem

import (
	"iter"
	"path"
	"strings"

	"github.com/xo/magic/walk"
)

// szItem is one item of an archive, as a handler of 7-Zip reports it to
// `7zz x`. binwalk extracts CramFS and ISO 9660 with 7zz, and writeSevenZip
// writes the items as 7zz writes them.
type szItem struct {
	path    string // the path, with "/" between the names
	dir     bool
	link    bool   // the item is a link to target
	target  string // the target of a link
	mode    int    // the bits of the mode, when hasMode is set
	hasMode bool

	// data gives the content of a file in parts. It stops at the first
	// part that it cannot read, and the file keeps the parts before it, as
	// 7zz keeps them after an error of data. It is nil for an empty file.
	data iter.Seq[[]byte]
}

// writeSevenZip writes items to out in their order, as 7zz writes them:
//
//  1. 7zz makes the folder of a file when it is not there.
//  2. An item whose path an item after it has too is written over, as 7zz
//     does with -y. So only the last one counts.
//  3. 7zz makes a link only when its target has no ".." part, and writes
//     an empty file in its place otherwise. It skips a link whose target
//     is only slashes.
//  4. 7zz sets the mode of a file. The walker adds the bit of execution
//     when the mode has one, as the other extractors do.
//
// 7zz goes on to the next item when it cannot write one, and so does
// writeSevenZip. It reports false only when out refuses the data of a file,
// which is the limit of the output.
func writeSevenZip(out *walk.Output, items []szItem) bool {
	last := make(map[string]int, len(items))
	for i, it := range items {
		if !it.dir {
			last[it.path] = i
		}
	}
	for i, it := range items {
		switch {
		case strings.Trim(it.path, "/") == "":
			continue
		case it.dir:
			out.CreateDirectory(it.path)
			continue
		case last[it.path] != i:
			continue
		}
		if dir := path.Dir(it.path); dir != "." && dir != "/" && !out.CreateDirectory(dir) {
			continue
		}
		if it.link {
			switch t := strings.TrimLeft(it.target, "/"); {
			case t == "":
			case !hasDotDot(t):
				out.CreateSymlink(it.path, it.target)
			default:
				// The link is dangerous for 7zz: it writes an empty file.
				out.CreateFile(it.path, nil)
			}
			continue
		}
		if !out.CreateFile(it.path, nil) {
			continue
		}
		if it.data != nil {
			for b := range it.data {
				if len(b) > 0 && !out.AppendFile(it.path, b) {
					return false
				}
			}
		}
		if it.hasMode && it.mode&0o111 != 0 {
			out.MakeExecutable(it.path)
		}
	}
	return true
}

// hasDotDot reports whether the path p has a ".." part.
func hasDotDot(p string) bool {
	for part := range strings.SplitSeq(p, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}
