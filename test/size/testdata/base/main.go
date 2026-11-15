// Command base reads a file and prints its length. It is the program
// without magic that TestPortSize compares with.
package main

import (
	"fmt"
	"os"
)

func main() {
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(len(b))
}
