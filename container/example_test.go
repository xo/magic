package container_test

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/xo/magic"
	"github.com/xo/magic/container"
)

// A program asks magic for the type of a file. When magic names a zip
// file, a Compound File or a Mobipocket book, the program asks container
// for the format inside, and keeps the answer of magic for Unknown.
func ExampleDetect() {
	b, err := os.ReadFile("../testdata/oxps.oxps")
	if err != nil {
		panic(err)
	}
	r, err := magic.Detect(context.Background(), b)
	if err != nil {
		panic(err)
	}
	mime := r.MIME
	switch mime {
	case "application/zip", "application/octet-stream", "application/encrypted",
		"application/x-mobipocket-ebook", "application/vnd.android.package-archive",
		"application/x-ios-app":
		f, err := container.Detect(bytes.NewReader(b), int64(len(b)), container.Limits{})
		if err == nil && f != container.Unknown {
			mime = f.MIME()
		}
	}
	fmt.Println(r.MIME)
	fmt.Println(mime)
	// Output:
	// application/zip
	// application/oxps
}
