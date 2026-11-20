package magic_test

import (
	"context"
	"fmt"

	"github.com/xo/magic"
)

func ExampleWithExtraRules() {
	ctx := context.Background()
	r, err := magic.DetectFile(ctx, "testdata/fb2.fb2")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(r.MIME, r.Extensions)
	r, err = magic.DetectFile(ctx, "testdata/fb2.fb2", magic.WithExtraRules())
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(r.MIME, r.Extensions)
	// Output:
	// text/xml []
	// application/x-fictionbook+xml [fb2]
}
