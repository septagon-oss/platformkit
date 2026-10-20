package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPoolBoundsTheWholeTenantWorkingSet(t *testing.T) {
	c := Memory("pooltest")
	p, err := PoolOf[string](c, "picture", time.Minute, 2)
	if err != nil {
		t.Fatal(err)
	}
	a, b := Of(uuid.New(), "picture"), Of(uuid.New(), "picture")
	keys := []Key{a.Entry("one"), b.Entry("one"), a.Entry("two")}
	var loads int
	load := func(ctx context.Context, k Key) string {
		v, err := p.Load(ctx, k, func(context.Context) (string, error) { loads++; return k.scope.tenant.String() + k.name, nil })
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	first := load(t.Context(), keys[0])
	second := load(t.Context(), keys[1])
	if first == second || loads != 2 {
		t.Fatal("tenant values collided")
	}
	load(t.Context(), keys[0])
	load(t.Context(), keys[2])
	load(t.Context(), keys[0])
	if loads != 3 {
		t.Fatal("recent value evicted")
	}
	load(t.Context(), keys[1])
	if loads != 4 || len(p.group.values) != 2 {
		t.Fatal("global LRU bound not enforced")
	}
	if err := p.Forget(t.Context(), keys[1]); err != nil {
		t.Fatal(err)
	}
	load(t.Context(), keys[1])
	if loads != 5 {
		t.Fatal("forgotten value retained")
	}
	// Expiry is fixed at insertion; a hit must not slide it forward.
	p.group.mu.Lock()
	expiry := p.group.values[keys[1]].expires
	p.group.mu.Unlock()
	load(t.Context(), keys[1])
	if !p.group.values[keys[1]].expires.Equal(expiry) {
		t.Fatal("sliding TTL")
	}
	p.group.mu.Lock()
	entry := p.group.values[keys[1]]
	entry.expires = time.Time{}
	p.group.values[keys[1]] = entry
	p.group.mu.Unlock()
	load(t.Context(), keys[1])
	if loads != 6 {
		t.Fatal("expired value retained")
	}
}
func TestPoolCoalescesMissesAndDoesNotRetainFailures(t *testing.T) {
	p, _ := PoolOf[string](Memory("coalesce"), "picture", time.Minute, 2)
	key := Of(uuid.New(), "picture").Entry("one")
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			v, err := p.Load(t.Context(), key, func(context.Context) (string, error) {
				if calls.Add(1) == 1 {
					close(entered)
				}
				<-release
				return "immutable", nil
			})
			if err != nil || v != "immutable" {
				t.Errorf("load %q: %v", v, err)
			}
		})
	}
	<-entered
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("composed %d times", calls.Load())
	}
	failure := errors.New("compose failed")
	bad := key.scope.Entry("bad")
	for range 2 {
		_, err := p.Load(t.Context(), bad, func(context.Context) (string, error) { calls.Add(1); return "", failure })
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
	}
	if calls.Load() != 3 {
		t.Fatal("failure cached")
	}
}
func TestPoolRefusesForeignKeysAndInvalidConfiguration(t *testing.T) {
	c := Memory("invalid")
	for _, args := range []struct {
		c   Cache
		ns  string
		ttl time.Duration
		max int
	}{{nil, "picture", time.Minute, 1}, {c, "", time.Minute, 1}, {c, "a/b", time.Minute, 1}, {c, "picture", 0, 1}, {c, "picture", time.Minute, 0}} {
		if _, err := PoolOf[string](args.c, args.ns, args.ttl, args.max); err == nil {
			t.Fatal("invalid pool configuration accepted")
		}
	}
	p, _ := PoolOf[string](c, "picture", time.Minute, 1)
	for _, key := range []Key{{}, Shared("picture").Entry("one"), Of(uuid.New(), "other").Entry("one")} {
		if _, err := p.Load(t.Context(), key, func(context.Context) (string, error) { t.Fatal("foreign loader called"); return "", nil }); err == nil {
			t.Fatal("foreign key admitted")
		}
		if err := p.Forget(t.Context(), key); err == nil {
			t.Fatal("foreign forget admitted")
		}
	}
	if _, err := p.Load(t.Context(), Of(uuid.New(), "picture").Entry("one"), nil); err == nil {
		t.Fatal("nil loader admitted")
	}
}
