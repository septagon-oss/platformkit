// Package cache is one value every replica can read.
//
// A map in one process's memory is three answers when three pods are running and
// none after a deploy — the same argument kit/limit makes about a counter, run
// against a value instead of a count. It is also three answers to the question
// "who may I treat this host as?": a suspension that invalidates one process
// leaves the other two believing what the installation just stopped believing,
// for the whole life of the entry. This package holds the port, the key type that
// names whose value an entry is, and the in-process adapter that is a deployment
// for one process and the double every test runs on.
//
// Two things make this more than a shared map. The first is the key: an entry is
// addressed by the tenant that owns it, and no caller can form a key without
// saying which of the two constructors it went through — Of, for a value that
// belongs to a customer, or Shared, for one that belongs to the installation.
// The second is Move: an invalidation that closes what was *already written*
// rather than only what is there when it runs. Delete alone loses a race — the
// replica whose load began before the suspension writes its stale answer back
// after it, for the full TTL — so every entry carries the generation it was
// written under, and a move closes a generation. See the comment on Move.
//
// The value cache is for values a replica may recompute. Sessions, permission
// grants and entitlements are never cached here, in this process or in any
// store: modules/auth's own kernel says why ("A permission cache is a window in
// which a revoked grant still works"), and the read path for those is the
// transaction under row-level security. docs/cache.md holds the table.
package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// budget bounds every call this package makes of its store. A cache sits in
// front of a database that is the truth, so neither a caller who hung up nor a
// store that stopped answering may turn a lookup into a request that never ends:
// past the budget the caller gets an error and fails open to the query. The
// context is detached first (WithoutCancel), for the opposite reason — an
// invalidation that follows a committed write must not be unwound by the client
// closing the connection. kit/limit/run makes the same two moves for the same
// two reasons.
const budget = 250 * time.Millisecond

// marker separates the generation from the value inside a stored entry.
const marker = '\n'

var (
	// ErrNoScope is what every method answers for a Key or Scope that was never
	// built by Of or Shared — a struct conversion, a zero value handed along, a
	// namespace nobody named. A scope with no namespace is a key no invalidation
	// can reach, so writing it would be storing a value nobody can forget. It is
	// immutable: a caller bug, not a condition to retry.
	ErrNoScope = errors.New("cache: this scope names no namespace")

	// ErrNoLifetime is what Set answers when the caller asks for no expiry. The
	// store's own default for a write with no TTL is "forever" — measured: the
	// TTL of such a key is -1s — so an entry nobody can expire is a stale answer
	// with no owner. It is immutable, and it writes nothing.
	ErrNoLifetime = errors.New("cache: this entry has no lifetime: an entry that never expires is a stale answer with no owner")

	// ErrClosed is what an adapter answers once Close has run. The Runtime owns
	// the process's resources and releases them once; a lookup that arrived
	// after the release is a request nobody routed.
	ErrClosed = errors.New("cache: this store is closed")
)

// Scope is one owner's namespace inside this app: the half of a key that an
// invalidation works on. Its fields are unexported because the only fact that
// matters about a scope is who built it, and a Scope assembled by struct literal
// answers to no one.
type Scope struct {
	tenant uuid.UUID
	ns     string
}

// Of is the scope of values that belong to a tenant.
//
// The tenant is a value and not a string, and it arrives from
// tenancy.FromContext or from a db.Tx[db.Tenant] — the two places this kernel
// knows a tenant because something checked. Of refuses the nil UUID: a caller
// that resolved nothing has no scope to write in, and Shared exists to name the
// one scope that belongs to no customer. A caller holding a context it never
// asked is refused here rather than writing a value that outlives the tenant it
// should have belonged to.
func Of(tenant uuid.UUID, ns string) Scope {
	if tenant == uuid.Nil || badNamespace(ns) {
		return Scope{}
	}
	return Scope{tenant: tenant, ns: ns}
}

