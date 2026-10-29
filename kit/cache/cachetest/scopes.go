package cachetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
)

// C5. Two replicas share one entry: kit/limit's TestTwoReplicasShareOneLimit
// argument, run against a value. The answer to a lookup may not depend on
// which process asked.
func sharedEntry(t *testing.T, store Store) {
	a, b := fresh(t, store), fresh(t, store)
	key := cache.Of(tenantA, "app").Entry("composed")
	if err := fill(t, a, key, []byte("one"), time.Minute); err != nil {
		t.Fatalf("Set through replica A: %v", err)
	}
	got, found, _, err := b.Get(context.Background(), key)
	if err != nil || !found || !bytes.Equal(got, []byte("one")) {
		t.Fatalf("replica B read %q, found=%v, err=%v; the store is not shared", got, found, err)
	}
}

// C6. What one replica deletes the other cannot read. This is the brief's own
// Done-when in harness form: an invalidation that reaches only the process that
// ran it is the bug this delivery exists to close.
func sharedDelete(t *testing.T, store Store) {
	a, b := fresh(t, store), fresh(t, store)
	key := cache.Of(tenantA, "app").Entry("composed")
	if err := fill(t, a, key, []byte("one"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := a.Delete(context.Background(), key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found, _, err := b.Get(context.Background(), key); err != nil || found {
		t.Fatalf("replica B still reads an entry replica A deleted: found=%v err=%v", found, err)
	}
	// The same invalidation run twice is not an error the second time: a route
	// that invalidates on a form submitted twice must not fail on the second
	// submission (modules/site's own rule, stated at its contract).
	if err := b.Delete(context.Background(), key); err != nil {
		t.Errorf("deleting a key nobody holds = %v; want no error", err)
	}
}

// A tenant, shared scope and namespace must have independent entries.
func tenantIsolation(t *testing.T, store Store) {
	c := fresh(t, store)
	key := cache.Of(tenantA, "app").Entry("settings")
	if err := fill(t, c, key, []byte("A's"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	for _, other := range []cache.Key{
		cache.Of(tenantB, "app").Entry("settings"),
		cache.Shared("app").Entry("settings"),
		cache.Of(tenantA, "other").Entry("settings"),
	} {
		if _, found, _, err := c.Get(context.Background(), other); err != nil || found {
			t.Errorf("%s reads another owner's entry: found=%v err=%v", other, found, err)
		}
	}
}

// C7b. The scope that names no tenant is the one called Shared. A caller that
// resolved nothing gets a refusal rather than a bucket every other unresolved
// caller shares — which is what a "convention" about the nil tenant becomes.
func requireScope(t *testing.T, store Store) {
	c := fresh(t, store)
	if err := c.Set(context.Background(), cache.Of(uuid.Nil, "app").Entry("x"), []byte("y"), time.Minute, cache.Generation{}); !errors.Is(err, cache.ErrNoScope) {
		t.Errorf("Set under a scope built from the nil tenant = %v; want ErrNoScope", err)
	}
	if _, _, _, err := c.Get(context.Background(), cache.Key{}); !errors.Is(err, cache.ErrNoScope) {
		t.Errorf("Get of a key nobody built = %v; want ErrNoScope", err)
	}
	if err := c.Move(context.Background(), cache.Scope{}); !errors.Is(err, cache.ErrNoScope) {
		t.Errorf("Move of a scope nobody built = %v; want ErrNoScope", err)
	}
}

// C8. A name cannot forge another owner's prefix. The tenant id is thirty-six
// fixed characters and comes first, so the name below stays inside tenant A's
// key however much of tenant B's address it copies (kit/limit.scoped's
// argument, which this case is the test for).
func keyPrefix(t *testing.T, store Store) {
	c := fresh(t, store)
	forged := cache.Of(tenantA, "app").Entry(tenantB.String() + "/secret")
	if err := fill(t, c, forged, []byte("A's"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, found, _, err := c.Get(context.Background(), cache.Of(tenantB, "app").Entry("secret")); err != nil || found {
		t.Errorf("tenant B read what tenant A wrote under B's own id: found=%v err=%v", found, err)
	}
}

// C9. A thousand keys in one invalidation. The batching belongs to this
// package's store and each adapter's own transport, so the suite asserts the
// work is done and every answer is gone; what it cost in commands is asserted
// where commands are visible — providers/valkey counts them with a hook.
func bulkDelete(t *testing.T, store Store) {
	c := fresh(t, store)
	ctx := context.Background()
	scope := cache.Shared("bulk")
	_, _, under, err := c.Get(ctx, scope.Entry("entry-0000"))
	if err != nil {
		t.Fatalf("reading the generation to write under: %v", err)
	}
	keys := make([]cache.Key, 0, 1000)
	for i := range 1000 {
		k := scope.Entry(fmt.Sprintf("entry-%04d", i))
		if err := c.Set(ctx, k, []byte("x"), time.Minute, under); err != nil {
			t.Fatalf("Set %s: %v", k, err)
		}
		keys = append(keys, k)
	}
	if err := c.Delete(ctx, keys...); err != nil {
		t.Fatalf("Delete of 1000 keys: %v", err)
	}
	for _, k := range []cache.Key{keys[0], keys[500], keys[999]} {
		if _, found, _, err := c.Get(ctx, k); err != nil || found {
			t.Fatalf("%s survived the delete: found=%v err=%v", k, found, err)
		}
	}
}

// C10. One spelling, pinned to the byte. Any drift — a dropped app segment, a
// different separator, a shortened uuid, a slash where a colon was — orphans
// every entry written before it, which reads as a cache that came back empty
// after a deploy. A constructor that replaces this one passes this case
// unchanged or is caught here.
func keySpelling(t *testing.T) {
	id := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	for _, pin := range []struct{ got, want string }{
		{cache.CacheKey("pkit", id, "host/example.com"), "pkit:33333333-3333-4333-8333-333333333333:host/example.com"},
		{cache.CacheKey("", id, "host/example.com"), ":33333333-3333-4333-8333-333333333333:host/example.com"},
		// A name carrying a colon and a uuid changes nothing before it.
		{cache.CacheKey("pkit", id, "ns/"+id.String()+":x"), "pkit:33333333-3333-4333-8333-333333333333:ns/33333333-3333-4333-8333-333333333333:x"},
	} {
		if pin.got != pin.want {
			t.Errorf("CacheKey = %q; want %q", pin.got, pin.want)
		}
	}
	for _, bad := range []string{"", "Pkit", "pk--it", "-pkit", "pkit-", strings.Repeat("a", 33), "pk it"} {
		if _, err := cache.Slug(bad); err == nil {
			t.Errorf("Slug accepted %q", bad)
		}
	}
	// The refusal names the grammar and never the value that arrived: a
	// refused configuration is quoted in a log, and the value is a client's.
	if _, err := cache.Slug("Not-A-Client-Slug"); !strings.Contains(err.Error(), "lower-case") {
		t.Errorf("Slug's refusal does not name the grammar: %v", err)
	}
	if got, err := cache.Slug("pkit"); err != nil || got != "pkit" {
		t.Errorf("Slug refused a good name: %q, %v", got, err)
	}
}

// C11. A move closes what was written before it. The racing half of the case —
// a write stamped with the generation the move closed, landing afterwards —
// needs to order two commands, so it runs against the port itself in
// Generations below; what an adapter owes is that its counter moves.
func moveScope(t *testing.T, store Store) {
	c := fresh(t, store)
	ctx := context.Background()
	scope := cache.Shared("host")
	live := scope.Entry("still.example")
	if err := fill(t, c, live, []byte("tenant"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.Move(ctx, scope); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, found, _, err := c.Get(ctx, live); err != nil || found {
		t.Errorf("an entry written before the move survived it: found=%v err=%v", found, err)
	}
	// The Scope is not closed to new work: a Set stamped by a read made after
	// the move is believed. The stamp, not the clock, is what a write lives or
	// dies by — Generations below is the same rule from the other side.
	if err := fill(t, c, live, []byte("fresh"), time.Minute); err != nil {
		t.Fatalf("Set after the move: %v", err)
	}
	if got, found, _, err := c.Get(ctx, live); err != nil || !found || !bytes.Equal(got, []byte("fresh")) {
		t.Errorf("a write after the move was not believed: %q found=%v err=%v", got, found, err)
	}
	// A move of one Scope leaves every other owner's entries alone: closing
	// the installation's host index must not be a way to lose a tenant's own
	// composed value.
	mine := cache.Of(tenantA, "app").Entry("settings")
	if err := fill(t, c, mine, []byte("kept"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.Move(ctx, scope); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if got, found, _, err := c.Get(ctx, mine); err != nil || !found || !bytes.Equal(got, []byte("kept")) {
		t.Errorf("moving another scope lost a tenant's entry: %q found=%v err=%v", got, found, err)
	}
}
