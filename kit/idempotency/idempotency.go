// Package idempotency remembers request retries and records tenant operation outcomes.
// Key claims are durable protocol state; receipt outcomes share the authoritative
// tenant transaction. kit/httpx owns admission and answer replay; kit/events
// couples outcomes to the outbox. README.md describes scope and retention.
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/request"
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
	Caller uuid.UUID // The authenticated subject: UserID, or TokenID for a key-only command; uuid.Nil when anonymous.
	Method string    // The request verb, or COMMAND at an in-process root.
	Route  string    // the mounted pattern, never the request's own path
	Key    string    // the caller's Idempotency-Key, syntax-gated before it gets here
	// Fingerprint is what the request was: a digest over the method, the route,
	// the query, the content type and the body. Two attempts are the same write
	// when it matches, and a different write when it does not.
	Fingerprint [sha256.Size]byte
	// Track binds this claim to an operation receipt; old untracked rows stay unbound.
	Track bool
}

// Answer is what the caller was finally given, held so the next attempt of the
// same write can be handed the same thing rather than a second copy of the work.
//
// Headers are part of the answer rather than decoration on it: a 303 whose
// Location is gone is not the same answer, it is a status with nothing to act
// on. Which names are worth keeping is nobody's business here — this package
// holds what it is given, verbatim, and kit/httpx decides what an answer
// consists of. It is the same division that keeps the body opaque: the memory
// does not read what it remembers. Limits are held the same way, and for the same
// reason, with one difference worth naming: they are held as the numbers a counter
// reported rather than as the headers those numbers were rendered into, because two
// of them are a countdown, and a countdown is restated rather than repeated.
type Answer struct {
	Status      int
	ContentType string
	Body        []byte
	Headers     []Header
	Limits      []Limit
}

// Limit is one ceiling an answer was decided under, as the counter that decided it
// described it: a name, a quota, what was left under it when the answer was given,
// the span the quota is measured over, and the instant the window it landed in
// closes. Only the window moves by being waited out, so it is the one number a
// replay works out again; the rest of an answer says what the first attempt was
// told.
type Limit struct {
	Name      string        `json:"n"`
	Quota     int           `json:"q"`
	Remaining int           `json:"r"`
	Window    time.Duration `json:"w"`
	ResetsAt  time.Time     `json:"c"`
}

// Header is one response header kept with an answer, name and value exactly as
// it was given. Order is kept, because a repeated name is a repeated value and
// a set that lost its order is a set that replays differently than it answered.
type Header struct {
	Name  string `json:"n"`
	Value string `json:"v"`
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
	OperationRef uuid.UUID
	StartedAt    time.Time
	Kind         VerdictKind
	Claim        uuid.UUID     // First: the token this attempt holds the record with
	Stored       Answer        // Replay: what the first attempt was answered with
	Lease        time.Duration // InFlight: how much longer the claim may be held
}

// Store is the memory: two writes, one to take a key and one to land an answer
// on it.
type Store interface {
	// Claim takes a key nobody holds, or reads what whoever holds it has.
	Claim(ctx context.Context, s Scope, at time.Time) (Verdict, error)

	// Begin admits the current claim to execution. Unbegun claims can be taken over
	// after their lease; begun HTTP claims require an uncommitted effect fence.
	// ErrLostClaim refuses an obsolete or already-begun attempt without a write.
	Begin(ctx context.Context, s Scope, claim uuid.UUID, at time.Time) error

	// Complete lands an answer, and only for the attempt that still owns the
	// claim. The zero Status is the spent record: the write happened and its
	// answer could not be kept.
	Complete(ctx context.Context, s Scope, claim uuid.UUID, a Answer, at time.Time) error

	// Mark fences this attempt and marks its effect in the supplied tenant
	// transaction. The caller must roll back on any error, including ErrLostClaim.
	// The marker commits with the command and resolves a lost COMMIT acknowledgement.
	// Memory records the request to commit but cannot undo it on rollback.
	Mark(ctx context.Context, tx db.Tx[db.Tenant], s Scope, claim uuid.UUID) error
}

// table is the one this package owns. See migrations/000062_idempotency.
const table = "platformkit_idempotency"

