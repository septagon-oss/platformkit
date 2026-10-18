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

// maxTracked starts pruning expired attempts. Unresolved identities survive
// regardless of count; Memory is a conformance fake, never a deployment bound.
const maxTracked = 10_000

// Memory returns a store that remembers in this process's memory. now is the
// clock a record is measured against on the calls that name no moment of their
// own — see clock. It is nil for the store every case here uses, which reads the
// wall clock like the shared one does.
func Memory(now func() time.Time) Store {
	if now == nil {
		now = time.Now
	}
	return &memory{now: now, records: map[string]record{}, operations: map[operationKey]Receipt{}}
}

type memory struct {
	mu         sync.Mutex
	now        func() time.Time
	records    map[string]record
	operations map[operationKey]Receipt
}

// record pairs an attempt with the tenant used to consult its receipt during pruning.
type operationKey struct{ tenant, ref uuid.UUID }

type record struct {
	tenant uuid.UUID
	row    row
}

// key is the identity as the map holds it. The two identifiers come first, in
// their fixed-length form, for the reason kit/limit's scoped gives: anything a
// caller types comes after the fields nobody else can forge, so no key can be
// written to read as another tenant's or another person's record.
func (s Scope) key() string {
	return s.Tenant.String() + " " + s.Caller.String() + " " + s.Method + " " + s.Route + " " + s.Key
}

func (m *memory) Claim(_ context.Context, s Scope, at time.Time) (Verdict, error) {
	if err := validClaim(s, at); err != nil {
		return Verdict{}, err
	}
	at = m.clock(at).UTC().Truncate(time.Microsecond)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(at)
	k := s.key()
	claim := uuid.New()
	cur, found := m.records[k]
	var existing *row
	if found {
		r := cur.row
		if receipt, ok := m.operations[operationKey{s.Tenant, r.operationRef}]; ok {
			r.receipt = &receipt
		}
		existing = &r
	}
	v := decide(existing, found, s, at, claim, lease, retention)
	if v.Kind == First {
		ref, started := uuid.Nil, time.Time{}
		if s.Track {
			ref, started = uuid.New(), at
		}
		if found && !claimExpired(existing, at, retention) {
			ref, started = existing.operationRef, existing.startedAt
		}
		v.OperationRef, v.StartedAt = ref, started
		m.records[k] = record{tenant: s.Tenant, row: row{
			operationRef: ref, startedAt: started,
			fingerprint: s.Fingerprint,
			claim:       claim,
			claimedAt:   at,
		}}
	}
	return v, nil
}

func (m *memory) Begin(_ context.Context, s Scope, claim uuid.UUID, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, found := m.records[s.key()]
	// The same three conditions the shared store puts in its WHERE clause: this
	// attempt's claim, no answer landed, no mark already made. The claim moment keeps
	// its reading — Begin neither lengthens nor shortens the lease.
	if !found || cur.row.claim != claim || cur.row.completed || cur.row.began {
		return ErrLostClaim
	}
	cur.row.began = true
	m.records[s.key()] = cur
	return nil
}

func (m *memory) Complete(_ context.Context, s Scope, claim uuid.UUID, a Answer, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, found := m.records[s.key()]
	if !found || cur.row.claim != claim || cur.row.completed {
		return ErrLostClaim
	}
	cur.row.completed = true
	cur.row.recorded = a.Status != 0
	cur.row.answer = a
	m.records[s.key()] = cur
	return nil
}

// clock supplies the fake's default for legacy claims that omitted an instant.
// Tracked claims reject zero before reaching this helper.
func (m *memory) clock(at time.Time) time.Time {
	if at.IsZero() {
		return m.now()
	}
	return at
}

// prune removes expired attempts at the fake's threshold. The caller holds the
// mutex; unresolved receipts keep their key identities.
func (m *memory) prune(at time.Time) {
	if len(m.records) < maxTracked {
		return
	}
	for k, r := range m.records {
		rr := r.row
		if receipt, ok := m.operations[operationKey{r.tenant, rr.operationRef}]; ok {
			rr.receipt = &receipt
		}
		if claimExpired(&rr, at, retention) {
			delete(m.records, k)
		}
	}

}
