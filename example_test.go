package magic_test

import (
	"context"
	"fmt"
	"os"

	"github.com/xo/magic"
)

func ExampleDetect() {
	buf, err := os.ReadFile("testdata/gif.gif")
	if err != nil {
		fmt.Println(err)
		return
	}
	r, err := magic.Detect(context.Background(), buf)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(r.Description)
	fmt.Println(r.ContentType())
	fmt.Println(r.Extensions)
	// Output:
	// GIF image data, version 89a, 1375 x 825
	// image/gif; charset=binary
	// [gif]
}

func ExampleDetectFile() {
	r, err := magic.DetectFile(context.Background(), "testdata")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(r.Description)
	fmt.Println(r.MIME)
	// Output:
	// directory
	// inode/directory
}