// effectTable is the marker of a COMMIT that was never acknowledged, and the only
// thing that can tell a begun claim's write from its absence. See
// migrations/000072_command_effect.
const effectTable = "platformkit_command_effect"

const (
	// The two ends of one key share one advisory lock: the attempt about to write
	// takes it, and so does the attempt that arrives while it is held. Postgres
	// releases a transaction-level advisory lock only once its commit is visible,
	// so the attempt that gets the lock is looking at a database the first write
	// either reached or did not — which is what turns an absent mark into evidence
	// rather than a race. A plain read sees nothing and does not wait.
	//
	// The lock covers nothing that has not reached Store.Mark yet; the lease fence
	// answers that one. The key is hashtextextended over 64 bits, and a collision
	// costs one refusal, never a second run of the write.
	effectLockTakeSQL = "SELECT pg_advisory_xact_lock(hashtextextended(?, 0))"
	effectLockTrySQL  = "SELECT pg_try_advisory_xact_lock(hashtextextended(?, 0))"
)

// effectLockTarget is the identity the lock is named after: the five columns the
// record is keyed on, so one attempt's write and another attempt's reading of its
// absence are the two ends of one lock. The claim id is deliberately absent — the
// reader knows which record it is resolving, not which attempt is committing.
func effectLockTarget(s Scope) string {
	return strings.Join([]string{
		"command-effect/", s.Tenant.String(), "/", s.Caller.String(), "/", s.Method, " ", s.Route, "/", s.Key,
	}, "")
}

const (
	// budget bounds every statement here, for kit/limit's reason verbatim
	// (kit/limit/limit.go's budget): this is on the path of a request that is
	// about to be refused, so a database that has stopped answering must cost the
	// caller this much and an error, never a request that never ends.
	//
	// Five seconds, not two: a claim that reads a record this package must refuse
	// rather than duplicate can cost a slow-but-healthy database its whole budget
	// and come back as an outage, so the bound is a stall detector and not a
	// queueing deadline. See kit/limit/limit.go's budget for the same reasoning.
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
	at = at.UTC().Truncate(time.Microsecond)
	if err := validClaim(s, at); err != nil {
		return Verdict{}, err
	}
	if at.IsZero() {
		return Verdict{}, fmt.Errorf("idempotency: claim needs an instant")
	}
	claim, ref := uuid.New(), uuid.Nil
	var started any
	if s.Track {
		ref = uuid.New()
		started = at
	}
	var verdict Verdict
	err := p.run(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		res := tx.DB().Exec(`INSERT INTO `+table+` (tenant, caller, method, route, key, fingerprint, claim, claimed_at, operation_ref, started_at)
   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (tenant, caller, method, route, `+keyColumn+`) DO NOTHING`,
			s.Tenant, s.Caller, s.Method, s.Route, s.Key, s.Fingerprint[:], claim, at, nullableUUID(ref), started)
		if res.Error != nil {
			return res.Error
		}
		existing, err := readClaim(tx, s, true)
		if err != nil {
			return err
		}
		if existing.claim == claim {
			verdict = Verdict{Kind: First, Claim: claim, OperationRef: ref, StartedAt: at}
			return fence(tx, s, claim)
		}
		if existing.operationRef != uuid.Nil {
			var receipt operationRow
			err := tx.DB().Raw(`SELECT * FROM platformkit_operations WHERE tenant_id = ? AND id = ? FOR UPDATE`, s.Tenant, existing.operationRef).Scan(&receipt).Error
			if err != nil {
				return err
			}
			if receipt.ID != uuid.Nil {
				r := receipt.receipt()
				existing.receipt = &r
			}
		}
		if existing.began && !existing.completed && at.Sub(existing.claimedAt) >= lease {
			proved, committed, err := resolveEffect(tx, s, existing.claim)
			if err != nil {
				return err
			}
			if !proved {
				verdict = Verdict{Kind: InFlight, OperationRef: existing.operationRef, StartedAt: existing.startedAt}
				return nil
			}
			existing.effect = committed
		}
		verdict = decide(existing, true, s, at, claim, lease, retention)
		if verdict.Kind != First {
			return nil
		}
		fresh := claimExpired(existing, at, retention)
		if !fresh {
			ref = existing.operationRef
			started = existing.startedAt
			if ref == uuid.Nil {
				started = nil
			}
		}
		res = tx.DB().Exec(`UPDATE `+table+` SET claim=?, claimed_at=?, fingerprint=?, completed_at=NULL,
   recorded=FALSE, status=NULL, content_type=NULL, body=NULL, headers=NULL, limits=NULL, began=FALSE,
   operation_ref=?, started_at=? WHERE tenant=? AND caller=? AND method=? AND route=? AND `+keyColumn+`=?`,
			claim, at, s.Fingerprint[:], nullableUUID(ref), started, s.Tenant, s.Caller, s.Method, s.Route, s.Key)
		if res.Error != nil {
			return res.Error
		}
		verdict.OperationRef = ref
		if t, ok := started.(time.Time); ok {
			verdict.StartedAt = t
		}
		return fence(tx, s, claim)
	})
	if err != nil {
		return Verdict{}, fmt.Errorf("idempotency: claim: %w", err)
	}
	return verdict, nil
}

