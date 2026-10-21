package cache

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// beforeMarker answers every call from the store it wraps and runs reader just
// before each write reaches that store.
type beforeMarker struct {
	Cache
	reader func(ctx context.Context, key Key)
}

func (b beforeMarker) Set(ctx context.Context, key Key, val []byte, ttl time.Duration, under Generation) error {
	b.reader(ctx, key)
	return b.Cache.Set(ctx, key, val, ttl, under)
}

// A request that reads the entry while the first composition is still writing
// its marker must not throw the composed value away: the next request is served
// from this process and composes nothing.
func TestAReaderBeforeTheSharedMarkerCostsNoSecondComposition(t *testing.T) {
	var p *Pool[string]
	var writes int
	store := beforeMarker{Cache: Memory("publication"), reader: func(ctx context.Context, key Key) {
		writes++
		p.group.local(ctx, key)
	}}
	p, err := PoolOf[string](store, "picture", time.Minute, 2)
	if err != nil {
		t.Fatal(err)
	}
	key := Of(uuid.New(), "picture").Entry("one")
	var composed int
	for range 2 {
		v, err := p.Load(t.Context(), key, func(context.Context) (string, error) { composed++; return "immutable", nil })
		if err != nil || v != "immutable" {
			t.Fatalf("load %q: %v", v, err)
		}
	}
	if composed != 1 || writes != 1 {
		t.Fatalf("composed %d times with %d marker writes; want one of each", composed, writes)
	}
}