// Shared is the scope of values that belong to the installation and to no
// tenant — the host-resolution index, which decides a tenant rather than
// belonging to one.
//
// It is called Shared rather than being Of(nil) so that every site holding a
// customer's value is a site that names a tenant, and a grep for cache.Shared is
// a list of the entries that do not. The tenant segment is still in the key, as
// the nil UUID: one bucket for the whole installation rather than a bucket shared
// with whichever customer happened to resolve, which is kit/limit.scoped's rule.
func Shared(ns string) Scope {
	if badNamespace(ns) {
		return Scope{}
	}
	return Scope{ns: ns}
}

// Namespace is this scope's own name, or "" for a scope that names none.
func (s Scope) Namespace() string { return s.ns }

// Entry is this Scope's own name for one value. A name is opaque to this package
// — it may hold a slash, because a host, an address or an id is the caller's to
// spell — and it cannot reach outside the Scope it was made in: the tenant
// segment is fixed-width and comes first, so nothing a caller appends shifts an
// earlier segment (kit/limit/limit.go:scoped makes the same argument, and
// TestAnotherTenantsEntryIsUnreachable is this rule's test).
func (s Scope) Entry(name string) Key {
	if name == "" {
		return Key{}
	}
	return Key{scope: s, name: name}
}

// Key is one entry's address: <app>:<tenant>:<namespace>/<name>, formed in one
// place by CacheKey. Its fields are unexported for the reason Scope's are: the
// construction is the claim.
type Key struct {
	scope Scope
	name  string
}

// Scope is this key's Scope — what an invalidation moves.
func (k Key) Scope() Scope { return k.scope }

// Entry is the name part of this key, which is what a Group keys its own map by.
func (k Key) Entry() string { return k.name }

// String is the key as its owner would say it: the namespace, the name, and no
// secret and no tenant id, because this string reaches a log line.
func (k Key) String() string {
	if k.scope.ns == "" {
		return "(no namespace)"
	}
	return k.scope.ns + "/" + k.name
}

// valid is the one check every method runs before it touches the store.
func (k Key) valid() error {
	if k.scope.ns == "" || k.name == "" {
		return ErrNoScope
	}
	return nil
}

// Cache is the port. Every method takes the caller's context and no transaction:
// a cache is consulted before a request has a tenant, and before it has a
// transaction to open — kit/httpx resolves a host precisely to decide whether one
// may be opened.
type Cache interface {
	// Get answers the value under key. found=false is a miss and never an error,
	// and a miss costs the caller its own lookup, not a refusal.
	Get(ctx context.Context, key Key) (val []byte, found bool, err error)

	// Set stores val under key for ttl. ttl <= 0 is refused with ErrNoLifetime
	// and writes nothing.
	Set(ctx context.Context, key Key, val []byte, ttl time.Duration) error

	// Delete drops exactly these keys, in as few calls to the store as the store
	// allows. Deleting a key nobody holds is not an error: a route that runs
	// twice must not fail the second time.
	Delete(ctx context.Context, keys ...Key) error

	// Move closes a whole Scope: every entry written under it reads as a miss
	// from now on, whoever wrote it and whenever it was written.
	//
	// A move and not a delete is the invalidation that is correct under a racing
	// load. Two replicas both miss for one host; both call the loader; replica A
	// finishes the suspension and deletes the key; replica B, whose load began
	// before it, writes the tenant it loaded — resurrecting the answer the
	// installation just revoked for the whole TTL. A delete can only forget what
	// is there when it runs; a move forbids what arrives afterwards.
	//
	// Its cost is its coarseness: a move closes every entry in the Scope, so one
	// suspension costs every other host one loader query on its next request.
	// For host resolution, where the operator changes a handful of hosts a day
	// and the query is indexed, that is the right trade; a finer generation, one
	// per entry, is the same mechanism at one key per entry and is deferred until
	// a deployment measures the difference.
	Move(ctx context.Context, scope Scope) error

	// Close releases what the adapter opened. Nothing else may: the Runtime owns
	// the process's resources and Close is their one release. It is safe to call
	// more than once; every call after the first answers ErrClosed.
	Close() error
}