// Mark sets this attempt's effect marker inside the caller's own transaction, under
// the marker's lock, which that transaction then holds until it ends.
func (p postgres) Mark(_ context.Context, tx db.Tx[db.Tenant], s Scope, claim uuid.UUID) error {
	if err := tx.DB().Exec(effectLockTakeSQL, effectLockTarget(s)).Error; err != nil {
		return fmt.Errorf("idempotency: mark: %w", err)
	}
	// One statement, conditional on the attempt the row names: the row Claim wrote when
	// it handed this attempt the key, or a retry's once the key has moved on. A row
	// naming somebody else is left alone and answers zero rows, which is the fence. The
	// insert is for a key no claim named, which a request through kit/httpx never is.
	res := tx.DB().Exec(`INSERT INTO `+effectTable+`
			(tenant_id, caller, method, route, `+keyColumn+`, claim, committed)
			VALUES (?, ?, ?, ?, ?, ?, TRUE)
			ON CONFLICT (tenant_id, caller, method, route, `+keyColumn+`)
			DO UPDATE SET committed = TRUE, applied_at = clock_timestamp()
			WHERE `+effectTable+`.claim = EXCLUDED.claim`,
		s.Tenant, s.Caller, s.Method, s.Route, s.Key, claim)
	if res.Error != nil {
		return fmt.Errorf("idempotency: mark: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrLostClaim
	}
	return nil
}

// fence names the attempt Claim has just handed the key to in the key's effect row,
// unmarked, inside the transaction that hands it over. From then on only that
// attempt's transaction can mark the row, so an earlier attempt still running past its
// lease cannot commit beside it (Mark). A scope with no tenant has no tenant
// transaction to mark it from, and gets no row. Non-HTTP COMMAND roots retain
// conservative refusal because their transaction owners do not call Mark.
func fence(tx db.Tx[db.System], s Scope, claim uuid.UUID) error {
	if s.Tenant == uuid.Nil || (s.Track && s.Method == "COMMAND") {
		return nil
	}
	return tx.DB().Exec(`INSERT INTO `+effectTable+`
			(tenant_id, caller, method, route, `+keyColumn+`, claim, committed)
			VALUES (?, ?, ?, ?, ?, ?, FALSE)
			ON CONFLICT (tenant_id, caller, method, route, `+keyColumn+`)
			DO UPDATE SET claim = EXCLUDED.claim, committed = FALSE, applied_at = clock_timestamp()`,
		s.Tenant, s.Caller, s.Method, s.Route, s.Key, claim).Error
}

// resolveEffect asks whether the transaction that held this record's claim reached its
// COMMIT, and answers whether that reading is evidence at all.
//
// It asks for the mark of the attempt the record names, and that is the whole
// question: the row names the record's current claim (fence) and no other attempt can
// mark it, so an unmarked row is an attempt whose transaction is not in the database.
//
// proved is false when the lock belongs to somebody else — a COMMIT of this command is
// executing, and its mark is written but not yet visible — in which case committed
// says nothing and the caller must not act on it.
func resolveEffect(tx db.Tx[db.System], s Scope, claim uuid.UUID) (proved, committed bool, err error) {
	if err := tx.DB().Raw(effectLockTrySQL, effectLockTarget(s)).Row().Scan(&proved); err != nil {
		return false, false, fmt.Errorf("idempotency: resolve: %w", err)
	}
	if !proved {
		return false, false, nil
	}
	// Missing fences predate this protocol or belong to non-HTTP command roots.
	// Neither absence proves rollback; only an existing unmarked fence does.
	err = tx.DB().Raw(`SELECT COALESCE((SELECT committed FROM `+effectTable+`
		WHERE tenant_id = ? AND caller = ? AND method = ? AND route = ? AND `+keyColumn+` = ? AND claim = ?), TRUE)`,
		s.Tenant, s.Caller, s.Method, s.Route, s.Key, claim).Row().Scan(&committed)
	return true, committed, err
}

func readClaim(tx db.Tx[db.System], s Scope, lock bool) (*row, error) {
	q := `SELECT fingerprint, claim, claimed_at, completed_at IS NOT NULL, recorded,
  COALESCE(status,0), COALESCE(content_type,''), body, headers, limits, began, operation_ref, started_at
  FROM ` + table + ` WHERE tenant=? AND caller=? AND method=? AND route=? AND ` + keyColumn + `=?`
	if lock {
		q += ` FOR UPDATE`
	}
	rows, err := tx.DB().Raw(q, s.Tenant, s.Caller, s.Method, s.Route, s.Key).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, errors.New("idempotency: claim disappeared")
	}
	var r row
	var fp, kept, counted []byte
	var ref *uuid.UUID
	var started *time.Time
	if err := rows.Scan(&fp, &r.claim, &r.claimedAt, &r.completed, &r.recorded, &r.answer.Status, &r.answer.ContentType, &r.answer.Body, &kept, &counted, &r.began, &ref, &started); err != nil {
		return nil, err
	}
	copy(r.fingerprint[:], fp)
	if ref != nil {
		r.operationRef = *ref
	}
	if started != nil {
		r.startedAt = *started
	}
	r.answer.Headers, err = decodeHeaders(kept)
	if err != nil {
		return nil, err
	}
	r.answer.Limits, err = decodeLimits(counted)
	if err != nil {
		return nil, err
	}
	return &r, rows.Err()
}

