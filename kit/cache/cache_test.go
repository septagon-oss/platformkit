package cache_test

// The in-process adapter runs the same suite the server-backed one does, and it is
// the reason a green run over it is evidence: the map is not a second
// implementation of the invalidation, it is the same store code with a mutex
// behind it. A case that fails here fails identically against Valkey, and the two
// failing halves of that claim are what C5, C6 and C11 are for.

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/cache/cachetest"
)

// TestTheInProcessStoreConforms is C1 to C13 over one mutex and one map.
func TestTheInProcessStoreConforms(t *testing.T) {
	cachetest.Conformance(t, memory(t), broken(t))
}

// TestThePortClosesWhatItMoved is C11's racing half: the write stamped by the read
// that came before the invalidation and landed after it. It runs against the port
// itself, which is the code both adapters share.
func TestThePortClosesWhatItMoved(t *testing.T) {
	cachetest.Generations(t)
}

// TestComposedValuesAreBoundedAndShared is G1 to G7 — decision 0028 §4.
func TestComposedValuesAreBoundedAndShared(t *testing.T) {
	cachetest.Groups(t, memory(t), broken(t))
}

// write stores one value the way a caller does: the write is stamped with the
// generation a read of this key answered open, and there is no other way to write.
func write(t *testing.T, c cache.Cache, key cache.Key, val []byte, ttl time.Duration) error {
	t.Helper()
	_, _, under, err := c.Get(context.Background(), key)
	if err != nil {
		return err
	}
	return c.Set(context.Background(), key, val, ttl, under)
}

