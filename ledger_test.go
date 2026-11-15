package magic_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// upstreamDir holds the list of upstreams and the ledger of each (D26).
var upstreamDir = filepath.Join("docs", "upstream")

// readTSV reads a file of tab-separated values. It fails unless the first
// line is the header that it expects and each line has as many columns.
func readTSV(t *testing.T, path string, header []string) [][]string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(read(t, path), "\n"), "\n")
	if got := strings.Join(header, "\t"); lines[0] != got {
		t.Fatalf("%s: expected the header %q, got: %q", path, got, lines[0])
	}
	var rows [][]string
	for i, line := range lines[1:] {
		cols := strings.Split(line, "\t")
		if len(cols) != len(header) {
			t.Errorf("%s:%d: expected %d columns, got: %d", path, i+2, len(header), len(cols))
			continue
		}
		rows = append(rows, cols)
	}
	return rows
}

var (
	fullHash  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	shortHash = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	isoDate   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// TestTheLedgerIsComplete holds D26. The ledger of each upstream has one line
// for each upstream commit after the base commit, in the order of upstream,
// with no gap. Each line has a status, and a ported line names the magic
// commit.
//
// A gap in the ledger is a change of upstream that nobody looked at. The
// checkout is needed to find one, so that part runs only where the checkout
// exists. The format of each line is checked everywhere.
func TestTheLedgerIsComplete(t *testing.T) {
	t.Parallel()
	ups := readTSV(t, filepath.Join(upstreamDir, "upstreams.tsv"),
		[]string{"repository", "url", "checkout", "ports", "magic", "policy", "base", "matched"})
	names := make([]string, 0, len(ups))
	for _, up := range ups {
		names = append(names, up[0])
	}
	if want := []string{"file", "binwalk-ng"}; !slices.Equal(names, want) {
		t.Fatalf("expected the upstreams %q in %s, got: %q. See D26 and D60", want, filepath.Join(upstreamDir, "upstreams.tsv"), names)
	}
	for _, up := range ups {
		t.Run(up[0], func(t *testing.T) {
			t.Parallel()
			checkLedger(t, up)
		})
	}
}

// checkLedger checks the ledger of one upstream, the row up of
// upstreams.tsv.
func checkLedger(t *testing.T, up []string) {
	name, checkout, base, matched := up[0], up[2], up[6], up[7]
	if !fullHash.MatchString(base) {
		t.Errorf("the base commit of %s is %q, not a full hash. See D26", name, base)
	}
	path := filepath.Join(upstreamDir, name+".tsv")
	rows := readTSV(t, path, []string{"upstream", "date", "status", "magic", "note"})
	status := map[string]bool{"ported": true, "not-applicable": true, "pending": true}
	seen := make(map[string]bool)
	pendingBefore := make(map[string]bool)
	var pending bool
	for i, r := range rows {
		line := i + 2
		hash, date, st, magic, note := r[0], r[1], r[2], r[3], r[4]
		switch {
		case !fullHash.MatchString(hash):
			t.Errorf("%s:%d: %q is not a full hash", path, line, hash)
		case seen[hash]:
			t.Errorf("%s:%d: %s is in the ledger twice", path, line, hash)
		}
		seen[hash] = true
		pendingBefore[hash] = pending
		if st == "pending" {
			pending = true
		}
		if !isoDate.MatchString(date) {
			t.Errorf("%s:%d: the date %q is not YYYY-MM-DD", path, line, date)
		}
		if !status[st] {
			t.Errorf("%s:%d: the status %q is not ported, not-applicable or pending. See D26", path, line, st)
		}
		if (st == "ported") != (magic != "") {
			t.Errorf("%s:%d: a ported line names the magic commit, and no other line does", path, line)
		}
		if magic != "" && !shortHash.MatchString(magic) {
			t.Errorf("%s:%d: the magic commit %q is not a hash", path, line, magic)
		}
		if strings.TrimSpace(note) == "" {
			t.Errorf("%s:%d: the note is empty. Write the subject of the commit and what it changes", path, line)
		}
	}
	if matched != "" {
		switch {
		case !seen[matched] && matched != base:
			t.Errorf("matched is %s, which is not in the ledger", matched)
		case pendingBefore[matched]:
			t.Errorf("matched is %s, and an earlier commit is still pending. See D26", matched)
		}
	}

	// The commits of upstream, when the checkout is here.
	if _, err := os.Stat(filepath.Join(checkout, ".git")); err != nil {
		t.Logf("no checkout in %s, so the ledger is not compared with upstream", checkout)
		return
	}
	if len(rows) == 0 {
		return
	}
	last := rows[len(rows)-1][0]
	out, err := exec.Command("git", "-C", checkout, "log", "--reverse", "--format=%H %cs", base+".."+last).Output()
	if err != nil {
		t.Fatalf("git log %s..%s in %s: %v. Fetch the checkout", base, last, checkout, err)
	}
	want := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(want) != len(rows) {
		t.Errorf("%s has %d lines, and upstream has %d commits from the base to %s. A commit is "+
			"missing or extra", path, len(rows), len(want), last)
	}
	for i := 0; i < len(want) && i < len(rows); i++ {
		hash, date, _ := strings.Cut(want[i], " ")
		if rows[i][0] != hash {
			t.Errorf("%s:%d: expected upstream commit %s here, got: %s", path, i+2, hash, rows[i][0])
			break
		}
		if rows[i][1] != date {
			t.Errorf("%s:%d: expected the date %s, got: %s", path, i+2, date, rows[i][1])
		}
	}
}
