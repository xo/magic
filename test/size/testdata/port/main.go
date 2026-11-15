// Command port reads a file and prints its length and its MIME type, with
// the port. TestPortSize compares it with the program base.
package main

import (
	"fmt"
	"os"

	"github.com/xo/magic/internal/engine"
	"github.com/xo/magic/internal/table"
)

func main() {
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	db, err := table.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mime, err := engine.New(db).Buffer(b, engine.MimeType)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(len(b), mime)
}
