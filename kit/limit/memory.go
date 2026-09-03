package limit

// The in-memory limiter: the same counter in one process's memory, for a test
// and for a fake. It is not an alternative deployment — a limit that resets on
// restart and multiplies by the replica count is the thing this package exists
// to replace — so nothing in apps/ constructs one.

import (
	"context"
	"sync"
	"time"
)

// maxTracked bounds the map. An attacker who tries a fresh invented key every
// time would otherwise fill it: past the bound the closed windows are dropped
// and, if that was not enough, the map is emptied, which costs the honest
// counts their number and costs an attacker nothing they did not have.
const maxTracked = 10_000

// Memory returns a limiter that counts in this process's memory.
func Memory() Limiter { return &memory{windows: map[string]window{}} }

type memory struct {
	mu      sync.Mutex
	windows map[string]window
}

type window struct {
	count int
	start time.Time
}

func (m *memory) Allow(ctx context.Context, key string, limit int, w time.Duration) (bool, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	now := time.Now()
	k := scoped(ctx, key)
	cur, ok := m.windows[k]
	if !ok || !cur.start.After(now.Add(-w)) {
		cur = window{start: now}
	}
	cur.count++
	m.windows[k] = cur
	if cur.count <= limit {
		return true, 0, nil
	}
	return false, cur.start.Add(w).Sub(now), nil
}

func (m *memory) Count(ctx context.Context, key string, w time.Duration) (int, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	cur, ok := m.windows[scoped(ctx, key)]
	if !ok || !cur.start.After(now.Add(-w)) {
		return 0, 0, nil
	}
	return cur.count, cur.start.Add(w).Sub(now), nil
}

func (m *memory) Forget(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.windows, scoped(ctx, key))
	return nil
}

// prune drops what has expired, and empties the map if that was not enough. The
// caller holds the lock. A window is expired here when it is older than the
// longest one any caller uses, because this map does not know the window a key
// was counted in.
func (m *memory) prune() {
	if len(m.windows) < maxTracked {
		return
	}
	cutoff := time.Now().Add(-keep)
	for k, w := range m.windows {
		if w.start.Before(cutoff) {
			delete(m.windows, k)
		}
	}
	if len(m.windows) >= maxTracked {
		clear(m.windows)
	}
}
