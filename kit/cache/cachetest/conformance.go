// Package cachetest is the one conformance suite every adapter of cache.Cache
// runs — the kernel's version of a module's contracts/<slug>test harness (T-0084).
//
// Both adapters in this repository call it: the in-process store in kit/cache's
// own test, the Valkey provider in its package's. That is the reason the
// in-process adapter is not allowed to be a different cache — it runs the same
// cases for the same reasons, so a green run over it is evidence and not theatre.
// Every case says what a wrong adapter would do, so the suite cannot be passed by
// an implementation that quietly means something else.
package cachetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
)

// tenantA and tenantB are two customers. They are fixed bytes rather than fresh
// uuids so a key that lets one reach the other is reproducible from the failure
// message alone.
var (
	tenantA = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	tenantB = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

// Store returns one handle onto the adapter's store.
//
// Calling it twice inside one case must return two handles onto **one** store:
// that is what "two replicas" means here. An adapter whose factory builds a fresh
// store per call cannot pass the sharing and the invalidation cases, which is the
// whole point of the suite. The suite closes what it is given.
type Store func(t *testing.T) cache.Cache

// Failing returns one handle onto a store that cannot be reached — no process
// listening, or one that stopped answering — and answers with an error within a
// bounded time rather than hanging.
type Failing func(t *testing.T) cache.Cache

// Conformance runs the adapter's cases.
func Conformance(t *testing.T, store Store, failing Failing) {
	t.Helper()

	// C1. A miss is a fact and not an outage. An adapter that surfaces "no such
	// key" as an error makes every cold cache look like a dead one, and the host
	// resolution that fails open on an error then fails open on every request.
	t.Run("a miss is not an error", func(t *testing.T) {
		c := fresh(t, store)
		val, found, err := c.Get(context.Background(), cache.Shared("host").Entry("nobody.example"))
		if err != nil || found || val != nil {
			t.Fatalf("Get of an absent key = %q, found=%v, err=%v; want nothing, false, no error", val, found, err)
		}
	})

	// C2. Byte for byte. An adapter that coerces the value through a type that
	// cannot hold a zero byte, or encodes on one side and not the other, corrupts
	// the envelope and the resolution comes back as nothing.
	t.Run("a value is read back byte for byte", func(t *testing.T) {
		c := fresh(t, store)
		want := []byte{0x00, 0xff, 0xfe, 'a', 0x00, 0x7f, 0xc3, 0x28}
		key := cache.Shared("host").Entry("example.com")
		if err := c.Set(context.Background(), key, want, time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
		got, found, err := c.Get(context.Background(), key)
		if err != nil || !found {
			t.Fatalf("Get: found=%v err=%v", found, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("Get = % x; want % x", got, want)
		}
	})

	// C3. An entry with no lifetime is refused. The measured default of a write
	// with no expiry is "forever" — the TTL of such a key answers -1s — so an
	// adapter that let it through would be holding a stale answer nobody owns.
	t.Run("an entry with no lifetime is refused", func(t *testing.T) {
		c := fresh(t, store)
		key := cache.Shared("host").Entry("example.com")
		for _, ttl := range []time.Duration{0, -time.Second} {
			err := c.Set(context.Background(), key, []byte("x"), ttl)
			if !errors.Is(err, cache.ErrNoLifetime) {
				t.Errorf("Set with ttl %s = %v; want ErrNoLifetime", ttl, err)
			}
			if _, found, err := c.Get(context.Background(), key); err != nil || found {
				t.Errorf("the refused Set wrote something: found=%v err=%v", found, err)
			}
		}
	})

	// C4. An entry expires. An adapter that ignores the lifetime never forgets a
	// suspended host, and the lifetime is the only thing standing between a failed
	// invalidation and a permanent one. Wide margins: this is the store's clock,
	// and no fake reaches it.
	t.Run("an entry expires", func(t *testing.T) {
		c := fresh(t, store)
		key := cache.Shared("host").Entry("example.com")
		if err := c.Set(context.Background(), key, []byte("x"), 60*time.Millisecond); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if _, found, err := c.Get(context.Background(), key); err != nil || !found {
			t.Fatalf("the entry was not there to begin with: found=%v err=%v", found, err)
		}
		time.Sleep(300 * time.Millisecond)
		if _, found, err := c.Get(context.Background(), key); err != nil || found {
			t.Errorf("the entry outlived its lifetime: found=%v err=%v", found, err)
		}
	})

	// C5. Two replicas share one entry: kit/limit's TestTwoReplicasShareOneLimit
	// argument, run against a value. The answer to a lookup may not depend on
	// which process asked.
	t.Run("two replicas share one entry", func(t *testing.T) {
		a, b := fresh(t, store), fresh(t, store)
		key := cache.Of(tenantA, "app").Entry("composed")
		if err := a.Set(context.Background(), key, []byte("one"), time.Minute); err != nil {
			t.Fatalf("Set through replica A: %v", err)
		}
		got, found, err := b.Get(context.Background(), key)
		if err != nil || !found || !bytes.Equal(got, []byte("one")) {
			t.Fatalf("replica B read %q, found=%v, err=%v; the store is not shared", got, found, err)
		}
	})

	// C6. What one replica deletes the other cannot read. This is the brief's own
	// Done-when in harness form: an invalidation that reaches only the process that
	// ran it is the bug this delivery exists to close.
	t.Run("what one replica deletes the other cannot read", func(t *testing.T) {
		a, b := fresh(t, store), fresh(t, store)
		key := cache.Of(tenantA, "app").Entry("composed")
		if err := a.Set(context.Background(), key, []byte("one"), time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if err := a.Delete(context.Background(), key); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, found, err := b.Get(context.Background(), key); err != nil || found {
			t.Fatalf("replica B still reads an entry replica A deleted: found=%v err=%v", found, err)
		}
		// The same invalidation run twice is not an error the second time: a route
		// that invalidates on a form submitted twice must not fail on the second
		// submission (modules/site's own rule, stated at its contract).
		if err := b.Delete(context.Background(), key); err != nil {
			t.Errorf("deleting a key nobody holds = %v; want no error", err)
		}
	})

	// C7. Another tenant's entry is unreachable. An adapter that dropped the
	// tenant segment from the key would answer A's bytes for B's question, and
	// that is the finding every other line of this policy stands on.
	t.Run("another tenant's entry is unreachable", func(t *testing.T) {
		c := fresh(t, store)
		key := cache.Of(tenantA, "app").Entry("settings")
		if err := c.Set(context.Background(), key, []byte("A's"), time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
		for _, other := range []cache.Key{
			cache.Of(tenantB, "app").Entry("settings"),
			cache.Shared("app").Entry("settings"),
			cache.Of(tenantA, "other").Entry("settings"),
		} {
			if _, found, err := c.Get(context.Background(), other); err != nil || found {
				t.Errorf("%s reads another owner's entry: found=%v err=%v", other, found, err)
			}
		}
	})

	// C7b. The scope that names no tenant is the one called Shared. A caller that
	// resolved nothing gets a refusal rather than a bucket every other unresolved
	// caller shares — which is what a "convention" about the nil tenant becomes.
	t.Run("a scope naming no tenant is refused", func(t *testing.T) {
		c := fresh(t, store)
		if err := c.Set(context.Background(), cache.Of(uuid.Nil, "app").Entry("x"), []byte("y"), time.Minute); !errors.Is(err, cache.ErrNoScope) {
			t.Errorf("Set under a scope built from the nil tenant = %v; want ErrNoScope", err)
		}
		if _, _, err := c.Get(context.Background(), cache.Key{}); !errors.Is(err, cache.ErrNoScope) {
			t.Errorf("Get of a key nobody built = %v; want ErrNoScope", err)
		}
		if err := c.Move(context.Background(), cache.Scope{}); !errors.Is(err, cache.ErrNoScope) {
			t.Errorf("Move of a scope nobody built = %v; want ErrNoScope", err)
		}
	})

	// C8. A name cannot forge another owner's prefix. The tenant id is thirty-six
	// fixed characters and comes first, so the name below stays inside tenant A's
	// key however much of tenant B's address it copies (kit/limit.scoped's
	// argument, which this case is the test for).
	t.Run("a key name cannot forge another owner's prefix", func(t *testing.T) {
		c := fresh(t, store)
		forged := cache.Of(tenantA, "app").Entry(tenantB.String() + "/secret")
		if err := c.Set(context.Background(), forged, []byte("A's"), time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if _, found, err := c.Get(context.Background(), cache.Of(tenantB, "app").Entry("secret")); err != nil || found {
			t.Errorf("tenant B read what tenant A wrote under B's own id: found=%v err=%v", found, err)
		}
	})

	// C9. A thousand keys in one invalidation. The batching belongs to this
	// package's store and each adapter's own transport, so the suite asserts the
	// work is done and every answer is gone; what it cost in commands is asserted
	// where commands are visible — providers/valkey counts them with a hook.
	t.Run("deleting a thousand keys is one invalidation", func(t *testing.T) {
		c := fresh(t, store)
		ctx := context.Background()
		scope := cache.Shared("bulk")
		keys := make([]cache.Key, 0, 1000)
		for i := range 1000 {
			k := scope.Entry(fmt.Sprintf("entry-%04d", i))
			if err := c.Set(ctx, k, []byte("x"), time.Minute); err != nil {
				t.Fatalf("Set %s: %v", k, err)
			}
			keys = append(keys, k)
		}
		if err := c.Delete(ctx, keys...); err != nil {
			t.Fatalf("Delete of 1000 keys: %v", err)
		}
		for _, k := range []cache.Key{keys[0], keys[500], keys[999]} {
			if _, found, err := c.Get(ctx, k); err != nil || found {
				t.Fatalf("%s survived the delete: found=%v err=%v", k, found, err)
			}
		}
	})

	// C10. One spelling, pinned to the byte. Any drift — a dropped app segment, a
	// different separator, a shortened uuid, a slash where a colon was — orphans
	// every entry written before it, which reads as a cache that came back empty
	// after a deploy. T-0228's adoption of this constructor passes this case
	// unchanged or is caught here.
	t.Run("the key spelling is one spelling", func(t *testing.T) {
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
	})

	// C11. A move closes what was written before it. The racing half of the case —
	// a write stamped with the generation the move closed, landing afterwards —
	// needs to order two commands, so it runs against the port itself in
	// Generations below; what an adapter owes is that its counter moves.
	t.Run("a move closes what was written before it", func(t *testing.T) {
		c := fresh(t, store)
		ctx := context.Background()
		scope := cache.Shared("host")
		live := scope.Entry("still.example")
		if err := c.Set(ctx, live, []byte("tenant"), time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if err := c.Move(ctx, scope); err != nil {
			t.Fatalf("Move: %v", err)
		}
		if _, found, err := c.Get(ctx, live); err != nil || found {
			t.Errorf("an entry written before the move survived it: found=%v err=%v", found, err)
		}
		// The Scope is not closed to new work: a Set that reads the open generation
		// after the move is believed.
		if err := c.Set(ctx, live, []byte("fresh"), time.Minute); err != nil {
			t.Fatalf("Set after the move: %v", err)
		}
		if got, found, err := c.Get(ctx, live); err != nil || !found || !bytes.Equal(got, []byte("fresh")) {
			t.Errorf("a write after the move was not believed: %q found=%v err=%v", got, found, err)
		}
		// A move of one Scope leaves every other owner's entries alone: closing
		// the installation's host index must not be a way to lose a tenant's own
		// composed value.
		mine := cache.Of(tenantA, "app").Entry("settings")
		if err := c.Set(ctx, mine, []byte("kept"), time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if err := c.Move(ctx, scope); err != nil {
			t.Fatalf("Move: %v", err)
		}
		if got, found, err := c.Get(ctx, mine); err != nil || !found || !bytes.Equal(got, []byte("kept")) {
			t.Errorf("moving another scope lost a tenant's entry: %q found=%v err=%v", got, found, err)
		}
	})

	// C12. An unreachable store is an error naming the operation, and nothing
	// panics. A panic in a middleware takes the process rather than the request,
	// and a caller that cannot tell which of its calls hung is a caller that fails
	// closed.
	t.Run("an unreachable store is an error and not a panic", func(t *testing.T) {
		c := fresh(t, failing)
		ctx := context.Background()
		key := cache.Shared("host").Entry("example.com")
		for name, call := range map[string]func() error{
			"Get":    func() error { _, _, err := c.Get(ctx, key); return err },
			"Set":    func() error { return c.Set(ctx, key, []byte("x"), time.Minute) },
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
	})

	// C13. Close is the Runtime's one release: safe to call twice, and everything
	// after it is refused rather than attempted against a closed handle.
	t.Run("a close releases the store and is idempotent", func(t *testing.T) {
		c := store(t)
		t.Cleanup(func() { _ = c.Close() })
		if err := c.Close(); err != nil {
			t.Fatalf("first Close: %v", err)
		}
		if err := c.Close(); !errors.Is(err, cache.ErrClosed) {
			t.Errorf("second Close = %v; want ErrClosed", err)
		}
		if _, _, err := c.Get(context.Background(), cache.Shared("host").Entry("example.com")); !errors.Is(err, cache.ErrClosed) {
			t.Errorf("Get after Close = %v; want ErrClosed", err)
		}
	})
}

// Generations runs the port's own invalidation rules against a store the suite
// controls, so the one interleaving that matters can be ordered rather than hoped
// for: the write that read the generation *before* the move and lands *after* it.
//
// It runs wherever kit/cache's store runs — both adapters share that code — and it
// is the case a delete-only adapter passes today and must not.
func Generations(t *testing.T) {
	t.Run("a write stamped with a closed generation is not believed", func(t *testing.T) {
		backend := &scripted{entries: map[string]stored{}, counters: map[string]int64{}}
		c, err := cache.New("pkit", backend)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx := context.Background()
		key := cache.Shared("host").Entry("acme.example")

		// Replica B misses, loads, and begins to write. Its first command is the
		// generation read; it stops there, before the write, and the suite holds it
		// there while the suspension is committed and invalidated in replica A.
		backend.pauseAfterRead()
		errc := make(chan error, 1)
		go func() { errc <- c.Set(ctx, key, []byte("the tenant as it was before the suspension"), time.Minute) }()
		<-backend.paused
		if err := c.Move(ctx, cache.Shared("host")); err != nil {
			t.Fatalf("Move during the racing write: %v", err)
		}
		backend.resume()
		if err := <-errc; err != nil {
			t.Fatalf("the racing Set: %v", err)
		}

		if _, found, err := c.Get(ctx, key); err != nil || found {
			t.Fatalf("the answer the installation revoked came back: found=%v err=%v; a delete alone leaves exactly this window", found, err)
		}
		// And the entry expires on its own: the resurrection is bounded even where
		// the invalidation is missed, which is why every Set carries a lifetime.
		if _, ok := backend.expiry(key.String()); !ok {
			t.Error("the racing write was stored with no expiry")
		}
	})

	t.Run("two concurrent moves are two moves and neither is lost", func(t *testing.T) {
		backend := &scripted{entries: map[string]stored{}, counters: map[string]int64{}}
		c, err := cache.New("pkit", backend)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx := context.Background()
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := c.Move(ctx, cache.Shared("host")); err != nil {
					t.Errorf("Move: %v", err)
				}
			}()
		}
		wg.Wait()
		if got := backend.counter("host#"); got != 8 {
			t.Errorf("eight moves left the generation at %d; a lost move is a suspension nobody honoured", got)
		}
	})
}

// Groups runs decision 0028 §4's bounded lazy composition over the same store, so
// a composed value is proved across processes by the same argument that proves a
// stored byte is.
func Groups(t *testing.T, store Store, failing Failing) {
	t.Helper()

	// G1. Nothing is composed until something asks. An instance that built every
	// client's router at boot is the thing 0028 §4 refuses.
	t.Run("nothing is composed until something asks", func(t *testing.T) {
		c := fresh(t, store)
		scope := cache.Shared("composition")
		var calls int
		g := cache.GroupOf(c, scope, time.Minute, 0, countingCompose(&calls, nil))
		if err := g.Forget(context.Background(), scope.Entry("nobody-asked")); err != nil {
			t.Fatalf("Forget: %v", err)
		}
		if calls != 0 {
			t.Errorf("a Group nobody read composed %d values; want none", calls)
		}
	})

	// G2. Fifty concurrent requests for a cold entry compose once. A cold cache at
	// the front of a spike is otherwise one composition per request.
	t.Run("concurrent misses compose once", func(t *testing.T) {
		c := fresh(t, store)
		scope := cache.Shared("composition")
		var (
			mu    sync.Mutex
			calls int
		)
		g := cache.GroupOf(c, scope, time.Minute, 0, func(context.Context, cache.Key) (int, error) {
			mu.Lock()
			calls++
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			return 7, nil
		})
		const readers = 50
		var (
			wg      sync.WaitGroup
			errs    = make([]error, readers)
			answers = make([]int, readers)
		)
		for i := range readers {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				answers[i], errs[i] = g.Get(context.Background(), scope.Entry("app"))
			}(i)
		}
		wg.Wait()
		for i := range readers {
			if errs[i] != nil {
				t.Fatalf("reader %d: %v", i, errs[i])
			}
			if answers[i] != 7 {
				t.Errorf("reader %d got %d; want 7", i, answers[i])
			}
		}
		if calls != 1 {
			t.Errorf("%d concurrent misses composed %d values; want one", readers, calls)
		}
	})

	// G3. A failed composition is refused and remembered nowhere. Caching the
	// failure would turn one blink into a lifetime of refusals for that host, and
	// 0028 §4's rule is that a client whose composition fails is a refusal for
	// that client and nothing else.
	t.Run("a failed composition is refused and not remembered", func(t *testing.T) {
		c := fresh(t, store)
		scope := cache.Shared("composition")
		boom := errors.New("the configuration this entry names does not exist")
		failing := true
		var calls int
		g := cache.GroupOf(c, scope, time.Minute, 0, func(context.Context, cache.Key) (int, error) {
			calls++
			if failing {
				return 0, boom
			}
			return 3, nil
		})
		ctx := context.Background()
		for range 2 {
			if _, err := g.Get(ctx, scope.Entry("app")); !errors.Is(err, boom) {
				t.Fatalf("a failing composition = %v; want the compose's own error", err)
			}
		}
		if calls != 2 {
			t.Errorf("%d attempts composed %d times; a failure was remembered", 2, calls)
		}
		failing = false
		if got, err := g.Get(ctx, scope.Entry("app")); err != nil || got != 3 {
			t.Errorf("the entry stayed poisoned after one refusal: %d, %v", got, err)
		}
	})

	// G4. The bound evicts, least recently used first, and an eviction costs
	// exactly one recomposition. An unbounded map here is a memory-exhaustion bug
	// keyed on a request header.
	t.Run("the bound evicts the least recently used", func(t *testing.T) {
		c := fresh(t, store)
		scope := cache.Shared("composition")
		var (
			mu    sync.Mutex
			calls = map[string]int{}
		)
		g := cache.GroupOf(c, scope, time.Minute, 2, func(_ context.Context, k cache.Key) (string, error) {
			mu.Lock()
			calls[k.Entry()]++
			mu.Unlock()
			return k.Entry(), nil
		})
		ctx := context.Background()
		composes := func(name string) int {
			mu.Lock()
			defer mu.Unlock()
			return calls[name]
		}
		for _, name := range []string{"a", "b", "a", "c"} {
			if _, err := g.Get(ctx, scope.Entry(name)); err != nil {
				t.Fatalf("Get %s: %v", name, err)
			}
		}
		if got := composes("b"); got != 1 {
			t.Fatalf("b composed %d times while filling the group; want once", got)
		}
		// "b" is the oldest of the two live entries, so "c" took its slot and "a",
		// which was read most recently, is still held: an eviction that took the
		// recently-used entry is a bound that recomposes everything.
		if _, err := g.Get(ctx, scope.Entry("a")); err != nil {
			t.Fatalf("Get a: %v", err)
		}
		if got := composes("a"); got != 1 {
			t.Errorf("the recently used entry was evicted: it composed %d times; want once", got)
		}
		if _, err := g.Get(ctx, scope.Entry("b")); err != nil {
			t.Fatalf("Get b: %v", err)
		}
		if got := composes("b"); got != 2 {
			t.Errorf("the least recently used entry composed %d times in total; want it evicted and recomposed", got)
		}
	})

	// G5. Forgetting reaches the other process. Two groups over one store: the
	// second holds its own copy of the value and must still notice that the first
	// one invalidated it. This is 0028 §4 with process-local eviction, which is
	// what exists today.
	t.Run("forgetting reaches the other process", func(t *testing.T) {
		scope := cache.Shared("composition")
		var (
			mu    sync.Mutex
			calls int
		)
		compose := func(context.Context, cache.Key) (int, error) {
			mu.Lock()
			calls++
			mu.Unlock()
			return 1, nil
		}
		a := cache.GroupOf(fresh(t, store), scope, time.Minute, 0, compose)
		b := cache.GroupOf(fresh(t, store), scope, time.Minute, 0, compose)
		ctx := context.Background()
		for _, g := range []*cache.Group[int]{a, b} {
			if _, err := g.Get(ctx, scope.Entry("app")); err != nil {
				t.Fatalf("Get: %v", err)
			}
		}
		mu.Lock()
		before := calls
		mu.Unlock()
		if before != 2 {
			t.Fatalf("two processes over one store composed %d values; want each to hold its own", before)
		}
		if err := a.Forget(ctx, scope.Entry("app")); err != nil {
			t.Fatalf("Forget through the first process: %v", err)
		}
		if _, err := b.Get(ctx, scope.Entry("app")); err != nil {
			t.Fatalf("Get through the second process after the first forgot: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if calls != before+1 {
			t.Errorf("the second process composed %d more values after the first forgot the entry; process-local eviction is back", calls-before)
		}
	})

	// G6. A marker nobody can read keeps the entry serving; a marker that answers
	// "gone" recomposes it. Fail-open decided in the wrong direction would either
	// stop serving tenants that are fine or never evict anything.
	t.Run("an unreachable marker keeps serving the entry", func(t *testing.T) {
		var calls int
		scope := cache.Shared("composition")
		good := fresh(t, store)
		sw := &switchable{Cache: good}
		g := cache.GroupOf(sw, scope, time.Minute, 0, countingCompose(&calls, nil)).WithLog(quiet())
		ctx := context.Background()
		if _, err := g.Get(ctx, scope.Entry("app")); err != nil || calls != 1 {
			t.Fatalf("first Get: %d composes, err=%v; want one and none", calls, err)
		}
		// The store stops answering. The value this process holds is served, and
		// no composition runs: an outage costs a lookup, not a refusal.
		sw.swap(fresh(t, failing))
		if _, err := g.Get(ctx, scope.Entry("app")); err != nil {
			t.Errorf("Get with the store down = %v; want the held value served", err)
		}
		if calls != 1 {
			t.Errorf("the store being down forced %d recompositions; want none", calls-1)
		}
		// A marker that another process forgot is not an outage, and the process
		// holding the value has to see it: the store answers "nothing here", and
		// the held value is dropped rather than served for its whole lifetime.
		reader := fresh(t, store)
		if err := reader.Delete(ctx, scope.Entry("app")); err != nil {
			t.Fatalf("the second process' Delete: %v", err)
		}
		sw.swap(good)
		if _, err := g.Get(ctx, scope.Entry("app")); err != nil {
			t.Fatalf("Get after the marker went away: %v", err)
		}
		if calls != 2 {
			t.Errorf("a marker nobody wrote composed %d times; want one more", calls-2)
		}
	})

	// G7. A value composed for one scope is never served for another. The bound is
	// a map keyed by name, and the name is the caller's; the scope is what keeps
	// two tenants' composed values apart inside one process.
	t.Run("one process's cache does not cross scopes", func(t *testing.T) {
		c := fresh(t, store)
		scope := cache.Of(tenantA, "composition")
		g := cache.GroupOf(c, scope, time.Minute, 0, func(_ context.Context, k cache.Key) (string, error) {
			return k.Entry(), nil
		})
		if _, err := g.Get(context.Background(), cache.Of(tenantB, "composition").Entry("app")); !errors.Is(err, cache.ErrScope) {
			t.Errorf("a read of another tenant's entry = %v; want ErrScope", err)
		}
	})
}

// fresh builds one handle and releases it when the case ends. Both Store and
// Failing are assignable here, because a handle onto a dead store is built the
// same way as a handle onto a live one.
func fresh(t *testing.T, store func(t *testing.T) cache.Cache) cache.Cache {
	t.Helper()
	c := store(t)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// countingCompose returns a compose that counts its calls and answers the entry's
// own name, so a case can tell which value it got back.
func countingCompose(calls *int, mu *sync.Mutex) func(context.Context, cache.Key) (string, error) {
	return func(_ context.Context, k cache.Key) (string, error) {
		if mu != nil {
			mu.Lock()
			defer mu.Unlock()
		}
		*calls++
		return k.Entry(), nil
	}
}

// switchable is a Cache whose store changes underneath a Group, which is the only
// way to ask "what happens to a composed value when the store goes away".
type switchable struct {
	mu sync.Mutex
	cache.Cache
}

func (s *switchable) swap(c cache.Cache) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Cache = c
}

func (s *switchable) Get(ctx context.Context, k cache.Key) ([]byte, bool, error) {
	return s.current().Get(ctx, k)
}

func (s *switchable) Set(ctx context.Context, k cache.Key, val []byte, ttl time.Duration) error {
	return s.current().Set(ctx, k, val, ttl)
}

func (s *switchable) Delete(ctx context.Context, keys ...cache.Key) error {
	return s.current().Delete(ctx, keys...)
}

func (s *switchable) Move(ctx context.Context, scope cache.Scope) error {
	return s.current().Move(ctx, scope)
}

func (s *switchable) Close() error { return s.current().Close() }

func (s *switchable) current() cache.Cache {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Cache
}

// quiet is a logger that says nothing, for a case whose whole subject is the line
// a failure writes. What the line says is asserted where the logger is observable:
// kit/httpx's own cases capture the handler's log.
func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(discard{}, nil)) }

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// stored is one entry of the scripted store, and expiry is what the port wrote for
// it — the assertion that no resurrection is forever.
type stored struct {
	val   []byte
	until time.Time
}

// scripted is a cache.Backend the suite drives: it behaves, except that it can be
// told to stop after answering one read until the suite lets it go. That pause is
// the race, and it is the reason this file holds a store of its own: the
// interleaving cannot be tested by hoping two goroutines meet at the right moment.
type scripted struct {
	mu       sync.Mutex
	entries  map[string]stored
	counters map[string]int64
	paused   chan struct{}
	release  chan struct{}
	armed    bool
	once     sync.Once
}

// pauseAfterRead arms the one pause: the next read answers, reports on paused and
// waits for resume. It fires once, because the invalidation the suite runs during
// the pause reads the same counter.
func (s *scripted) pauseAfterRead() {
	s.paused = make(chan struct{}, 1)
	s.release = make(chan struct{})
	s.armed = true
}

// resume lets the paused command finish.
func (s *scripted) resume() { s.once.Do(func() { close(s.release) }) }

// counter reads a generation by the name the Scope gave it, without the caller
// having to re-derive the whole key this package owns.
func (s *scripted) counter(name string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, n := range s.counters {
		if strings.HasSuffix(k, name) {
			return n
		}
	}
	return 0
}
func (s *scripted) expiry(name string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, e := range s.entries {
		if strings.HasSuffix(k, name) {
			return e.until, true
		}
	}
	return time.Time{}, false
}

func (s *scripted) GetMany(ctx context.Context, keys ...string) ([]cache.Value, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	now := time.Now()
	values := make([]cache.Value, 0, len(keys))
	for _, k := range keys {
		// A generation counter reads exactly as a server's does, so the port asks
		// one question of both halves of an entry and never has to know which kind
		// of key it was handed.
		if n, ok := s.counters[k]; ok {
			values = append(values, cache.Value{Val: []byte(strconv.FormatInt(n, 10)), Found: true})
			continue
		}
		e, ok := s.entries[k]
		if !ok || !e.until.After(now) {
			values = append(values, cache.Value{})
			continue
		}
		values = append(values, cache.Value{Val: e.val, Found: true})
	}
	armed := s.armed
	s.armed = false
	s.mu.Unlock()
	if armed {
		s.paused <- struct{}{}
		<-s.release
	}
	return values, nil
}

func (s *scripted) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = stored{val: val, until: time.Now().Add(ttl)}
	return nil
}

func (s *scripted) Delete(ctx context.Context, keys ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		delete(s.entries, k)
	}
	return nil
}

func (s *scripted) Raise(ctx context.Context, key string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters[key]++
	return s.counters[key], nil
}

func (s *scripted) Close() error { return nil }
