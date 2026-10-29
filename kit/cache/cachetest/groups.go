package cachetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/cache"
)

// Groups checks bounded lazy composition over the same store as Conformance.
// Each case owns its factories and cleanup.
func Groups(t *testing.T, store Store, failing Failing) {
	t.Helper()

	t.Run("nothing is composed until something asks", func(t *testing.T) { lazyComposition(t, store) })
	t.Run("concurrent misses compose once", func(t *testing.T) { concurrentMisses(t, store) })
	t.Run("a failed composition is refused and not remembered", func(t *testing.T) { failedComposition(t, store) })
	t.Run("the bound evicts the least recently used", func(t *testing.T) { leastRecentlyUsed(t, store) })
	t.Run("forgetting reaches the other process", func(t *testing.T) { sharedForget(t, store) })
	t.Run("an unreachable marker keeps serving the entry", func(t *testing.T) { unreachableMarker(t, store, failing) })
	t.Run("one process's cache does not cross scopes", func(t *testing.T) { groupScope(t, store) })
}

// G1. Nothing is composed until something asks. An instance that built every
// client's router at boot is the thing 0028 §4 refuses.
func lazyComposition(t *testing.T, store Store) {
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
}

// G2. Fifty concurrent requests for a cold entry compose once. A cold cache at
// the front of a spike is otherwise one composition per request.
func concurrentMisses(t *testing.T, store Store) {
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
}

// G3. A failed composition is refused and remembered nowhere. Caching the
// failure would turn one blink into a lifetime of refusals for that host, and
// 0028 §4's rule is that a client whose composition fails is a refusal for
// that client and nothing else.
func failedComposition(t *testing.T, store Store) {
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
}

// G4. The bound evicts, least recently used first, and an eviction costs
// exactly one recomposition. An unbounded map here is a memory-exhaustion bug
// keyed on a request header.
func leastRecentlyUsed(t *testing.T, store Store) {
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
}

// G5. Forgetting reaches the other process. Two groups over one store: the
// second holds its own copy of the value and must still notice that the first
// one invalidated it. This is 0028 §4 with process-local eviction, which is
// what exists today.
func sharedForget(t *testing.T, store Store) {
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
}

// G6. A marker nobody can read keeps the entry serving; a marker that answers
// "gone" recomposes it. Fail-open decided in the wrong direction would either
// stop serving tenants that are fine or never evict anything.
func unreachableMarker(t *testing.T, store Store, failing Failing) {
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
}

// G7. A value composed for one scope is never served for another. The bound is
// a map keyed by name, and the name is the caller's; the scope is what keeps
// two tenants' composed values apart inside one process.
func groupScope(t *testing.T, store Store) {
	c := fresh(t, store)
	scope := cache.Of(tenantA, "composition")
	g := cache.GroupOf(c, scope, time.Minute, 0, func(_ context.Context, k cache.Key) (string, error) {
		return k.Entry(), nil
	})
	if _, err := g.Get(context.Background(), cache.Of(tenantB, "composition").Entry("app")); !errors.Is(err, cache.ErrScope) {
		t.Errorf("a read of another tenant's entry = %v; want ErrScope", err)
	}
}
