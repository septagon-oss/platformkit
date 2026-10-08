// Package idempotency remembers a write the caller said was one write, so the
// retry of it is not a second one.
//
// A phone on a bad network retries the POST it already sent. Without a memory of
// the first attempt the server does the work twice and the caller is told the
// invoice was raised twice. The IETF's answer (draft-ietf-httpapi-idempotency-key-header)
// is a header the caller chooses and the server keys everything else on; this
// package is the memory behind that header and nothing else — the middleware that
// reads the header, refuses the requests that make no promise, and answers the
// retries is kit/httpx.
//
// The memory has to outlive the process that made it. A retried write arrives at
// whichever replica the load balancer chose, minutes later, after a deploy that
// emptied every map in the fleet. That is why the record is a row: the same
// argument, the same shape and the same purge schedule as kit/limit's counters
// (docs/adr/0010-a-limit-is-a-row.md), which faced the identical problem one
// package over — a counter in one pod's memory is three limits when three pods
// are running, and a memory in one pod's map is a promise three pods cannot keep.
package idempotency

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
)

// Scope is what one write is remembered under. Every field is part of the
// identity, and none of it is derived when the record is read back: the route is
// the mounted pattern the request matched rather than the path it spelled, the
// tenant and the caller are the kernel's own identifiers rather than anything the
// caller asserted, and the key is the caller's string, verbatim.
//
// Two tenants, two people, two routes or two verbs therefore never share a
// record — which is the property kit/limit buys by putting a fixed-length tenant
// id at the front of every key, taken here with actual columns instead of a
// prefix, because a key anybody types in front of a separator is a key that can
// be made to read as somebody else's.
type Scope struct {
	Tenant uuid.UUID // tenancy.FromContext; uuid.Nil for a host that resolved no tenant
	Caller uuid.UUID // tenancy.PrincipalFrom's UserID; uuid.Nil for an anonymous caller
	Method string    // "POST" or "PATCH", exactly as the request spelled it
	Route  string    // the mounted pattern, never the request's own path
	Key    string    // the caller's Idempotency-Key, syntax-gated before it gets here
	// Fingerprint is what the request was: a digest over the method, the route,
	// the query, the content type and the body. Two attempts are the same write
	// when it matches, and a different write when it does not.
	Fingerprint [sha256.Size]byte
}

// Answer is what the caller was finally given, held so the next attempt of the
// same write can be handed the same thing rather than a second copy of the work.
type Answer struct {
	Status      int
	ContentType string
	Body        []byte
}

// VerdictKind is what one attempt at a key is met with.
type VerdictKind int

const (
	// First is the attempt that owns the key. It runs the handler.
	First VerdictKind = iota
	// Replay is the same write again: answer from the record and run nothing.
	Replay
	// ConflictBody is the same key carrying a different write. The draft answers
	// it 422, because the caller's mistake is in the request and only the
	// caller can correct it.
	ConflictBody
	// InFlight is the same write still being made. The draft answers it 409
	// because there is nothing to correct — the first answer exists or does not,
	// and a retry that arrives before it does is early rather than wrong.
	InFlight
	// Spent is a key whose write completed and whose answer was never recorded:
	// the answer was too large to hold, or it was already on the wire when the
	// process ended. It is answered 409 and never re-run: losing an answer may
	// cost a caller a retry, it may not cost them a second write.
	Spent
)

// Verdict is the whole disposition of one attempt, decided by decide and by
// nothing else — the Postgres store and the in-memory fake answer from the same
// function, which is what entitles kit/httpx's cases to run on the fake.
type Verdict struct {
	Kind   VerdictKind
	Claim  uuid.UUID     // First: the token this attempt holds the record with
	Stored Answer        // Replay: what the first attempt was answered with
	Lease  time.Duration // InFlight: how much longer the claim may be held
}

// Store is the memory: two writes, one to take a key and one to land an answer
// on it.
type Store interface {
	// Claim takes a key nobody holds, or reads what whoever holds it has.
	Claim(ctx context.Context, s Scope, at time.Time) (Verdict, error)

	// Complete lands an answer, and only for the attempt that still owns the
	// claim. The zero Status is the spent record: the write happened and its
	// answer could not be kept.
	Complete(ctx context.Context, s Scope, claim uuid.UUID, a Answer, at time.Time) error
}

// table is the one this package owns. See migrations/000044_idempotency.
const table = "platformkit_idempotency"

