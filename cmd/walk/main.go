//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Command walk is a small example of the package walk: it prints the files
// that are embedded in each file that it gets, as binwalk does.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/xo/magic/walk"
	_ "github.com/xo/magic/walk/format/all"
)

func main() {
	include := flag.String("y", "", "walk for these formats only, separated by commas")
	exclude := flag.String("x", "", "walk for every format but these, separated by commas")
	all := flag.Bool("a", false, "try the short signatures at every offset")
	flag.Parse()
	var opts []walk.Option
	if *include != "" {
		opts = append(opts, walk.WithFormats(strings.Split(*include, ",")...))
	}
	if *exclude != "" {
		opts = append(opts, walk.WithoutFormats(strings.Split(*exclude, ",")...))
	}
	if *all {
		opts = append(opts, walk.WithSearchAll())
	}
	status := 0
	for _, name := range flag.Args() {
		fmt.Printf("%s\n\n%-12s %-12s %s\n", name, "DECIMAL", "HEXADECIMAL", "DESCRIPTION")
		for e, err := range walk.WalkFile(context.Background(), name, opts...) {
			if err != nil {
				fmt.Fprintf(os.Stderr, "walk: %v\n", err)
				status = 1
				break
			}
			fmt.Printf("%-12d %-12s %s\n", e.Offset, fmt.Sprintf("0x%X", e.Offset), e.Description)
		}
		fmt.Println()
	}
	os.Exit(status)
}
