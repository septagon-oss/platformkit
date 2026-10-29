package cachetest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/cache"
)

// C12. An unreachable store is an error naming the operation, and nothing
// panics. A panic in a middleware takes the process rather than the request,
// and a caller that cannot tell which of its calls hung is a caller that fails
// closed.
func unreachableStore(t *testing.T, store Store, failing Failing) {
	c := fresh(t, failing)
	live := fresh(t, store)
	ctx := context.Background()
	key := cache.Shared("host").Entry("example.com")
	// The stamp the write below carries comes from a store that is answering,
	// so what the case refuses is a write a real caller could have made.
	_, _, under, err := live.Get(ctx, key)
	if err != nil {
		t.Fatalf("reading the generation to write under: %v", err)
	}
	for name, call := range map[string]func() error{
		"Get":    func() error { _, _, _, err := c.Get(ctx, key); return err },
		"Set":    func() error { return c.Set(ctx, key, []byte("x"), time.Minute, under) },
		"Delete": func() error { return c.Delete(ctx, key) },
		"Move":   func() error { return c.Move(ctx, cache.Shared("host")) },
	} {
		err := call()
		if err == nil {
			t.Errorf("%s of an unreachable store answered no error", name)
			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(name)) {
			t.Errorf("%s of an unreachable store = %v; want the sentence to name the operation", name, err)
		}
	}
}

// C13. Every case is handed a store of its own, and a release belongs to the
// case that called for it. The suite closes a case's store when that case ends,
// so a factory that reaches one store from two cases hands the later case a
// closed one, and the suite's verdict then depends on the order it ran in. The
// close happens inside this case and the cases it asks about are minted here, so
// the claim holds whatever else runs next.
func caseIsolation(t *testing.T, store Store) {
	c := fresh(t, store)
	ctx, key := context.Background(), cache.Shared("host").Entry("example.com")
	if _, _, _, err := c.Get(ctx, key); errors.Is(err, cache.ErrClosed) {
		t.Fatalf("the store this case was handed answers ErrClosed before anything here closed one: a release reached a store belonging to another case, or to an earlier run of this one")
	}
	if err := fill(t, c, key, []byte("this case"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// A nested run is how this case asks what a case that is not it is handed:
	// the *testing.T below is one no other case, and no later repetition of this
	// name, will ever be given. One before the release, one after it.
	var closed, shares bool
	reads := func(t *testing.T) bool {
		_, found, _, err := fresh(t, store).Get(ctx, key)
		switch {
		case errors.Is(err, cache.ErrClosed):
			closed = true
		case err != nil:
			t.Fatalf("Get of an absent key = %q, found=%v, err=%v; want nothing, false, no error", "", found, err)
		}
		return found
	}
	t.Run("the case before the release", func(t *testing.T) { shares = reads(t) })
	if closed {
		t.Fatal("a case was handed a store some earlier case closed: the suite gives every case a store that case owns alone")
	}
	if shares {
		t.Fatal("two cases reach one store: a case can pass on what an earlier one left behind, and the release this suite runs at the end of each case closes that store for the next")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close of this case's own store: %v", err)
	}
	t.Run("the case after the release", func(t *testing.T) { shares = reads(t) })
	if closed {
		t.Fatal("this case's Close reached the store the case after it was handed: a case may release its own store and no one else's")
	}
	if shares {
		t.Error("the store the case after this one was handed still holds what this case wrote: the suite's cases would read each other's entries")
	}
}

// C14. Close is the Runtime's one release: safe to call twice, and everything
// after it is refused rather than attempted against a closed handle.
//
// This case closes the store it was handed, so it is placed last and on purpose.
// An adapter whose backend has one connection cannot run it anywhere else; no
// other case may depend on where it sits.
func closeStore(t *testing.T, store Store) {
	c := store(t)
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); !errors.Is(err, cache.ErrClosed) {
		t.Errorf("second Close = %v; want ErrClosed", err)
	}
	if _, _, _, err := c.Get(context.Background(), cache.Shared("host").Entry("example.com")); !errors.Is(err, cache.ErrClosed) {
		t.Errorf("Get after Close = %v; want ErrClosed", err)
	}
}
