// Package cache is one value every replica can read.
//
// Multiple replicas need shared invalidation: a host suspension must reach every
// process that cached the host. This package holds the cache port and the
// tenant-qualified keys; the in-process adapter runs the same conformance cases
// as a shared store, so a single-process deployment needs no store of its own.
//
// Two things make this more than a shared map. An entry is addressed by the
// tenant that owns it, and no caller can form a key without saying which
// constructor it went through — Of for a customer's value, Shared for the
// installation's. And Move closes what was *already written*, which is the
// invalidation a racing load needs. Sessions, grants and entitlements are never
// cached here: their read path is the transaction under row-level security, and
// docs/cache.md holds the table of what may and may not sit here.
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
	"github.com/septagon-oss/platformkit/kit/appname"
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

	// ErrNoGeneration is what Set answers for a Generation that came from no read.
	// A write has to be stamped by the read that decided it: stamped with anything
	// else — the open generation at the moment of the write, or a number someone
	// made up — it survives a move that already happened, which is the window this
	// whole package exists to close. It is immutable, a caller bug, and it writes
	// nothing.
	ErrNoGeneration = errors.New("cache: this write names no read; a write stamped by nothing outlives the invalidation that should have closed it")
)

// Generation is the open generation of a Scope, as one read answered it, and the
// only thing that may stamp a write.
//
// It is a value with unexported fields because the fact it carries is *when*: the
// moment a lookup was made is what decides whether a later write is still
// believed, and a number assembled by hand claims a moment that never happened.
// Get answers one, Set takes one, and there is no third way to obtain it.
type Generation struct {
	gen  int64
	read bool
}

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
	// Get answers the value under key, and the generation its own read found open.
	// found=false is a miss and never an error, and a miss costs the caller its own
	// lookup, not a refusal.
	//
	// The Generation is the second half of the answer and not a detail of it: an
	// entry is believed because the read that found it ran under the generation
	// that is still open, and a caller that decides to load on a miss can only
	// promise what its write is worth by stamping it with that same read. Pass it
	// to Set.
	Get(ctx context.Context, key Key) (val []byte, found bool, under Generation, err error)

	// Set stores val under key for ttl, stamped with the generation a read of this
	// same key answered open — the read that decided the load, not a later one.
	//
	// It asks the store for nothing: the write is worth exactly what the read that
	// bought it was worth, so a move that lands between the two closes this entry
	// too, and the racing write is believed by nobody rather than refused noisily.
	// under from no read is refused with ErrNoGeneration and writes nothing; so is
	// ttl <= 0, with ErrNoLifetime.
	Set(ctx context.Context, key Key, val []byte, ttl time.Duration, under Generation) error

	// Delete drops exactly these keys, in as few calls to the store as the store
	// allows. Deleting a key nobody holds is not an error: a route that runs
	// twice must not fail the second time.
	Delete(ctx context.Context, keys ...Key) error

	// Move closes a whole Scope: every entry written under it reads as a miss
	// from now on, whoever wrote it and whenever it was written.
	//
	// A move, not a delete, is the invalidation that survives a racing load. Two
	// replicas both miss and both call the loader; A finishes the suspension and
	// deletes the key; B, whose load began before it, writes what it loaded —
	// resurrecting the answer the installation just revoked, for the whole TTL. A
	// delete can only forget what is there when it runs.
	//
	// Its cost is its coarseness: one suspension costs every other host in the
	// Scope one loader query on its next request. For host resolution, where an
	// operator changes a handful of hosts a day and the query is indexed, that is
	// the right trade; a per-entry generation is the same mechanism one key per
	// entry.
	Move(ctx context.Context, scope Scope) error

	// Close releases what the adapter opened. Nothing else may: the Runtime owns
	// the process's resources and Close is their one release. It is safe to call
	// more than once; every call after the first answers ErrClosed.
	Close() error
}

// Entry is one cache read. Found distinguishes a hit, including an empty value,
// from a miss that may still carry stale bytes. Generation stamps a subsequent
// Set for the same key, including after a miss.
type Entry struct {
	Value      []byte
	Found      bool
	Generation Generation
}