const (
	// budget bounds every statement here, for kit/limit's reason verbatim
	// (kit/limit/limit.go's budget): this is on the path of a request that is
	// about to be refused, so a database that has stopped answering must cost the
	// caller this much and an error, never a request that never ends.
	//
	// The number is five seconds because the two it replaced was wrong in a way
	// that reads as a store outage. `make check` of 2026-10-08 ran 90 packages that
	// all create a database at once and measured statements at 1.7s and 2.5s; a
	// Claim then came back `idempotency: claim: timeout: context deadline exceeded`
	// on a cluster that was slow and perfectly healthy, and an unreadable record is
	// a write this package must refuse (503) rather than risk duplicating. That
	// bound was refusing real writes because other packages were migrating. The
	// stall it exists to catch is still bounded; see kit/limit/limit.go's budget, which
	// carries the same measurement for the same reason.
	budget = 5 * time.Second

	// lease is how long a claim may be held without an answer. It is the window
	// in which a retried write is refused 409 rather than allowed to run, so it
	// has to be longer than the longest request this kernel can be inside — its
	// own read and write timeouts bound that — and short enough that a replica
	// killed mid-write costs its caller a few minutes on that one key rather than
	// the whole retention. No caller can set it.
	lease = 5 * time.Minute

	// retention is how long a record lives, from its claim (the brief's day). The
	// purge is the only thing that deletes, so the table is bounded by one day of
	// keyed writes and nothing on the hot path pays for that. The hot path does
	// enforce it for the one key it is standing on: a claim that finds a record past
	// the retention takes it (postgres.Claim's takeover, and memory.Claim through the
	// same decide) rather than handing its caller the answer of a write it has
	// stopped remembering — which is what makes the day a promise rather than the
	// schedule of whatever job happens to run it.
	retention = 24 * time.Hour
)

// systemToken is the capability these rows need. They belong to no tenant — the
// tenant is one field of the key, and uuid.Nil is a value it can take — so they
// are written cross-tenant and detached, exactly as kit/limit's counters are for
// the reason that file states: a claim that only became visible at the commit of
// the request that made it is a claim two simultaneous retries both win.
var systemToken = syscap.NewSystemToken("idempotency records")

// ErrNoConnection is what every method answers when Connections finds none. It
// is an error rather than a silent allowance because what it protects is a
// promise: kit/httpx refuses a keyed request it cannot record rather than running
// it, which is the opposite of how it fails when the rate limit cannot be read —
// a limit that cannot be counted lets traffic through, and a memory that cannot
// be written duplicates a write.
var ErrNoConnection = errors.New("idempotency: no database connection on this context")

// ErrLostClaim reports that Complete was asked for by an attempt that no longer
// owns the key — its lease expired and another attempt took the record, or the
// answer is already there. Nothing is written. It is an error rather than a
// silent nil so the caller logs the one case where a write happened whose answer
// nobody will ever be shown.
var ErrLostClaim = errors.New("idempotency: this attempt no longer holds the claim")

// Connections is where a store finds the pool: httpx.ConnFrom is what the
// application passes, and the reason it is a parameter and not an import is the
// one kit/limit gives for the same shape (kit/limit/limit.go's Connections).
type Connections func(context.Context) (*db.Conn, bool)

// Postgres returns the store every replica shares.
func Postgres(conns Connections) Store { return postgres{conns: conns} }

type postgres struct{ conns Connections }

