package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/xo/magic"
)

func main() {
	mime := flag.Bool("mime", false, "enable mime type")
	flag.Parse()
	if err := run(*mime, flag.Args()...); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(mime bool, files ...string) error {
	flags := magic.None
	if mime {
		flags = magic.MimeType
	}
	m, err := magic.New(flags)
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
