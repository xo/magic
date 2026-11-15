// Command file is a small example of the package magic: it prints the type
// of each file that it gets, as the file command does.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/xo/magic"
	_ "github.com/xo/magic/compress/bzip2"
	_ "github.com/xo/magic/compress/zlib"
)

func main() {
	mime := flag.Bool("mime", false, "print the MIME type and the charset")
	mimeType := flag.Bool("mime-type", false, "print the MIME type")
	ext := flag.Bool("extension", false, "print the extensions")
	all := flag.Bool("k", false, "print every match")
	z := flag.Bool("z", false, "look inside gzip, zlib and bzip2 data")
	brief := flag.Bool("b", false, "print no file name")
	flag.Parse()
	var opts []magic.Option
	if *all {
		opts = append(opts, magic.WithAll())
	}
	if *z {
		opts = append(opts, magic.WithDecompress())
	}
	status := 0
	for _, name := range flag.Args() {
		r, err := magic.DetectFile(context.Background(), name, opts...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "file: %v\n", err)
			status = 1
			continue
		}
		out := r.Description
		switch {
		case *mime:
			out = r.ContentType()
		case *mimeType:
			out = r.MIME
		case *ext:
			out = strings.Join(r.Extensions, "/")
			if out == "" {
				out = "???"
			}
		}
		if !*brief {
			out = name + ": " + out
		}
		fmt.Println(out)
	}
	os.Exit(status)
}
