package idempotency

// The in-memory store: the same records in one process's memory, for a test and
// for a fake. It is not an alternative deployment — a memory that resets on
// restart and multiplies by the replica count is the thing this package exists to
// replace, which is why nothing in apps/ constructs one — and its reason to exist
// is kit/limit's: an interface is justified by a passing fake, and the conformance
// suite that passes here is what entitles kit/httpx's cases to run on it.

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// maxTracked bounds the map, for maxTracked's reason in kit/limit/memory.go: a
// key anybody can invent is a record anybody can mint, so past the bound the
// records that can no longer be replayed are dropped, and if that was not enough
// the map is emptied. What that costs an attacker is the memory of writes they
// never made.
const maxTracked = 10_000

// Memory returns a store that remembers in this process's memory. now is the
// clock a record is measured against on the calls that name no moment of their
// own — see clock. It is nil for the store every case here uses, which reads the
// wall clock like the shared one does.
func Memory(now func() time.Time) Store {
	if now == nil {
		now = time.Now
	}
	return &memory{now: now, records: map[string]record{}}
}

type memory struct {
	mu      sync.Mutex
	now     func() time.Time
	records map[string]record
}

// record is the stored row plus the clock reading that put it there, which is
// what a map has to keep explicitly and a row keeps in a column.
type record struct {
	at  time.Time
	row row
}

// key is the identity as the map holds it. The two identifiers come first, in
// their fixed-length form, for the reason kit/limit's scoped gives: anything a
// caller types comes after the fields nobody else can forge, so no key can be
// written to read as another tenant's or another person's record.
func (s Scope) key() string {
	return s.Tenant.String() + " " + s.Caller.String() + " " + s.Method + " " + s.Route + " " + s.Key
}

func (m *memory) Claim(_ context.Context, s Scope, at time.Time) (Verdict, error) {
	at = m.clock(at)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(at)
	k := s.key()
	claim := uuid.New()
	cur, found := m.records[k]
	var existing *row
	if found {
		r := cur.row
		existing = &r
	}
	v := decide(existing, found, s, at, claim, lease, retention)
	if v.Kind == First {
		m.records[k] = record{at: at, row: row{
			fingerprint: s.Fingerprint,
			claim:       claim,
			claimedAt:   at,
		}}
	}
	return v, nil
}

func (m *memory) Complete(_ context.Context, s Scope, claim uuid.UUID, a Answer, at time.Time) error {
	at = m.clock(at)
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, found := m.records[s.key()]
	if !found || cur.row.claim != claim || cur.row.completed {
		return ErrLostClaim
	}
	cur.at = at
	cur.row.completed = true
	cur.row.recorded = a.Status != 0
	cur.row.answer = a
	m.records[s.key()] = cur
	return nil
}

// clock is the caller's reading of now, or this store's own when the call named
// none. The zero time is never kept as a moment: it is a claim older than the
// retention by the whole of the calendar, so a record written with one would be
// answered as First by the next attempt and the key's whole promise would last as
// long as one call. Postgres needs no such guard — a timestamptz column cannot
// hold the zero time — which is the difference between a map that stores a time
// and a row that is required to.
func (m *memory) clock(at time.Time) time.Time {
	if at.IsZero() {
		return m.now()
	}
	return at
}

// prune drops what can no longer be replayed, and empties the map if that was not
// enough. The caller holds the lock. Unlike Postgres.Purge this runs on the write
// path, and it may: a map this small is bounded by the bound itself, and a fake
// that grew without one would be a fixture that hides a memory leak rather than a
// stand-in for the shared store.
func (m *memory) prune(at time.Time) {
	if len(m.records) < maxTracked {
		return
	}
	for k, r := range m.records {
		if at.Sub(r.at) >= retention {
			delete(m.records, k)
		}
	}
	if len(m.records) >= maxTracked {
		clear(m.records)
	}
}
