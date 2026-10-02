package cache

// The in-process store: the same envelope, the same generations and the same
// refusals, over a mutex and a map.
//
// It is a deployment for one process and the double every test runs on, and it is
// deliberately not a second implementation of the rules. A two-replica test shares
// one instance, which is what kit/limit's TestTwoReplicasShareOneLimit does over
// one database: the claim under test is that a value and an invalidation reach
// every reader of a store, and that claim does not need a second machine to be
// worth proving. What a memory adapter may not be is a *deployment* that thinks
// it is a shared one — kit/app says so once, in a log line, when it builds one.

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// maxTracked bounds the map. An attacker who tries a fresh invented key every
// time would otherwise fill it: past the bound the expired entries are dropped
// and, if that was not enough, the map is emptied, which costs the honest entries
// their number and costs an attacker nothing they did not have. kit/limit's
// memory.go makes the same argument about a counter and arrives at the same
// shape, because the threat is the same one.
const maxTracked = 10_000

// Memory returns the store of one process, named for the application it serves.
func Memory(app string) Cache {
	return MemoryNamed(app, MemoryBackend())
}

// MemoryNamed is Memory over a store someone already holds — the shape a test
// uses to give two replicas one store, and the shape a composition uses to put a
// counter or a decorator in front of it without copying this package.
func MemoryNamed(app string, backend Backend) Cache {
	c, err := New(app, backend)
	// The only error New answers is an application name that is not a slug,
	// which is a composition bug and is a panic here for the same reason
	// httpx.New panics on a missing option: nothing can be served until it is
	// fixed, and a value that cannot work is not worth threading an error for.
	if err != nil {
		panic(err)
	}
	return c
}

// MemoryBackend is the in-process store on its own, for whoever needs the store
// apart from the port: the conformance suite, because two handles over one store
// is the arrangement the sharing and invalidation cases are about.
func MemoryBackend() Backend {
	return &memory{entries: map[string]record{}, counters: map[string]int64{}}
}

type record struct {
	val   []byte
	until time.Time
}

type memory struct {
	mu      sync.Mutex
	entries map[string]record
	// counters outlive their entries: see generationKey.
	counters map[string]int64
	closed   bool
}

func (m *memory) GetMany(ctx context.Context, keys ...string) ([]Value, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	now := time.Now()
	values := make([]Value, 0, len(keys))
	for _, k := range keys {
		// A generation counter is readable exactly as a server's is: the port asks
		// the same question of both halves of an entry in one command and must not
		// have to know which kind of key it was handed.
		if n, ok := m.counters[k]; ok {
			values = append(values, Value{Val: []byte(strconv.FormatInt(n, 10)), Found: true})
			continue
		}
		r, ok := m.entries[k]
		if !ok || !r.until.After(now) {
			// Dropped on the way past, so an entry that stopped being believed
			// stops occupying the map instead of waiting for a restart.
			delete(m.entries, k)
			values = append(values, Value{})
			continue
		}
		values = append(values, Value{Val: r.val, Found: true})
	}
	return values, nil
}

func (m *memory) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	m.prune(time.Now())
	// The bytes are the caller's slice; a store that kept the reference would
	// let the caller change what the next replica reads.
	buf := make([]byte, len(val))
	copy(buf, val)
	m.entries[key] = record{val: buf, until: time.Now().Add(ttl)}
	return nil
}

func (m *memory) Delete(_ context.Context, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	for _, k := range keys {
		delete(m.entries, k)
	}
	return nil
}

func (m *memory) Raise(_ context.Context, key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, ErrClosed
	}
	m.counters[key]++
	return m.counters[key], nil
}

func (m *memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	m.closed = true
	clear(m.entries)
	return nil
}

// prune drops what has expired, and empties the entries if that was not enough.
// The caller holds the lock. Entries only: the generations belong to the
// installation, and emptying them would reopen every entry written under a
// closed generation.
func (m *memory) prune(now time.Time) {
	if len(m.entries) < maxTracked {
		return
	}
	for k, r := range m.entries {
		if !r.until.After(now) {
			delete(m.entries, k)
		}
	}
	if len(m.entries) >= maxTracked {
		clear(m.entries)
	}
}
