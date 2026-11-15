package walk

import (
	"fmt"
	"io"
	"os"
)

// readFile reads f into memory, and fails when it holds more than limit
// bytes.
func readFile(f *os.File, limit int64) ([]byte, func(), error) {
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", f.Name(), err)
	}
	if int64(len(data)) > limit {
		return nil, nil, fmt.Errorf("%s is above the limit of %d bytes", f.Name(), limit)
	}
	return data, func() {}, nil
}