// Value is one raw answer from a Backend. Found distinguishes "absent" from
// "present and empty", which a byte slice cannot.
type Value struct {
	Val   []byte
	Found bool
}

// Backend is what a store must answer for this port: four commands and a
// release.
//
// Everything the invalidation depends on belongs to this package and not to an
// adapter — the generation an entry is written under, the envelope that carries
// it, the budget every call gets, the batching of a delete, and the refusal of an
// entry with no lifetime. An adapter that cannot get any of that wrong is the
// point: the in-process adapter and a server-backed one then run the same
// conformance suite for the same reason, and a third one is four commands rather
// than a re-derivation of the invalidation.
type Backend interface {
	// GetMany answers the bytes under these keys, in order, with Found false for
	// one that is not there. A miss is never an error.
	GetMany(ctx context.Context, keys ...string) ([]Value, error)

	// Set stores raw bytes under key for the given lifetime. The caller has
	// already refused a lifetime of zero or less.
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error

	// Delete drops exactly these keys. A key nobody holds is not an error.
	Delete(ctx context.Context, keys ...string) error

	// Raise closes a namespace: it makes the named counter one greater and answers
	// the new value. It is the only command that mutates a counter, and it is what
	// a Move is.
	Raise(ctx context.Context, key string) (int64, error)

	// Close releases what this backend opened.
	Close() error
}

// New returns the Cache over a store. app is this application's name, the first
// segment of every key; it is validated to the slug grammar here, at the one
// moment a composition could get it wrong, rather than on every call.
func New(app string, backend Backend) (Cache, error) {
	slug, err := Slug(app)
	if err != nil {
		return nil, err
	}
	return &store{app: slug, backend: backend}, nil
}

// store is the whole implementation of the port. Adapters hold none of it.
type store struct {
	app     string
	backend Backend
}

func (s *store) Get(ctx context.Context, k Key) ([]byte, bool, error) {
	if err := k.valid(); err != nil {
		return nil, false, err
	}
	values, err := s.call(ctx, func(c context.Context) ([]Value, error) {
		// The generation and the entry arrive together: the entry carries the
		// generation it was written under, so nothing has to wait for the
		// counter before asking for it, and one round trip answers both halves
		// of "is this still believed?".
		return s.backend.GetMany(c, s.generationKey(k.scope), s.entryKey(k))
	})
	if err != nil {
		return nil, false, fmt.Errorf("cache: get %s: %w", k, err)
	}
	if len(values) != 2 || !values[1].Found {
		return nil, false, nil
	}
	gen, val, ok := decodeEnvelope(values[1].Val)
	if !ok {
		// A value this package did not write is not a value it can believe.
		// Reading it as one would let anything sharing the store's keyspace put a
		// tenant on a request.
		return nil, false, nil
	}
	open, ok := openGeneration(values[0])
	if !ok {
		return nil, false, nil
	}
	return val, gen == open, nil
}

func (s *store) Set(ctx context.Context, k Key, val []byte, ttl time.Duration) error {
	if err := k.valid(); err != nil {
		return err
	}
	if ttl <= 0 {
		return fmt.Errorf("cache: refusing to store %s: %w", k, ErrNoLifetime)
	}
	_, err := s.call(ctx, func(c context.Context) ([]Value, error) {
		// The write is stamped with the generation it read, not a later one, so a
		// move that lands between this read and this write still closes it. Two
		// round trips, and the only reason this port is not a pure proxy.
		values, err := s.backend.GetMany(c, s.generationKey(k.scope))
		if err != nil {
			return nil, err
		}
		if len(values) != 1 {
			return nil, errors.New("the store answered the generation read with something else")
		}
		open, ok := openGeneration(values[0])
		if !ok {
			// Writing under a generation nobody can name would be writing a value no
			// invalidation could ever close, which is the one thing this cache is for.
			return nil, errors.New("the open generation of this namespace is unreadable")
		}
		return nil, s.backend.Set(c, s.entryKey(k), encodeEnvelope(open, val), ttl)
	})
	if err != nil {
		return fmt.Errorf("cache: set %s: %w", k, err)
	}
	return nil
}