// TestTheKeyNamesTheTenantFirst is the type's rule stated as bytes: two customers
// asking the same question of the same namespace get different addresses, in the
// same process and in the same store, whatever the name says.
func TestTheKeyNamesTheTenantFirst(t *testing.T) {
	const ns = "host"
	c := memory(t)(t)
	ctx := context.Background()
	first, second := uuid.New(), uuid.New()
	key := cache.Of(first, ns).Entry("example.com")
	if err := write(t, c, key, []byte("first"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, found, _, err := c.Get(ctx, cache.Of(second, ns).Entry("example.com")); err != nil || found {
		t.Fatalf("a second tenant read the first one's entry: found=%v err=%v", found, err)
	}
	// A name that repeats another tenant's whole address is still a name inside
	// this one's key: the tenant segment is fixed-width and first.
	forged := cache.Of(first, ns).Entry(second.String() + "/example.com")
	if err := write(t, c, forged, []byte("first"), time.Minute); err != nil {
		t.Fatalf("Set of a forged name: %v", err)
	}
	if _, found, _, err := c.Get(ctx, cache.Of(second, ns).Entry("example.com")); err != nil || found {
		t.Fatalf("a forged name reached another tenant's address: found=%v err=%v", found, err)
	}
}

// TestAMoveReachesEveryReaderOfTheNamespace is the brief's Done-when, in the form
// the kernel can prove without a second machine: one store, two readers, and an
// invalidation in one that the other obeys.
func TestAMoveReachesEveryReaderOfTheNamespace(t *testing.T) {
	ctx := context.Background()
	writer, reader := memory(t)(t), memory(t)(t)
	host := cache.Shared("host").Entry("acme.example")
	if err := write(t, writer, host, []byte("the tenant behind it"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, found, _, err := reader.Get(ctx, host); err != nil || !found {
		t.Fatalf("the second reader cannot see what the first wrote: found=%v err=%v", found, err)
	}
	if err := writer.Move(ctx, cache.Shared("host")); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, found, _, err := reader.Get(ctx, host); err != nil || found {
		t.Fatalf("the second reader still believes a closed generation: found=%v err=%v", found, err)
	}
}

// TestANamespaceThatNamesNothingWritesNothing is the refusal a caller meets when it
// assembled a Scope rather than going through Of or Shared.
func TestANamespaceThatNamesNothingWritesNothing(t *testing.T) {
	c := memory(t)(t)
	ctx := context.Background()
	if err := c.Set(ctx, cache.Shared("").Entry("x"), []byte("y"), time.Minute, cache.Generation{}); err == nil {
		t.Error("Set under a scope with no namespace wrote something")
	}
	if err := c.Set(ctx, cache.Shared("a/b").Entry("x"), []byte("y"), time.Minute, cache.Generation{}); err == nil {
		t.Error("Set under a namespace holding a slash wrote something: a namespace with a slash in it cannot be separated from its entries")
	}
	if err := c.Set(ctx, cache.Shared("host").Entry(""), []byte("y"), time.Minute, cache.Generation{}); err == nil {
		t.Error("Set of an entry with no name wrote something")
	}
}

// TestAnApplicationNameIsCheckedOnce names when the shape of a key is decided: a
// store that was never told which application it serves would put two clients'
// identically-named entries in one bucket.
func TestAnApplicationNameIsCheckedOnce(t *testing.T) {
	for _, bad := range []string{"", "Pkit", "collect.example", "pkit/"} {
		if _, err := cache.New(bad, cache.MemoryBackend()); err == nil {
			t.Errorf("New accepted the application name %q", bad)
		}
	}
	c, err := cache.New("platformkit", cache.MemoryBackend())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
}

// TestTheBudgetBoundsAStoreThatNeverAnswers: a cache in front of a database that
// is the truth costs its caller this much and an error, never a request that
// never ends. The margin is the point, not the number.
func TestTheBudgetBoundsAStoreThatNeverAnswers(t *testing.T) {
	c, err := cache.New("pkit", stalled{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	start := time.Now()
	if _, _, _, err := c.Get(context.Background(), cache.Shared("host").Entry("example.com")); err == nil {
		t.Fatal("a store that never answered produced no error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("a stalled store cost %s; the budget is what a lookup may cost, not the caller's patience", elapsed)
	}
	// A caller whose own deadline went away mid-flight is not a reason to abandon
	// an invalidation that follows a committed write, and it is not a reason to
	// wait forever either.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start = time.Now()
	if err := c.Move(ctx, cache.Shared("host")); err == nil {
		t.Error("Move through a cancelled context and a stalled store answered no error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("a cancelled caller waited %s on a stalled store", elapsed)
	}
}

// stalled is a store that answers nothing. Every command blocks until its context
// is done, which is the outage the budget exists to bound.
type stalled struct{}

func (stalled) GetMany(ctx context.Context, _ ...string) ([]cache.Value, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (stalled) Set(ctx context.Context, _ string, _ []byte, _ time.Duration) error {
	<-ctx.Done()
	return ctx.Err()
}

func (stalled) Delete(ctx context.Context, _ ...string) error {
	<-ctx.Done()
	return ctx.Err()
}

func (stalled) Raise(ctx context.Context, _ string) (int64, error) {
	<-ctx.Done()
	return 0, ctx.Err()
}

func (stalled) Close() error { return nil }

// memory returns a Store factory: every handle built for one case is a reader of
// one store, because "two replicas" is the arrangement half the suite is about,
// and every case gets a store of its own so a case cannot read another's entries.
func memory(t *testing.T) cachetest.Store {
	t.Helper()
	return func(t *testing.T) cache.Cache {
		t.Helper()
		c, err := cache.New("pkit", shared(t))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return c
	}
}

// broken returns a Failing factory: a store whose every command fails, which is
// what "unreachable" means to a caller and the same shape a connection refused
// three milliseconds in answers.
func broken(t *testing.T) cachetest.Failing {
	t.Helper()
	return func(t *testing.T) cache.Cache {
		t.Helper()
		c, err := cache.New("pkit", unreachable{})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return c
	}
}

// unreachable is the store behind no address. The error text names the operation
// the port puts around it, so the sentence the suite reads is the port's own.
type unreachable struct{}

func (unreachable) GetMany(context.Context, ...string) ([]cache.Value, error) {
	return nil, fmt.Errorf("dial tcp 127.0.0.1:1: connect: connection refused")
}

func (unreachable) Set(context.Context, string, []byte, time.Duration) error {
	return fmt.Errorf("dial tcp 127.0.0.1:1: connect: connection refused")
}

func (unreachable) Delete(context.Context, ...string) error {
	return fmt.Errorf("dial tcp 127.0.0.1:1: connect: connection refused")
}

func (unreachable) Raise(context.Context, string) (int64, error) {
	return 0, fmt.Errorf("dial tcp 127.0.0.1:1: connect: connection refused")
}

func (unreachable) Close() error { return nil }

// stores is one store per test name, so the two handles one case builds read one
// map and the next case starts clean. The Close the suite runs is the handle's and
// not the store's: it releases nothing this map has to keep.
var stores sync.Map // test name -> cache.Backend

func shared(t *testing.T) cache.Backend {
	t.Helper()
	if b, ok := stores.Load(t.Name()); ok {
		return b.(cache.Backend)
	}
	b := cache.MemoryBackend()
	actual, _ := stores.LoadOrStore(t.Name(), b)
	return actual.(cache.Backend)
}
