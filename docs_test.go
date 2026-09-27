package magic_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// These tests read the documents of the repository, not the package. They
// hold the layout that D15 adopted from dbmeta D110.

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

// decision is one decision in docs/PLAN.md.
type decision struct {
	num    int
	title  string
	status string
	anchor string
}

// decisionHeading matches the heading of a decision:
//
//	### D9. A handle is safe to share across goroutines. Decided. Amends D6.
//
// The status is Decided or Proposed, and then each decision that this one
// amends or that amends it.
var decisionHeading = regexp.MustCompile(`^### D(\d+)\. (.+?)\. ((?:Decided|Proposed)` +
	`(?:\. (?:Amends|Amended by) D\d+)*)\.$`)

// decisions reads every decision in docs/PLAN.md, in the order of the
// document. A small library keeps its decisions there (dbmeta D111).
func decisions(t *testing.T) []decision {
	t.Helper()
	var out []decision
	for i, line := range strings.Split(read(t, planPath), "\n") {
		if !strings.HasPrefix(line, "### D") {
			continue
		}
		m := decisionHeading.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("%s:%d: a decision heading is \"### D<n>. <title>. <status>.\", and the status "+
				"is Decided or Proposed, then \"Amends Dn\" or \"Amended by Dn\" if needed: %q",
				planPath, i+1, line)
			continue
		}
		num, _ := strconv.Atoi(m[1])
		out = append(out, decision{
			num:    num,
			title:  m[2],
			status: m[3],
			anchor: anchor(strings.TrimPrefix(line, "### ")),
		})
	}
	if len(out) == 0 {
		t.Fatalf("%s holds no decision, so there is nothing to test", planPath)
	}
	return out
}

// planPath is the document that holds the plan and the decisions.
var planPath = filepath.Join("docs", "PLAN.md")

// anchor returns the fragment that GitHub gives a heading. It keeps letters,
// digits, hyphens and underscores, changes each space to a hyphen and drops
// the rest.
func anchor(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return "#" + b.String()
}

// TestTheDecisionIndexIsComplete compares the index at the top of the
// decisions in docs/PLAN.md with their headings. A reader finds a decision by its
// number in that table, so a missing row or a stale status hides it.
func TestTheDecisionIndexIsComplete(t *testing.T) {
	t.Parallel()
	rows := make(map[int]string)
	for _, m := range regexp.MustCompile(`(?m)^\| \[D(\d+)\]\(.*$`).FindAllStringSubmatch(read(t, planPath), -1) {
		num, _ := strconv.Atoi(m[1])
		if _, ok := rows[num]; ok {
			t.Errorf("%s has two rows for D%d", planPath, num)
		}
		rows[num] = m[0]
	}
	written := make(map[int]bool)
	for i, d := range decisions(t) {
		if d.num != i+1 {
			t.Errorf("%s: decision %d in the document is D%d. The numbers run from D1 with no gap",
				planPath, i+1, d.num)
		}
		written[d.num] = true
		want := fmt.Sprintf("| [D%d](%s) | %s | %s |", d.num, d.anchor, d.title, d.status)
		switch got, ok := rows[d.num]; {
		case !ok:
			t.Errorf("D%d has no row in the index in %s. Add:\n%s", d.num, planPath, want)
		case got != want:
			t.Errorf("D%d: the row in the index in %s is\n%s\nand the heading says\n%s", d.num, planPath, got, want)
		}
	}
	for num := range rows {
		if !written[num] {
			t.Errorf("the index in %s has a row for D%d, and no heading holds it", planPath, num)
		}
	}
}

// TestAnAmendmentPointsBothWays fails unless both headings name each other
// when a decision amends an earlier one. A reader who finds the older decision otherwise
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
				t.Errorf("D%d says %q, and the heading of D%d does not say %q", num, m[0], other, want)
			}
		}
	}
}

// decisionRef matches a reference to a decision. A reference to a decision of
// another repository names that repository first, as in dbmeta D110. A bare
// number, as in D3, names a decision in docs/PLAN.md.
var decisionRef = regexp.MustCompile(`(?:\b([a-z0-9]+)\s+)?\bD(\d+)\b`)

// TestEveryDecisionReferenceExists fails on a bare reference, such as D3, to a
// decision that docs/PLAN.md does not hold. It reads the documents and the Go
// files of this repository, and not the skills, which are copies of another
// project.
func TestEveryDecisionReferenceExists(t *testing.T) {
	t.Parallel()
	written := make(map[string]bool)
	for _, d := range decisions(t) {
		written[strconv.Itoa(d.num)] = true
	}
	// The repositories whose decisions a document here can cite.
	repos := map[string]bool{
		"dbmeta": true, "dbimp": true, "usql": true, "dburl": true, "tblfmt": true,
	}
	var paths []string
	for _, pattern := range []string{"*.md", "*.go", filepath.Join("docs", "*.md"), filepath.Join("cmd", "*", "*.go")} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, matches...)
	}
	for _, path := range paths {
		for _, m := range decisionRef.FindAllStringSubmatch(read(t, path), -1) {
			if repos[m[1]] || written[m[2]] {
				continue
			}
			t.Errorf("%s: refers to D%s, which is not in %s. Name the repository first if it "+
				"is a decision of another repository, as in dbmeta D110", path, m[2], planPath)
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
