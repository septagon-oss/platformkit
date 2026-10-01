package cache

// group.go is decision 0028 §4: an instance does not build 999 routers at boot.
// The first request that needs one composes it, caches it, and a bounded number
// of idle ones are evicted.
//
// The value lives here, in this process's memory, because a composed value is a
// graph of closures — a router, a template set, a decoded configuration — that no
// serialisation reaches. What lives in the shared store is the *marker*: one
// small write saying this entry has been composed and nobody has invalidated it
// since. That is the whole of the cross-process half, and it is why an eviction
// in one process is not a fact the other processes have to hear about: they never
// held the value.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// DefaultGroupMax is how many composed values one Group keeps in this process.
//
// It is a stated guess with a name, a test and an override argument, and not a
// measurement: no memory measurement of an idle composed value exists anywhere in
// this program, and the number a deployment would need is a function of what it
// composes. Sixteen is the shape of the arrangement the decision describes — a
// handful of applications behind one address, not the whole corpus — and the cost
// of being wrong on the small side is one recomposition on the next request for
// an evicted entry, never a refusal.
const DefaultGroupMax = 16

// markerValue is what the shared store holds for a composed entry: a fact with no
// content. The store's own expiry is the entry's lifetime, so a marker nobody
// refreshed goes away by itself and the value next to it is composed again.
var markerValue = []byte{1}

// ErrScope is what a Group answers when a key arrives from a different Scope than
// the one the group composes: the composition would be the wrong one for the
// value's owner. Immutable — a caller bug, and a value composed for one tenant
// and served to another is exactly the finding this package exists to make
// impossible.
var ErrScope = errors.New("cache: this entry belongs to another scope")

// Group is a bounded set of lazily composed values, one per entry of a Scope.
//
// A Group holds no resource that needs releasing — no pool, no subscription, no
// file — because eviction has no release step: the entry is dropped and the next
// request composes another. A value that owns a connection does not belong here;
// the pools belong to kit/app's Runtime.
type Group[V any] struct {
	cache   Cache
	scope   Scope
	ttl     time.Duration
	max     int
	compose func(context.Context, Key) (V, error)
	log     *slog.Logger

	mu     sync.Mutex
	order  []string
	values map[string]composed[V]
	flight singleflight.Group
}

type composed[V any] struct {
	value   V
	expires time.Time
}

// GroupOf names one kind of composed value over one Scope.
//
// compose is called with the caller's context, at most once per concurrent miss
// for an entry, and never for a read that finds one. ttl is the entry's own
// lifetime and the marker's, so a marker nobody recomposes expires with the value
// it stands for; max is the bound on this process, and zero means
// DefaultGroupMax. Nothing is composed at construction, and a compose that fails
// is remembered nowhere.
func GroupOf[V any](c Cache, scope Scope, ttl time.Duration, max int, compose func(context.Context, Key) (V, error)) *Group[V] {
	if max <= 0 {
		max = DefaultGroupMax
	}
	if ttl <= 0 {
		ttl = DefaultGroupTTL
	}
	if compose == nil {
		panic("cache.GroupOf: compose is required; a group that composes nothing is a map")
	}
	return &Group[V]{
		cache: c, scope: scope, ttl: ttl, max: max, compose: compose,
		log: slog.Default(), values: map[string]composed[V]{},
	}
}

// DefaultGroupTTL is how long a composed entry is believed when the caller names
// no lifetime. A composed router is a configuration, and a configuration that is
// thirty seconds old is the same fact the host resolution is built on.
const DefaultGroupTTL = 30 * time.Second

// WithLog names the logger this Group writes its one warning to. Set it at
// composition, beside the Cache it reads: a group built by a composition and used
// by a request has no other moment to be told where its complaints go.
func (g *Group[V]) WithLog(log *slog.Logger) *Group[V] {
	if log != nil {
		g.log = log
	}
	return g
}

