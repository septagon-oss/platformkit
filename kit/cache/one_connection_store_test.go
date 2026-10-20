package cache_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/cache/cachetest"
)

// TestAOneConnectionStoreConformsWhenEachCaseHasItsOwnKeyspace holds the shape
// cachetest.Store prescribes for a backend that cannot open a second store: one
// connection behind every case, a handle whose Close releases only that handle,
// and a keyspace the case owns alone. The suite runs over it twice in one process,
// so a later run reaches the connection an earlier run's close cases released.
func TestAOneConnectionStoreConformsWhenEachCaseHasItsOwnKeyspace(t *testing.T) {
	connection := cache.MemoryBackend()
	t.Cleanup(func() { _ = connection.Close() })
	var minted atomic.Int64
	var spaces sync.Map // *testing.T -> the app slug that is the case's keyspace
	store := func(t *testing.T) cache.Cache {
		t.Helper()
		space, loaded := spaces.LoadOrStore(t, fmt.Sprintf("case-%d", minted.Add(1)))
		if !loaded {
			t.Cleanup(func() { spaces.Delete(t) })
		}
		c, err := cache.New(space.(string), &handle{Backend: connection})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return c
	}
	for range 2 {
		t.Run("run", func(t *testing.T) { cachetest.Conformance(t, store, broken(t)) })
	}
	if _, err := connection.GetMany(context.Background(), "after"); err != nil {
		t.Errorf("the one connection answers %v after the suite: a case's Close reached it", err)
	}
}

// handle is one case's view of the shared connection: its Close ends this handle
// and leaves the connection open for every other.
type handle struct {
	cache.Backend
	closed atomic.Bool
}

func (h *handle) GetMany(ctx context.Context, keys ...string) ([]cache.Value, error) {
	if h.closed.Load() {
		return nil, cache.ErrClosed
	}
	return h.Backend.GetMany(ctx, keys...)
}

func (h *handle) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	if h.closed.Load() {
		return cache.ErrClosed
	}
	return h.Backend.Set(ctx, key, val, ttl)
}

func (h *handle) Delete(ctx context.Context, keys ...string) error {
	if h.closed.Load() {
		return cache.ErrClosed
	}
	return h.Backend.Delete(ctx, keys...)
}

func (h *handle) Close() error {
	if h.closed.Swap(true) {
		return cache.ErrClosed
	}
	return nil
}