// GetEntry calls c.Get once with ctx and key, preserving its bytes, generation
// and error without copying or wrapping. Byte ownership remains c.Get's; an
// erroneous answer must not be used as a successful read. c must be non-nil.
func GetEntry(ctx context.Context, c Cache, key Key) (Entry, error) {
	value, found, generation, err := c.Get(ctx, key)
	return Entry{Value: value, Found: found, Generation: generation}, err
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

// New returns a cache over backend, rejecting empty or invalid application names
// with a Slug error. Construction does no I/O; Close releases backend.
//
// Deprecated: Use NewFor with an appname.Name. NewFor also accepts the zero name.
func New(app string, backend Backend) (Cache, error) {
	if app == "" {
		_, err := Slug(app)
		return nil, err
	}
	return NewFor(appname.Name(app), backend)
}

// NewFor returns a cache over backend, with app as its first key segment. The
// zero name keeps an empty segment; nonempty names follow Slug's legacy grammar
// and return its error if invalid. See README.md for the compatibility rule.
// Construction does no I/O. Close releases backend, including for other handles
// sharing it; a refused construction leaves backend untouched.
func NewFor(app appname.Name, backend Backend) (Cache, error) {
	name := app.String()
	if name != "" {
		if _, err := Slug(name); err != nil {
			return nil, err
		}
	}
	return &store{app: name, backend: backend}, nil
}

// store is the whole implementation of the port. Adapters hold none of it.
type store struct {
	app     string
	backend Backend
}

func (s *store) Get(ctx context.Context, k Key) ([]byte, bool, Generation, error) {
	if err := k.valid(); err != nil {
		return nil, false, Generation{}, err
	}
	values, err := s.call(ctx, func(c context.Context) ([]Value, error) {
		// The generation and the entry arrive together: the entry carries the
		// generation it was written under, so nothing has to wait for the
		// counter before asking for it, and one round trip answers both halves
		// of "is this still believed?".
		return s.backend.GetMany(c, s.generationKey(k.scope), s.entryKey(k))
	})
	if err != nil {
		return nil, false, Generation{}, fmt.Errorf("cache: get %s: %w", k, err)
	}
	if len(values) != 2 {
		return nil, false, Generation{}, nil
	}
	open, ok := openGeneration(values[0])
	if !ok {
		// A counter that answers nonsense is a namespace this package cannot
		// invalidate, so nothing under it is believed and no write may be stamped
		// under it either: the caller loads from its own truth and stores nothing.
		return nil, false, Generation{}, nil
	}
	under := Generation{gen: open, read: true}
	if !values[1].Found {
		return nil, false, under, nil
	}
	gen, val, ok := decodeEnvelope(values[1].Val)
	if !ok {
		// A value this package did not write is not a value it can believe.
		// Reading it as one would let anything sharing the store's keyspace put a
		// tenant on a request.
		return nil, false, under, nil
	}
	return val, gen == open, under, nil
}

func (s *store) Set(ctx context.Context, k Key, val []byte, ttl time.Duration, under Generation) error {
	if err := k.valid(); err != nil {
		return err
	}
	if ttl <= 0 {
		return fmt.Errorf("cache: refusing to store %s: %w", k, ErrNoLifetime)
	}
	if !under.read {
		return fmt.Errorf("cache: refusing to store %s: %w", k, ErrNoGeneration)
	}
	_, err := s.call(ctx, func(c context.Context) ([]Value, error) {
		// One command, and no read of the counter: the write is stamped with the
		// generation the caller's own read found open, which is the moment that
		// decides whether anything may still believe it. A move in between does not
		// meet an error — it lands, is read as a miss from then on, and expires on
		// the lifetime this call was made with.
		return nil, s.backend.Set(c, s.entryKey(k), encodeEnvelope(under.gen, val), ttl)
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
// quietly reset to zero would reopen the window a move exists to close. Expiry is
// not the only way it can be lost — a store evicting on memory pressure takes an idle
// counter, and openGeneration reads an absent one as generation 0, which believes the
// entries written before the first move again. So an operator's maxmemory-policy may
// not evict it: docs/cache.md, and compose.yaml's noeviction. The keys it costs are
// bounded by the namespaces of this installation, which an attacker choosing request
// data cannot widen.
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
