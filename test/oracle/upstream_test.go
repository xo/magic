package oracle

import (
	"testing"
)

// TestOracleMatchesTheTestsOfFile holds D30. The oracle is the reference only
// if it gives the result that file expects for each of its own tests. A
// difference here is a fault of the build of the oracle, not of the port.
func TestOracleMatchesTheTestsOfFile(t *testing.T) {
	t.Parallel()
	o := Open(t)
	cases, err := UpstreamCases()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			got, err := o.Ask(Question{Op: "file", Flags: c.Flags, Path: c.TestFile, Magic: c.Magic})
			if err != nil {
				t.Fatal(err)
			}
			if got.Err != "" {
				t.Fatalf("expected no error, got: %s", got.Err)
			}
			if got.Text != c.Want {
				t.Errorf("expected %q, got: %q", c.Want, got.Text)
			}
		})
	}
	t.Logf("%d tests of file", len(cases))
}
