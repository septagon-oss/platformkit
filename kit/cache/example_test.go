package cache_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/internal/exampletest"
)

func ExampleCache_readThrough() {
	ctx := context.Background()
	store := cache.Memory("example-cache")
	defer func() {
		if err := store.Close(); err != nil {
			panic(err)
		}
	}()
	key := cache.Of(uuid.MustParse("11111111-1111-4111-8111-111111111111"), "greetings").Entry("welcome")
	loads := 0
	load := func() ([]byte, error) {
		loads++
		return []byte("hello"), nil
	}
	read := func() ([]byte, error) {
		value, found, under, err := store.Get(ctx, key)
		if err != nil || found {
			return value, err
		}
		value, err = load()
		if err != nil {
			return nil, err
		}
		return value, store.Set(ctx, key, value, time.Hour, under)
	}
	first, err := read()
	if err != nil {
		panic(err)
	}
	second, err := read()
	if err != nil {
		panic(err)
	}
	fmt.Println(string(first), string(second))
	fmt.Println("loads:", loads)
	// Output:
	// hello hello
	// loads: 1
}

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

func TestDocumentedExamplesExecute(t *testing.T) {
	exampletest.Execute(t, "ExampleCache_readThrough", "ExampleGetEntry")
}
