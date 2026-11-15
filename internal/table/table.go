// Package table holds the compiled rules of file, at the commit that the
// port follows (D26), as the port embeds them (D35). The generator in gen/ writes rules.flate from a checkout of
// file, and TestTableMatchesTheCompiler in the test module checks it.
package table

//go:generate go run ./gen -git ../../upstream/file -ledger ../../docs/upstream/upstreams.tsv -o rules.flate

import (
	"bytes"
	"compress/flate"
	_ "embed"
	"fmt"
	"io"
	"sync"

	"github.com/xo/magic/internal/rules"
)

//go:embed rules.flate
var compressed []byte

var load = sync.OnceValues(func() (*rules.Database, error) {
	b, err := io.ReadAll(flate.NewReader(bytes.NewReader(compressed)))
	if err != nil {
		return nil, fmt.Errorf("expanding the rules: %w", err)
	}
	return rules.UnmarshalTable(b)
})

// Load returns the compiled rules. It expands them the first time.
func Load() (*rules.Database, error) {
	return load()
}
