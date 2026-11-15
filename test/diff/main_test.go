// Package diff compares the port with the oracle of D30, on the corpus: the
// engine with every output flag, the public API with each form of a result,
// and the tests of file (docs/PLAN.md).
package diff

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/xo/magic/test/oracle"
)

// TestMain runs the tests in the environment of the reference (D33): the C
// locale and TZ=UTC. libmagic and the port print dates in local time, so on
// a machine in another time zone, every rule that prints a date would differ
// from the oracle, which runs with TZ=UTC. The first run of the gate found
// this on a machine in UTC+7.
func TestMain(m *testing.M) {
	for k, v := range map[string]string{"TZ": "UTC", "LC_ALL": "C"} {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintf(os.Stderr, "setting %s: %v\n", k, err)
			os.Exit(2)
		}
	}
	os.Exit(m.Run())
}

// compare returns how an answer of the port differs from the oracle, or ""
// when they agree. An error of the port ends with the text of magic_error.
func compare(want oracle.Answer, got string, gerr error) string {
	switch {
	case want.Err != "" && gerr == nil:
		return fmt.Sprintf("expected the error %q, got: %q", want.Err, got)
	case want.Err == "" && gerr != nil:
		return fmt.Sprintf("expected %q, got the error: %v", want.Text, gerr)
	case want.Err != "":
		if !strings.HasSuffix(gerr.Error(), want.Err) {
			return fmt.Sprintf("expected the error %q, got: %v", want.Err, gerr)
		}
		return ""
	case got != want.Text:
		return fmt.Sprintf("expected %q, got: %q", want.Text, got)
	}
	return ""
}
