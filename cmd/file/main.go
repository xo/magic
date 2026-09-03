package main

import (
	"fmt"
	"os"

	"github.com/xo/magic"
)

func main() {
	if err := run(os.Args[1:]...); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(files ...string) error {
	m, err := magic.New(magic.None)
	if err != nil {
		return err
	}
	for _, f := range files {
		typ, err := m.File(f)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "%s: %s\n", f, typ)
	}
	return nil
}
