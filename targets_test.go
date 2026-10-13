package magic_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// modulePath is the path of the root module. Each target is a nested module
// under it, in libmagic/<goos>_<goarch>, and its path ends in /v5 for libmagic
// 5.48, because Go wants /vN on a module at version 2 or higher.
const modulePath = "github.com/xo/magic"

// major is the major version of libmagic that the targets hold.
const major = "v5"

// archives are the files that every target module holds.
var archives = []string{"libmagic.a", "libz.a", "libbz2.a", "liblzma.a", "libzstd.a"}

// targets returns the names of the target folders in libmagic/, such as
// linux_amd64. A folder is a target if it holds a go.mod.
func targets(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("libmagic")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join("libmagic", e.Name(), "go.mod")); err != nil {
			t.Errorf("libmagic/%s has no go.mod. A target is a module of its own. See D17", e.Name())
			continue
		}
		out = append(out, e.Name())
	}
	if len(out) == 0 {
		t.Fatal("libmagic/ holds no target, so there is nothing to test")
	}
	slices.Sort(out)
	return out
}

// TestEveryTargetIsWired holds D17. Each target in libmagic/ is a module that
// holds its archives, the root has a file that imports it for its platform,
// and the root go.mod requires it and points it at the working tree.
//
// A user who builds for one platform then downloads the archives of that
// platform and no other. A target that is missing from one of these places
// builds in this repository and fails for every user, because the replace
// line is the only thing that finds the module before it has a tag.
func TestEveryTargetIsWired(t *testing.T) {
	t.Parallel()
	gomod := read(t, "go.mod")
	for _, name := range targets(t) {
		path := modulePath + "/libmagic/" + name + "/" + major
		dir := filepath.Join("libmagic", name)

		// The target module.
		mod := read(t, filepath.Join(dir, "go.mod"))
		if want := "module " + path + "\n"; !strings.Contains(mod, want) {
			t.Errorf("%s/go.mod must hold %q. See D17", dir, strings.TrimSpace(want))
		}
		if !regexp.MustCompile(`(?m)^go 1\.\d+(\.\d+)?$`).MatchString(mod) {
			t.Errorf("%s/go.mod has no go directive. See D18", dir)
		}
		want := slices.Clone(archives)
		if strings.HasPrefix(name, "windows_") {
			want = append(want, "libgnurx.a")
		}
		for _, a := range want {
			if _, err := os.Stat(filepath.Join(dir, a)); err != nil {
				t.Errorf("%s is missing. Build it with build-libmagic.sh", filepath.Join(dir, a))
			}
		}
		link := read(t, filepath.Join(dir, "link.go"))
		if !strings.Contains(link, "#cgo LDFLAGS: -L${SRCDIR} -lmagic") {
			t.Errorf("%s/link.go must link libmagic from its own folder. See D17", dir)
		}

		// The root file that imports it.
		file := "link_" + name + ".go"
		root := read(t, file)
		if !strings.Contains(root, `_ "`+path+`"`) {
			t.Errorf("%s must import %s", file, path)
		}

		// The root go.mod.
		if !regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(path) + ` v\d+\.\d+\.\d+$`).MatchString(gomod) {
			t.Errorf("go.mod does not require %s at a version like v5.48.0. See D17", path)
		}
		if !strings.Contains(gomod, path+" => ./libmagic/"+name+"\n") {
			t.Errorf("go.mod has no replace line for %s to ./libmagic/%s. See D17", path, name)
		}
	}
}

// TestNoLinkFileWithoutATarget fails on a root file that imports a target
// that has no folder. Deleting a target means deleting its file and its go.mod
// lines.
func TestNoLinkFileWithoutATarget(t *testing.T) {
	t.Parallel()
	have := targets(t)
	files, err := filepath.Glob("link_*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(strings.TrimPrefix(f, "link_"), ".go")
		if !slices.Contains(have, name) {
			t.Errorf("%s has no target folder libmagic/%s. See D17", f, name)
		}
	}
	gomod := read(t, "go.mod")
	for _, m := range regexp.MustCompile(`(?m)^\s*`+regexp.QuoteMeta(modulePath)+`/libmagic/([^/\s]+)/v\d+ `).
		FindAllStringSubmatch(gomod, -1) {
		if !slices.Contains(have, m[1]) {
			t.Errorf("go.mod names libmagic/%s, which is not a target folder. See D17", m[1])
		}
	}
}
