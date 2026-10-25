package cache_test

import (
	"context"
	"fmt"
	"time"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/cache"
)

func ExampleGetEntry() {
	c := cache.MemoryFor(appname.Name(""))
	defer c.Close()
	ctx := context.Background()
	key := cache.Shared("host").Entry("example.com")
	entry, err := cache.GetEntry(ctx, c, key)
	if err != nil {
		panic(err)
	}
	fmt.Println("found:", entry.Found)
	if err := c.Set(ctx, key, []byte("resolved"), time.Minute, entry.Generation); err != nil {
		panic(err)
	}
	entry, err = cache.GetEntry(ctx, c, key)
	if err != nil {
		panic(err)
	}
	fmt.Println("found:", entry.Found, "value:", string(entry.Value))
	// Output:
	// found: false
	// found: true value: resolved
}
