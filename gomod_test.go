package magic_test

import (
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// thisRepository is the path of the root module. A module whose path starts
// with it is a module of this repository, not an outside module.
const thisRepository = "github.com/xo/magic"

// approvedRequires names each module of this repository by its folder, and
// the outside modules that its go.mod can require. Each outside module names
// the decisions that approve it.
//
// The test module requires the two outside modules because it requires
// compress/xz and compress/zstd, which need them (D45), and because the
// tests of the walker in test/diff import them.
var approvedRequires = map[string]map[string]string{
	".": {},
	"compress/xz": {
		"github.com/ulikunitz/xz": "D28 and D45",
	},
	"compress/zstd": {
		"github.com/klauspost/compress": "D28 and D45",
	},
	"test": {
		"github.com/ulikunitz/xz":       "D28 and D45",
		"github.com/klauspost/compress": "D28 and D45",
	},
}

// TestEveryRequireIsApproved holds D14. The root module requires no module.
// Every other module of this repository requires only the modules of this
// repository, and the outside modules in approvedRequires.
//
// The test reads each go.mod in the repository, as go finds them. It skips
// the folder upstream/, which holds the checkouts of other projects (D25).
func TestEveryRequireIsApproved(t *testing.T) {
	t.Parallel()
	dirs := goModDirs(t)
	for dir := range approvedRequires {
		if !slices.Contains(dirs, dir) {
			t.Errorf("expected a go.mod in %s, got: none. If the module moved, change approvedRequires", dir)
		}
	}
	for _, dir := range dirs {
		file := path.Join(dir, "go.mod")
		approved, ok := approvedRequires[dir]
		if !ok {
			t.Errorf("%s: a new module. Add it to approvedRequires, with the decisions that approve "+
				"each outside module that it requires. See D14 and D28", file)
			continue
		}
		for _, r := range requires(t, file) {
			switch {
			case dir == ".":
				t.Errorf("%s:%d: requires %s. The root module requires no module. See D14 and D45",
					file, r.line, r.module)
			case r.module == thisRepository || strings.HasPrefix(r.module, thisRepository+"/"):
				// A module of this repository.
			case approved[r.module] == "":
				t.Errorf("%s:%d: requires %s, which no decision approves. The maintainers approve each "+
					"outside module, and a decision records it. See D14 and D28", file, r.line, r.module)
			}
		}
	}
}

// goModDirs returns the folder of each go.mod in the repository, with
// slashes, and "." for the root. It skips the folders that go skips, testdata
// and the ones that start with a dot or an underscore, and upstream.
func goModDirs(t *testing.T) []string {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != "." && (name == "testdata" || name == "upstream" ||
				strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if name == "go.mod" {
			dirs = append(dirs, filepath.ToSlash(filepath.Dir(p)))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	return dirs
}

// require is one module that a go.mod requires, and its line.
type require struct {
	module string
	line   int
}

// requires reads the require lines of the go.mod at file, in the form of one
// line, as in "require m v1.0.0", and in a block, as in "require (".
func requires(t *testing.T, file string) []require {
	t.Helper()
	lines := strings.Split(read(t, file), "\n")
	out := make([]require, 0, len(lines))
	var block string
	for i, line := range lines {
		if before, _, ok := strings.Cut(line, "//"); ok {
			line = before
		}
		fields := strings.Fields(line)
		switch {
		case len(fields) == 0:
			continue
		case block != "" && fields[0] == ")":
			block = ""
			continue
		case block == "" && len(fields) == 2 && fields[1] == "(":
			block = fields[0]
			continue
		case block == "require":
		case block == "" && fields[0] == "require" && len(fields) > 1:
			fields = fields[1:]
		default:
			continue
		}
		module := fields[0]
		if s, err := strconv.Unquote(module); err == nil {
			module = s
		}
		out = append(out, require{module: module, line: i + 1})
	}
	if block != "" {
		t.Errorf("%s: the block %q has no end", file, block)
	}
	return out
}