func (s *store) Delete(ctx context.Context, keys ...Key) error {
	if len(keys) == 0 {
		return nil
	}
	raw := make([]string, 0, len(keys))
	for _, k := range keys {
		if err := k.valid(); err != nil {
			return err
		}
		raw = append(raw, s.entryKey(k))
	}
	_, err := s.call(ctx, func(c context.Context) ([]Value, error) {
		return nil, s.backend.Delete(c, raw...)
	})
	if err != nil {
		return fmt.Errorf("cache: delete %d entries: %w", len(raw), err)
	}
	return nil
}

func (s *store) Move(ctx context.Context, scope Scope) error {
	if scope.ns == "" {
		return ErrNoScope
	}
	_, err := s.call(ctx, func(c context.Context) ([]Value, error) {
		_, err := s.backend.Raise(c, s.generationKey(scope))
		return nil, err
	})
	if err != nil {
		return fmt.Errorf("cache: move %s: %w", scope.ns, err)
	}
	return nil
}

func (s *store) Close() error { return s.backend.Close() }

// call runs one round of commands inside the budget, on a context detached from
// the caller's cancellation but not from its values: the trace id the kernel put
// on the request rides with the command, so a store's own failure log joins to
// the request that caused it.
func (s *store) call(ctx context.Context, fn func(context.Context) ([]Value, error)) ([]Value, error) {
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), budget)
	defer cancel()
	return fn(c)
}

// entryKey is the full address of one entry: the app, the tenant, then the
// caller's own name under its namespace.
func (s *store) entryKey(k Key) string {
	return CacheKey(s.app, k.scope.tenant, k.scope.ns+"/"+k.name)
}

// generationKey is the counter that closes a Scope. Every entry key contains a
// slash — a namespace is followed by one and a namespace may not hold one — and
// this key never does, so no name a caller invents can reach the counter its own
// invalidation reads.
//
// The counter outlives every entry under it and never expires: a generation that
// quietly reset to zero would reopen the window a move exists to close. The keys
// it costs are bounded by the namespaces of this installation, which an attacker
// choosing request data cannot widen.
func (s *store) generationKey(scope Scope) string {
	return CacheKey(s.app, scope.tenant, scope.ns+"#")
}

// encodeEnvelope is the generation in decimal, a newline, then the value. Raw
// bytes after the separator, because a value that has to survive base64 is a
// value that costs a third more space and an allocation on every read.
func encodeEnvelope(gen int64, val []byte) []byte {
	head := append(strconv.AppendInt(nil, gen, 10), byte(marker))
	return append(head, val...)
}

// decodeEnvelope is the other half, and ok=false for anything this package did
// not write.
func decodeEnvelope(buf []byte) (gen int64, val []byte, ok bool) {
	at := bytes.IndexByte(buf, marker)
	if at <= 0 {
		return 0, nil, false
	}
	n, err := strconv.ParseInt(string(buf[:at]), 10, 64)
	if err != nil || n < 0 {
		return 0, nil, false
	}
	return n, buf[at+1:], true
}

// openGeneration is the generation entries are written under right now, from what
// the store answered. A counter that is absent is zero — nothing has ever moved
// this namespace — and a counter that answers something unparsable is no
// generation at all, which is a reason to refuse the entry and not a reason to
// guess open: a cache that could not be invalidated is worse than one that is cold.
func openGeneration(v Value) (int64, bool) {
	if !v.Found {
		return 0, true
	}
	n, err := strconv.ParseInt(string(v.Val), 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// badNamespace is the shape test of a namespace: non-empty, and no slash, which
// is what keeps a Scope's address separable from its entries' names. A namespace
// is a constant written by a package or a composition, never caller data, so a
// bad one is a bug worth refusing loudly and once.
func badNamespace(ns string) bool {
	return ns == "" || strings.ContainsRune(ns, '/')
}
