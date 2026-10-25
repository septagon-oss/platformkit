package cachetest

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/cache"
)

// fresh builds one handle and releases it when the case ends. Both Store and
// Failing are assignable here, because a handle onto a dead store is built the
// same way as a handle onto a live one.
func fresh(t *testing.T, store func(t *testing.T) cache.Cache) cache.Cache {
	t.Helper()
	c := store(t)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// fill stores one value the only way a caller can: it reads the entry, then
// stamps the write with the generation that read found open. Every incidental
// write in this suite goes through it, so no adapter is ever exercised through a
// write no real caller is able to make.
func fill(t *testing.T, c cache.Cache, key cache.Key, val []byte, ttl time.Duration) error {
	t.Helper()
	_, _, under, err := c.Get(context.Background(), key)
	if err != nil {
		return err
	}
	return c.Set(context.Background(), key, val, ttl, under)
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

func (s *switchable) Get(ctx context.Context, k cache.Key) ([]byte, bool, cache.Generation, error) {
	return s.current().Get(ctx, k)
}

func (s *switchable) Set(ctx context.Context, k cache.Key, val []byte, ttl time.Duration, under cache.Generation) error {
	return s.current().Set(ctx, k, val, ttl, under)
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

// scripted is a cache.Backend the suite drives, and it counts the commands it was
// asked for. The port's own bookkeeping — which generation a write carries, how
// many commands a fill costs — is invisible to a caller and to an adapter's
// server unless the server is there to count it, and the store that skips is the
// one case that cannot be.
type scripted struct {
	mu       sync.Mutex
	entries  map[string]stored
	counters map[string]int64
	reads    int
	writes   int
}

// commands is what the store was asked to do since the last look.
func (s *scripted) commands() (reads, writes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads, s.writes
}

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
	s.reads++
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
	s.mu.Unlock()
	return values, nil
}

func (s *scripted) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
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