func (p postgres) Claim(ctx context.Context, s Scope, at time.Time) (Verdict, error) {
	// Take the key if nobody holds it. One statement, and ON CONFLICT DO NOTHING
	// is the whole of the race: two retries of one dropped response arrive at two
	// replicas at once, and exactly one of them gets a row back.
	claim := uuid.New()
	var own uuid.UUID
	taken := false
	err := p.run(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		rows, err := tx.DB().Raw(`INSERT INTO `+table+` (tenant, caller, method, route, key, fingerprint, claim, claimed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (tenant, caller, method, route, `+keyColumn+`) DO NOTHING
			RETURNING claim`,
			s.Tenant, s.Caller, s.Method, s.Route, s.Key, s.Fingerprint[:], claim, at).Rows()
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			return rows.Scan(&own)
		}
		taken = true
		return rows.Err()
	})
	if err != nil {
		return Verdict{}, fmt.Errorf("idempotency: claim: %w", err)
	}
	if !taken {
		return Verdict{Kind: First, Claim: own}, nil
	}

	existing, found, err := p.read(ctx, s)
	if err != nil {
		return Verdict{}, err
	}
	v := decide(existing, found, s, at, claim, lease, retention)
	if v.Kind != First || !found {
		return v, nil
	}
	// decide has just said First about a row that exists, which leaves exactly two
	// reasons it can say that: the claim is older than its lease, so whoever held it
	// has stopped being able to answer for it, or the record is older than the
	// retention, so the memory it holds is one this package no longer owes anybody.
	// Taking it is one conditional statement rather than a read and a write, so two
	// simultaneous attempts cannot both take it; the loser gets InFlight and the row
	// it did not write.
	//
	// The two conditions are the two comparisons decide made, restated in SQL rather
	// than read back into Go and acted on — and both are needed. A takeover that
	// could touch only an unfinished row would leave a caller whose key aged out
	// told that a write which finished a day ago is still in flight, until the
	// hourly purge happened to run; `retention` is a day and the purge is what the
	// brief calls a schedule, not a guarantee.
	//
	// The SET list clears the answer along with the claim, because the row that comes
	// back out of this statement is a new write's record: a stale fingerprint behind a
	// new claim would make the next attempt of the caller's own write a conflict, and
	// a stale answer behind a new claim would be replayed as this one's.
	taken = false
	err = p.run(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		res := tx.DB().Exec(`UPDATE `+table+` SET claim = ?, claimed_at = ?, fingerprint = ?,
				completed_at = NULL, recorded = FALSE, status = NULL, content_type = NULL, body = NULL
			WHERE tenant = ? AND caller = ? AND method = ? AND route = ? AND `+keyColumn+` = ?
			  AND (claimed_at <= ? OR (completed_at IS NULL AND claimed_at <= ?))`,
			claim, at, s.Fingerprint[:],
			s.Tenant, s.Caller, s.Method, s.Route, s.Key,
			at.Add(-retention), at.Add(-lease))
		if res.Error != nil {
			return res.Error
		}
		taken = res.RowsAffected > 0
		return nil
	})
	if err != nil {
		return Verdict{}, fmt.Errorf("idempotency: take over: %w", err)
	}
	if taken {
		return Verdict{Kind: First, Claim: claim}, nil
	}
	return Verdict{Kind: InFlight, Lease: remaining(existing, at, lease)}, nil
}

func (p postgres) Complete(ctx context.Context, s Scope, claim uuid.UUID, a Answer, at time.Time) error {
	recorded := a.Status != 0
	err := p.run(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		res := tx.DB().Exec(`UPDATE `+table+`
			SET completed_at = ?, recorded = ?, status = ?, content_type = ?, body = ?
			WHERE tenant = ? AND caller = ? AND method = ? AND route = ? AND `+keyColumn+` = ?
			  AND claim = ? AND completed_at IS NULL`,
			at, recorded, a.Status, a.ContentType, answerBody(a),
			s.Tenant, s.Caller, s.Method, s.Route, s.Key, claim)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrLostClaim
		}
		return nil
	})
	if err != nil && !errors.Is(err, ErrLostClaim) {
		return fmt.Errorf("idempotency: complete: %w", err)
	}
	return err
}

func (p postgres) read(ctx context.Context, s Scope) (*row, bool, error) {
	var (
		r    row
		got  bool
		body []byte
	)
	err := p.run(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		rows, err := tx.DB().Raw(`SELECT fingerprint, claim, claimed_at, completed_at IS NOT NULL, recorded,
			COALESCE(status, 0), COALESCE(content_type, ''), body
			FROM `+table+` WHERE tenant = ? AND caller = ? AND method = ? AND route = ? AND `+keyColumn+` = ?`,
			s.Tenant, s.Caller, s.Method, s.Route, s.Key).Rows()
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return rows.Err()
		}
		got = true
		var fp []byte
		if err := rows.Scan(&fp, &r.claim, &r.claimedAt, &r.completed, &r.recorded, &r.answer.Status, &r.answer.ContentType, &body); err != nil {
			return err
		}
		copy(r.fingerprint[:], fp)
		r.answer.Body = body
		return rows.Err()
	})
	if err != nil {
		return nil, false, fmt.Errorf("idempotency: read: %w", err)
	}
	if !got {
		return nil, false, nil
	}
	return &r, true, nil
}

