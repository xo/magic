package magic

import (
	"testing"
)

func TestVersion(t *testing.T) {
	t.Logf("version: %s", Version())
}
