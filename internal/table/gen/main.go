// Command gen writes the table of the rules of file at a commit (D35). It
// reads the rule files that the build of file compiles, the list
// MAGIC_FRAGMENTS of magic/Makefile.am, from a git checkout, compiles them as
// libmagic does, and writes the table compressed with flate.
package main

import (
	"bytes"
	"compress/flate"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"

	"github.com/xo/magic/internal/rules"
)

func main() {
	repo := flag.String("git", "", "the checkout of file")
	commit := flag.String("commit", "", "the commit of file")
	out := flag.String("o", "rules.flate", "the file to write")
	flag.Parse()
	if err := run(*repo, *commit, *out); err != nil {
		fmt.Fprintf(os.Stderr, "gen: %v\n", err)
		os.Exit(1)
	}
}

// show returns a file of the checkout at the commit.
func show(repo, commit, path string) ([]byte, error) {
	b, err := exec.Command("git", "-C", repo, "show", commit+":"+path).Output()
	if err != nil {
		return nil, fmt.Errorf("reading %s at %s: %w", path, commit, err)
	}
	return b, nil
}

func run(repo, commit, out string) error {
	mk, err := show(repo, commit, "magic/Makefile.am")
	if err != nil {
		return err
	}
	start := bytes.Index(mk, []byte("MAGIC_FRAGMENTS ="))
	if start < 0 {
		return fmt.Errorf("finding MAGIC_FRAGMENTS in magic/Makefile.am")
	}
	end := bytes.Index(mk[start:], []byte("\n\n"))
	if end < 0 {
		end = len(mk) - start
	}
	var names []string
	for _, m := range regexp.MustCompile(`\$\(MAGIC_FRAGMENT_DIR\)/(\S+)`).FindAllSubmatch(mk[start:start+end], -1) {
		names = append(names, string(m[1]))
	}
	// libmagic sorts the paths of a folder with strcmp.
	slices.Sort(names)
	var files []rules.File
	for _, n := range names {
		b, err := show(repo, commit, "magic/Magdir/"+n)
		if err != nil {
			return err
		}
		files = append(files, rules.File{Name: "magic/" + n, Data: b})
	}
	db, err := rules.Compile(files)
	if err != nil {
		return fmt.Errorf("compiling the rules: %w", err)
	}
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		return fmt.Errorf("compressing the table: %w", err)
	}
	if _, err := w.Write(db.MarshalTable()); err != nil {
		return fmt.Errorf("compressing the table: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("compressing the table: %w", err)
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing the table: %w", err)
	}
	fmt.Printf("gen: %d files, %d bytes\n", len(files), buf.Len())
	return nil
}
