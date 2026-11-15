// Package diff compares the code under test with the oracle of D30, on the
// corpus. It tests the cgo package of today, which is the gate of phase 1,
// and the port, which is the gate of phases 2 and 3 (docs/PLAN.md).
package diff

import (
	"fmt"
	"os"
	"testing"
)

// TestMain runs the tests in the environment of the reference (D33): the C
// locale and TZ=UTC. libmagic prints dates in local time, so on a machine in
// another time zone, every rule that prints a date differs from the oracle.
// The first run of the gate found this on a machine in UTC+7. With cgo,
// os.Setenv also sets the variable for C, and libmagic reads TZ when it
// formats a date.
func TestMain(m *testing.M) {
	for k, v := range map[string]string{"TZ": "UTC", "LC_ALL": "C"} {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintf(os.Stderr, "setting %s: %v\n", k, err)
			os.Exit(2)
		}
	}
	os.Exit(m.Run())
}