func (p postgres) Begin(ctx context.Context, s Scope, claim uuid.UUID, at time.Time) error {
	// One conditional statement, for the reason kit/limit gives for merging its read
	// and its write: the row is what decides, and a read-then-write is two answers
	// about one attempt. A row that answers zero is a claim this attempt no longer
	// holds or an answer already landed on it, and the caller must not walk in to the
	// write on either.
	err := p.run(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		res := tx.DB().Exec(`UPDATE `+table+` SET began = TRUE
			WHERE tenant = ? AND caller = ? AND method = ? AND route = ? AND `+keyColumn+` = ?
			  AND claim = ? AND began = FALSE AND completed_at IS NULL`,
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
		return fmt.Errorf("idempotency: begin: %w", err)
	}
	return err
}

func (p postgres) Complete(ctx context.Context, s Scope, claim uuid.UUID, a Answer, at time.Time) error {
	recorded := a.Status != 0
	headers, err := encodeHeaders(a.Headers)
	if err != nil {
		// An answer this package cannot write down is an answer it must not half-
		// remember: the status alone would be replayed as the whole of it.
		return fmt.Errorf("idempotency: complete: %w", err)
	}
	counted, err := encodeLimits(a.Limits)
	if err != nil {
		return fmt.Errorf("idempotency: complete: %w", err)
	}
	err = p.run(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		res := tx.DB().Exec(`UPDATE `+table+`
			SET completed_at = ?, recorded = ?, status = ?, content_type = ?, body = ?, headers = ?, limits = ?
			WHERE tenant = ? AND caller = ? AND method = ? AND route = ? AND `+keyColumn+` = ?
			  AND claim = ? AND completed_at IS NULL`,
			at, recorded, a.Status, a.ContentType, answerBody(a), answerHeaders(a, headers), answerLimits(a, counted),
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

// Purge uses db.Now to retire eligible receipts and their remembered requests.
// Call PurgeAt when the transaction owner supplies a clock.
func Purge(ctx context.Context, conn *db.Conn) error { return PurgeAt(ctx, conn, db.Now()) }

// PurgeAt retires eligible terminal receipts and their retry identities under
// their row locks. Unknown and approval-pending outcomes are never discarded.
func PurgeAt(ctx context.Context, conn *db.Conn, at time.Time) error {
	if at.IsZero() {
		return fmt.Errorf("idempotency: purge needs an instant")
	}
	return db.RunSystem(ctx, conn, systemToken, func(_ context.Context, tx db.Tx[db.System]) error {
		var scopes []struct {
			Tenant uuid.UUID
			Caller uuid.UUID
			Method string
			Route  string
			Key    string
		}
		if err := tx.DB().Raw(`SELECT tenant,caller,method,route,key FROM platformkit_idempotency
   WHERE claimed_at <= ? ORDER BY tenant,caller,method,route,key FOR UPDATE SKIP LOCKED`, at.Add(-retention)).Scan(&scopes).Error; err != nil {
			return err
		}
		for _, s := range scopes {
			scope := Scope{Tenant: s.Tenant, Caller: s.Caller, Method: s.Method, Route: s.Route, Key: s.Key}
			r, err := readClaim(tx, scope, false)
			if err != nil {
				return err
			}
			if r.operationRef != uuid.Nil {
				if err := tx.DB().Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "platformkit.operation-set."+s.Tenant.String()).Error; err != nil {
					return err
				}
				var row operationRow
				if err := tx.DB().Raw(`SELECT * FROM platformkit_operations WHERE tenant_id=? AND id=? FOR UPDATE`, s.Tenant, r.operationRef).Scan(&row).Error; err != nil {
					return err
				}
				if row.ID != uuid.Nil {
					receipt := row.receipt()
					r.receipt = &receipt
				}
			}
			if !claimExpired(r, at, retention) {
				continue
			}
			if err := tx.DB().Exec(`DELETE FROM platformkit_idempotency WHERE tenant=? AND caller=? AND method=? AND route=? AND `+keyColumn+`=?`, s.Tenant, s.Caller, s.Method, s.Route, s.Key).Error; err != nil {
				return err
			}
			if r.operationRef != uuid.Nil {
				if err := tx.DB().Exec(`DELETE FROM platformkit_operations WHERE tenant_id=? AND id=?`, s.Tenant, r.operationRef).Error; err != nil {
					return err
				}
			}
		}
		var orphans []*operationRow
		if err := tx.DB().Raw(`SELECT o.* FROM platformkit_operations o WHERE o.outcome IN ('succeeded','failed')
   AND NOT EXISTS (SELECT 1 FROM platformkit_idempotency k WHERE k.tenant=o.tenant_id AND k.operation_ref=o.id)
   ORDER BY o.tenant_id,o.id FOR UPDATE OF o SKIP LOCKED`).Scan(&orphans).Error; err != nil {
			return err
		}
		for _, row := range orphans {
			if expired(row.receipt(), at) {
				if err := tx.DB().Exec(`DELETE FROM platformkit_operations WHERE tenant_id=? AND id=?`, row.TenantID, row.ID).Error; err != nil {
					return err
				}
			}
		}

		// A retained retry identity still needs its fence, including async receipts.
		return tx.DB().Exec(`DELETE FROM `+effectTable+` e WHERE applied_at < ?
			AND NOT EXISTS (SELECT 1 FROM `+table+` k WHERE k.tenant=e.tenant_id
			AND k.caller=e.caller AND k.method=e.method AND k.route=e.route AND k.key=e.key)`, at.Add(-retention)).Error
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
	operationRef uuid.UUID
	startedAt    time.Time
	receipt      *Receipt
	fingerprint  [sha256.Size]byte
	claim        uuid.UUID
	claimedAt    time.Time
	completed    bool
	recorded     bool
	// began says the attempt that holds this claim was let in to do the write. It
	// is a boolean and not a timestamp because nothing reads the moment: the one
	// question it answers is whether a claim whose lease ran out may be taken up
	// again, and a time would be a field nothing consults.
	began  bool
	effect bool
	answer Answer
}

// decide is the whole disposition, and the only place it is decided.
//
// `found` is separate from a nil record rather than folded into it because
// "nobody holds this key" and "the record aged out a moment ago" are the same
// answer for a caller and two different facts for whoever reads this function
// next; the purge is the only thing that deletes, and a store that has not run
// one yet still answers a two-day-old claim as First.
func decide(existing *row, found bool, want Scope, at time.Time, claim uuid.UUID, lease, retention time.Duration) Verdict {
	if !found || existing == nil || claimExpired(existing, at, retention) {
		return Verdict{Kind: First, Claim: claim}
	}
	result := func(v Verdict) Verdict {
		v.OperationRef = existing.operationRef
		v.StartedAt = existing.startedAt
		return v
	}
	if existing.fingerprint != want.Fingerprint {
		// Said before any look at the state: a different write under this key is
		// the caller's mistake whether or not the first one finished, and the
		// answer they need — send it under a fresh key — does not change.
		return result(Verdict{Kind: ConflictBody})
	}
	if existing.operationRef != uuid.Nil && at.Sub(existing.startedAt) >= retention {
		return result(Verdict{Kind: Spent})
	}
	if existing.completed {
		if !existing.recorded {
			return result(Verdict{Kind: Spent})
		}
		return result(Verdict{Kind: Replay, Stored: existing.answer})
	}
	if held := at.Sub(existing.claimedAt); held < lease {
		return result(Verdict{Kind: InFlight, Lease: lease - held})
	}
	// A known outcome already settled the intent, even if answer recording failed.
	if existing.began && (existing.effect || (existing.receipt != nil && existing.receipt.Outcome != "")) {
		return result(Verdict{Kind: Spent})
	}
	return result(Verdict{Kind: First, Claim: claim})
}

func claimExpired(r *row, at time.Time, span time.Duration) bool {
	if r.operationRef != uuid.Nil {
		return r.receipt != nil && expired(*r.receipt, at)
	}
	return at.Sub(r.claimedAt) >= span
}

// keyColumn is the column holding the caller's key, named once because it is a
// reserved word in the middle of five statements.
const keyColumn = `"key"`

// answerBody keeps nil for a spent record, so a row that holds no answer holds
// no empty bytea either.
func answerBody(a Answer) []byte {
	if a.Status == 0 {
		return nil
	}
	return a.Body
}

// answerHeaders is the same rule for the headers: nothing is kept of an answer
// that is not being kept.
func answerHeaders(a Answer, encoded []byte) []byte {
	if a.Status == 0 {
		return nil
	}
	return encoded
}

// answerLimits is that rule for the ceilings, which belong to the answer for the
// same reason the headers do: a 429 replayed without the limit that decided it
// states a refusal the caller cannot act on.
func answerLimits(a Answer, encoded []byte) []byte {
	if a.Status == 0 {
		return nil
	}
	return encoded
}

// encodeHeaders is the row's own spelling of a kept header list: JSON in a bytea,
// which is how the body is already held, so neither column is parsed by anything
// but this package and a name kept is a name replayed verbatim. A list rather
// than an object, because the same name may appear twice.
func encodeHeaders(h []Header) ([]byte, error) {
	if len(h) == 0 {
		return nil, nil
	}
	return json.Marshal(h)
}

func decodeHeaders(kept []byte) ([]Header, error) {
	if len(kept) == 0 {
		return nil, nil
	}
	var h []Header
	if err := json.Unmarshal(kept, &h); err != nil {
		return nil, err
	}
	return h, nil
}

// encodeLimits is the same spelling for the same reason: JSON in a bytea, held by a
// package that never reads it — the names, the windows and the reading are
// kit/httpx's, and the row is the memory that has to survive a deploy.
func encodeLimits(l []Limit) ([]byte, error) {
	if len(l) == 0 {
		return nil, nil
	}
	return json.Marshal(l)
}

func decodeLimits(counted []byte) ([]Limit, error) {
	if len(counted) == 0 {
		return nil, nil
	}
	var l []Limit
	if err := json.Unmarshal(counted, &l); err != nil {
		return nil, err
	}
	return l, nil
}

func validClaim(s Scope, at time.Time) error {
	if s.Track && (s.Tenant == uuid.Nil || !request.ValidToken(s.Key) || s.Method == "" || s.Route == "" || at.IsZero()) {
		return fmt.Errorf("idempotency: invalid tracked claim")
	}
	return nil
}
