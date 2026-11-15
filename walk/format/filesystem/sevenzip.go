//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package filesystem

import (
	"iter"
	"path"
	"strings"

	"github.com/xo/magic/walk"
)

// szItem is one item of an archive, as a handler of 7-Zip reports it to
// `7z x`. binwalk extracts CramFS and ISO 9660 with 7z, and writeSevenZip
// writes the items as 7z writes them.
type szItem struct {
	path    string // the path, with "/" between the names
	dir     bool
	link    bool   // the item is a link to target
	target  string // the target of a link
	mode    int    // the bits of the mode, when hasMode is set
	hasMode bool

	// data gives the content of a file in parts. It stops at the first
	// part that it cannot read, and the file keeps the parts before it, as
	// 7z keeps them after an error of data. It is nil for an empty file.
	data iter.Seq[[]byte]

	// linkData, when it is not nil, gives the target of a link, which 7z
	// reads when it writes the link. A target that is not linkSize bytes is
	// an error of data, and 7z then writes an empty file in place of the
	// link, with no mode.
	linkData iter.Seq[[]byte]
	linkSize int

	// fatal, when it is not nil, reports whether reading the data of the
	// item failed with an error that stops 7z. 7z then keeps the item and
	// the items before it, and writes no item after it.
	fatal func() bool
}

// writeSevenZip writes items to out in their order, as 7z writes them:
//
//  1. 7z makes the folder of a file when it is not there.
//  2. An item whose path an item after it has too is written over, as 7z
//     does with -y. So only the last one counts.
//  3. 7z makes a link only when its target has no ".." part, and writes
//     an empty file in its place otherwise. It skips a link whose target
//     is only slashes.
//  4. 7z sets the mode of a file. The walker adds the bit of execution
//     when the mode has one, as the other extractors do.
//  5. 7z stops at an item whose data has an error that is fatal.
//
// 7z goes on to the next item when it cannot write one, and so does
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
		if it.linkData != nil {
			var b strings.Builder
			for p := range it.linkData {
				b.Write(p)
			}
			if b.Len() != it.linkSize {
				out.CreateFile(it.path, nil)
				if it.fatal != nil && it.fatal() {
					return true
				}
				continue
			}
			it.link, it.target = true, b.String()
		}
		if it.link {
			switch t := strings.TrimLeft(it.target, "/"); {
			case t == "":
			case !hasDotDot(t):
				out.CreateSymlink(it.path, it.target)
			default:
				// The link is dangerous for 7z: it writes an empty file.
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
		if it.fatal != nil && it.fatal() {
			return true
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