// run opens the record's own transaction: detached, so the claim outlives the
// rollback of the request that made it, and bounded, so neither of those may turn
// a stalled database into a request that never ends. It is deliberately not the
// request's transaction — an idempotency record is protocol state, not domain
// state, and house rule 9's "state and its outbox event commit together" is about
// a command's rows. The same reading, the same reasons, as kit/limit's run.
func (p postgres) run(ctx context.Context, fn func(context.Context, db.Tx[db.System]) error) error {
	conn, ok := p.conns(ctx)
	if !ok {
		return ErrNoConnection
	}
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), budget)
	defer cancel()
	return db.RunSystem(detached, conn, systemToken, fn)
}

// Purge deletes the records whose claim is a day old.
//
// It is a scheduled job and never a side effect of Claim, for the reason kit/limit
// gives for its own (kit/limit/limit.go's Purge): the hot path is the path of a
// request that is about to be refused, and a refusal that also deleted somebody
// else's row is a memory paying for the traffic it is refusing. It is also the
// only thing that bounds the table, which holds a caller's own request and answer
// for the day it is kept.
func Purge(ctx context.Context, conn *db.Conn) error {
	return db.RunSystem(ctx, conn, systemToken, func(_ context.Context, tx db.Tx[db.System]) error {
		stmt := "DELETE FROM " + table + " WHERE claimed_at < now() - ?::interval"
		if err := tx.DB().Exec(stmt, interval(retention)).Error; err != nil {
			return fmt.Errorf("idempotency: purge: %w", err)
		}
		return nil
	})
}

// Retention is how long a record lives, for the caller that has to say it in a
// refusal sentence and for the document that declares the header. It is a
// constant rather than a setting: a knob nobody has a reason to turn is a knob
// nobody tests (kit/httpx/surfaces.go).
const Retention = retention

// Lease is how long a claim may be held without an answer, for the same two
// readers.
const Lease = lease

// row is one record as it is stored, and the shape decide reads. The in-memory
// fake holds the same struct, which is why the two agree.
type row struct {
	fingerprint [sha256.Size]byte
	claim       uuid.UUID
	claimedAt   time.Time
	completed   bool
	recorded    bool
	answer      Answer
}

// decide is the whole disposition, and the only place it is decided.
//
// `found` is separate from a nil record rather than folded into it because
// "nobody holds this key" and "the record aged out a moment ago" are the same
// answer for a caller and two different facts for whoever reads this function
// next; the purge is the only thing that deletes, and a store that has not run
// one yet still answers a two-day-old claim as First.
func decide(existing *row, found bool, want Scope, at time.Time, claim uuid.UUID, lease, retention time.Duration) Verdict {
	if !found || existing == nil || at.Sub(existing.claimedAt) >= retention {
		return Verdict{Kind: First, Claim: claim}
	}
	if existing.fingerprint != want.Fingerprint {
		// Said before any look at the state: a different write under this key is
		// the caller's mistake whether or not the first one finished, and the
		// answer they need — send it under a fresh key — does not change.
		return Verdict{Kind: ConflictBody}
	}
	if existing.completed {
		if !existing.recorded {
			return Verdict{Kind: Spent}
		}
		return Verdict{Kind: Replay, Stored: existing.answer}
	}
	if held := at.Sub(existing.claimedAt); held < lease {
		return Verdict{Kind: InFlight, Lease: lease - held}
	}
	return Verdict{Kind: First, Claim: claim}
}

// remaining is how much longer the current holder's claim runs, for the attempt
// that lost the race to inherit it.
func remaining(existing *row, at time.Time, lease time.Duration) time.Duration {
	if existing == nil {
		return lease
	}
	return max(lease-at.Sub(existing.claimedAt), 0)
}

// keyColumn is the column holding the caller's key, named once because it is a
// reserved word in the middle of five statements.
const keyColumn = `"key"`

// interval is a span as Postgres reads one. Milliseconds, so a test may use a
// lease or a retention shorter than a second, and the same helper kit/limit uses
// for the same reason.
func interval(span time.Duration) string {
	return fmt.Sprintf("%d milliseconds", span.Milliseconds())
}

// answerBody keeps nil for a spent record, so a row that holds no answer holds
// no empty bytea either.
func answerBody(a Answer) []byte {
	if a.Status == 0 {
		return nil
	}
	return a.Body
}