// Get returns this entry's value, composing it if nobody has.
//
// The read is two-level and that is the price: one shared read per request for a
// value that cannot be serialised but must still be invalidated across processes.
// A marker read that *errors* keeps serving the local value, because the marker's
// subject is which configuration is composed and not who is allowed in — a store
// that is down must cost a lookup, not a refusal.
func (g *Group[V]) Get(ctx context.Context, key Key) (V, error) {
	var zero V
	if err := key.valid(); err != nil {
		return zero, err
	}
	if key.scope != g.scope {
		return zero, fmt.Errorf("cache: group %s: %w (%s)", g.scope.ns, ErrScope, key)
	}
	if v, ok := g.local(ctx, key); ok {
		return v, nil
	}
	shared, err, _ := g.flight.Do(key.name, func() (any, error) {
		// The second look is inside the singleflight: fifty concurrent requests
		// for a cold entry ask the loader once, and the forty-nine that waited
		// read what the one that went first composed.
		if v, ok := g.peek(key); ok {
			return v, nil
		}
		v, err := g.compose(ctx, key)
		if err != nil {
			// A failed composition is remembered nowhere. Caching it would turn
			// one blink into a TTL of refusals for this host, and 0028 §4's rule
			// is that a client whose composition fails is a refusal for that
			// client and nothing else.
			return zero, err
		}
		g.remember(key, v)
		if err := g.cache.Set(ctx, key, markerValue, g.ttl); err != nil {
			// The value is composed and this process holds it; a marker nobody
			// can write costs the next request one lookup and costs this one
			// nothing. Refusing here would be a cache deciding the answer.
			g.log.WarnContext(ctx, "cache: composed an entry this process cannot mark shared",
				"entry", key.String(), "error", err)
		}
		return v, nil
	})
	if err != nil {
		return zero, err
	}
	v, ok := shared.(V)
	if !ok {
		return zero, fmt.Errorf("cache: group %s: composed %s and got it back as %T", g.scope.ns, key, shared)
	}
	return v, nil
}

// Forget drops one entry here and in every other process reading the same store.
// It deletes and does not move: what this invalidates is which configuration a
// process composed, which is bounded by the entry's own TTL and is not an
// authorisation question. A Scope-wide Move is available on the Cache itself for
// whoever needs the stronger thing.
func (g *Group[V]) Forget(ctx context.Context, key Key) error {
	g.drop(key)
	return g.cache.Delete(ctx, key)
}

// ForgetAll closes every entry of this group's Scope in every process, and drops
// this process's whole set.
func (g *Group[V]) ForgetAll(ctx context.Context) error {
	g.mu.Lock()
	clear(g.values)
	g.order = nil
	g.mu.Unlock()
	return g.cache.Move(ctx, g.scope)
}

// local answers a held entry and whether the store still believes it.
func (g *Group[V]) local(ctx context.Context, key Key) (V, bool) {
	var zero V
	v, ok := g.peek(key)
	if !ok {
		return zero, false
	}
	val, found, err := g.cache.Get(ctx, key)
	switch {
	case err != nil:
		g.log.WarnContext(ctx, "cache: could not ask whether a composed entry is still believed; serving it anyway",
			"entry", key.String(), "error", err)
		return v, true
	case !found || !bytes.Equal(val, markerValue):
		// Somebody forgot it — another process, or the store's own expiry.
		g.drop(key)
		return zero, false
	}
	return v, true
}

// peek is the map, and nothing else: no lock across the shared read, and no
// read of an entry that expired while nobody asked for it. Reading an entry is
// what makes it the most recent one, which is the whole of the LRU policy.
func (g *Group[V]) peek(key Key) (V, bool) {
	var zero V
	g.mu.Lock()
	defer g.mu.Unlock()
	c, ok := g.values[key.name]
	if !ok {
		return zero, false
	}
	if !c.expires.After(time.Now()) {
		g.forget(key.name)
		return zero, false
	}
	g.touch(key.name)
	return c.value, true
}

// remember puts a composed value in the map and evicts down to the bound, least
// recently used first — Get touches what it reads, so the order is the recency.
func (g *Group[V]) remember(key Key, value V) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.values[key.name] = composed[V]{value: value, expires: time.Now().Add(g.ttl)}
	g.touch(key.name)
	for len(g.order) > g.max {
		g.forget(g.order[0])
	}
}

func (g *Group[V]) drop(key Key) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.forget(key.name)
}

// forget is the eviction. Its whole cost is one recomposition on the next request
// for this entry: no refusal, no dropped tenant, nothing closed.
func (g *Group[V]) forget(name string) {
	delete(g.values, name)
	g.order = slices.DeleteFunc(g.order, func(n string) bool { return n == name })
}

// touch moves a name to the back of the recency list. The caller holds the lock.
func (g *Group[V]) touch(name string) {
	g.order = slices.DeleteFunc(g.order, func(n string) bool { return n == name })
	g.order = append(g.order, name)
}
