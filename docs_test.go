package magic_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// These tests read the documents of the repository, not the package. They
// hold the layout that D15 adopted from dbmeta D110, and the folder of
// decisions of D23.

// TestTheRootHoldsFourDocuments holds dbmeta D110, which D15 adopts. A
// document that appears in the root is one that nobody filed.
func TestTheRootHoldsFourDocuments(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"README.md": true, "AGENTS.md": true, "CLAUDE.md": true, "CONTRIBUTING.md": true,
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if !allowed[e.Name()] {
			t.Errorf("%s is in the repository root. Only README, AGENTS, CLAUDE and CONTRIBUTING "+
				"belong there. Move it to docs/. See D15", e.Name())
		}
	}
	for name := range allowed {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("expected %s in the repository root. See D15", name)
		}
	}
}

// decisionDir is the folder that holds the decisions (D23).
var decisionDir = filepath.Join("docs", "decisions")

// decision is one file in docs/decisions.
type decision struct {
	num    int
	title  string
	status string
	file   string
}

// decisionFile names a decision file: D, the number in three digits, and the
// title in lower case words joined by hyphens.
var decisionFile = regexp.MustCompile(`^D(\d{3})-[a-z0-9-]+\.md$`)

// decisionHead matches the start of a decision file:
//
//	# D9. A handle is safe to share across goroutines
//
//	Status: Decided. Amends D6.
//
// The status is Decided or Proposed, and then each decision that this one
// amends or that amends it.
var decisionHead = regexp.MustCompile(`\A# D(\d+)\. (.+)\n\nStatus: ((?:Decided|Proposed)` +
	`(?:\. (?:Amends|Amended by) D\d+)*)\.\n`)

// decisions reads every decision in docs/decisions, in the order of the
// numbers.
func decisions(t *testing.T) []decision {
	t.Helper()
	entries, err := os.ReadDir(decisionDir)
	if err != nil {
		t.Fatal(err)
	}
	var out []decision
	for _, e := range entries {
		if e.Name() == "README.md" {
			continue
		}
		name := decisionFile.FindStringSubmatch(e.Name())
		if name == nil {
			t.Errorf("%s: a decision file is named D, three digits, a hyphen and the title "+
				"in lower case words, such as D022-magic-becomes-a-pure-go-port-of-libmagic.md. See D23", e.Name())
			continue
		}
		m := decisionHead.FindStringSubmatch(read(t, filepath.Join(decisionDir, e.Name())))
		if m == nil {
			t.Errorf("%s: a decision opens with \"# D<n>. <title>\", a blank line and \"Status: <status>.\", "+
				"and the status is Decided or Proposed, then \"Amends Dn\" or \"Amended by Dn\" if needed. See D23",
				e.Name())
			continue
		}
		num, _ := strconv.Atoi(m[1])
		if file, _ := strconv.Atoi(name[1]); file != num {
			t.Errorf("%s holds D%d. The file name and the heading name one decision", e.Name(), num)
		}
		out = append(out, decision{num: num, title: m[2], status: m[3], file: e.Name()})
	}
	if len(out) == 0 {
		t.Fatalf("%s holds no decision, so there is nothing to test", decisionDir)
	}
	return out
}

// TestTheDecisionIndexIsComplete compares the table in docs/decisions/README.md
// with the decision files. A reader finds a decision by its number in that
// table, so a missing row or a stale status hides it.
func TestTheDecisionIndexIsComplete(t *testing.T) {
	t.Parallel()
	index := filepath.Join(decisionDir, "README.md")
	rows := make(map[int]string)
	for _, m := range regexp.MustCompile(`(?m)^\| D(\d+) \|.*$`).FindAllStringSubmatch(read(t, index), -1) {
		num, _ := strconv.Atoi(m[1])
		if _, ok := rows[num]; ok {
			t.Errorf("%s has two rows for D%d", index, num)
		}
		rows[num] = m[0]
	}
	written := make(map[int]bool)
	for i, d := range decisions(t) {
		if d.num != i+1 {
			t.Errorf("%s: decision %d in the folder is D%d. The numbers run from D1 with no gap",
				decisionDir, i+1, d.num)
		}
		written[d.num] = true
		want := fmt.Sprintf("| D%d | [%s](%s) | %s |", d.num, d.title, d.file, d.status)
		switch got, ok := rows[d.num]; {
		case !ok:
			t.Errorf("D%d has no row in %s. Add:\n%s", d.num, index, want)
		case got != want:
			t.Errorf("D%d: the row in %s is\n%s\nand the file says\n%s", d.num, index, got, want)
		}
	}
	for num := range rows {
		if !written[num] {
			t.Errorf("%s has a row for D%d, and no file holds it", index, num)
		}
	}
}

// TestAnAmendmentPointsBothWays fails unless both decisions name each other
// when one amends the other. A reader who finds the older decision otherwise
// gets a rule that no longer holds.
func TestAnAmendmentPointsBothWays(t *testing.T) {
	t.Parallel()
	status := make(map[int]string)
	for _, d := range decisions(t) {
		status[d.num] = d.status
	}
	reverse := map[string]string{"Amends": "Amended by", "Amended by": "Amends"}
	link := regexp.MustCompile(`(Amends|Amended by) D(\d+)`)
	for num, s := range status {
		for _, m := range link.FindAllStringSubmatch(s, -1) {
			other, _ := strconv.Atoi(m[2])
			want := fmt.Sprintf("%s D%d", reverse[m[1]], num)
			switch back, ok := status[other]; {
			case !ok:
				t.Errorf("D%d says %q, and D%d is not a decision", num, m[0], other)
			case !strings.Contains(back, want):
				t.Errorf("D%d says %q, and the status of D%d does not say %q", num, m[0], other, want)
			}
		}
	}
}

// decisionRef matches a reference to a decision. A reference to a decision of
// another repository names that repository first, as in dbmeta D110. A bare
// number, as in D3, names a decision in docs/decisions.
var decisionRef = regexp.MustCompile(`(?:\b([a-z0-9]+)\s+)?\bD(\d+)\b`)

// TestEveryDecisionReferenceExists fails on a bare reference, such as D3, to a
// decision that docs/decisions does not hold. It reads the documents and the
// Go files of this repository, and not the skills, which are copies of
// another project, nor the checkout of upstream.
func TestEveryDecisionReferenceExists(t *testing.T) {
	t.Parallel()
	written := make(map[int]bool)
	for _, d := range decisions(t) {
		written[d.num] = true
	}
	// The repositories whose decisions a document here can cite.
	repos := map[string]bool{
		"dbmeta": true, "dbimp": true, "usql": true, "dburl": true, "tblfmt": true,
		"okage": true, "transit": true, "resvg": true,
	}
	var paths []string
	for _, pattern := range []string{
		"*.md", "*.go",
		filepath.Join("docs", "*.md"),
		filepath.Join("docs", "decisions", "*.md"),
		filepath.Join("cmd", "*", "*.go"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, matches...)
	}
	for _, path := range paths {
		for _, m := range decisionRef.FindAllStringSubmatch(read(t, path), -1) {
			// A file name such as D022-... writes the number in three digits.
			num, _ := strconv.Atoi(m[2])
			if repos[m[1]] || written[num] {
				continue
			}
			t.Errorf("%s: refers to D%s, which is not in %s. Name the repository first if it "+
				"is a decision of another repository, as in dbmeta D110", path, m[2], decisionDir)
		}
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
