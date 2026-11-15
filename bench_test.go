package magic

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkDetect times the public API on the sample files. Each call walks
// the rules once for each form of the Result (D46).
func BenchmarkDetect(b *testing.B) {
	for _, name := range []string{"gif.gif", "utf8.txt", "xlsx.xlsx", "tar.tar", "doc.doc"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			b.Fatal(err)
		}
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Detect(context.Background(), data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
