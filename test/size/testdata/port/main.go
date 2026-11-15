// Command port reads a file and prints its length and its MIME type with
// magic. TestPortSize compares it with the program base.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/xo/magic"
)

func main() {
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	r, err := magic.Detect(context.Background(), b)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(len(b), r.MIME)
}
