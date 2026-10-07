package idempotency_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The clock every case writes with. A record's age is the one fact the lease and
// the retention turn on, so both stores take it as an argument rather than reading
// a wall clock of their own, and the cases can say "an hour ago" instead of
// sleeping for it.
var start = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

var (
	acme  = uuid.New()
	other = uuid.New()
	ada   = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	bob   = uuid.MustParse("33333333-3333-4333-8333-333333333333")
)

// TestBothStoresAgree runs one suite against both implementations, which is what
// entitles every kit/httpx case to run on the in-memory one: an interface is
// justified by a passing fake, and this is the passing. It is the same argument and
// the same shape as kit/limit's TestBothLimitersAgree, and the same reason for the
// store at all — the memory a keyed write needs has to be the one the next replica
// reads.
func TestBothStoresAgree(t *testing.T) {
	for implementation, build := range map[string]func(*testing.T) (idempotency.Store, context.Context){
		"memory":   inMemory,
		"postgres": inPostgres,
	} {
		t.Run(implementation, func(t *testing.T) {
			for name, run := range cases {
				t.Run(name, func(t *testing.T) {
					store, ctx := build(t)
					run(t, store, ctx)
				})
			}
		})
	}
}

var cases = map[string]func(*testing.T, idempotency.Store, context.Context){
	"the first write claims the key": func(t *testing.T, s idempotency.Store, ctx context.Context) {
		v, err := s.Claim(ctx, scope("k1"), start)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if v.Kind != idempotency.First {
			t.Fatalf("first claim = %v, want First", v.Kind)
		}
		if v.Claim == uuid.Nil {
			t.Error("a First verdict with no claim token is a claim nobody can complete")
		}
	},

	"the same bytes replay the answer": func(t *testing.T, s idempotency.Store, ctx context.Context) {
		same := scope("replay")
		first, err := s.Claim(ctx, same, start)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		answer := idempotency.Answer{Status: 201, ContentType: "application/json", Body: []byte(`{"id":"1"}`)}
		if err := s.Complete(ctx, same, first.Claim, answer, start.Add(time.Second)); err != nil {
			t.Fatalf("complete: %v", err)
		}
		v, err := s.Claim(ctx, same, start.Add(time.Minute))
		if err != nil {
			t.Fatalf("replay claim: %v", err)
		}
		if v.Kind != idempotency.Replay {
			t.Fatalf("retry of a completed write = %v, want Replay", v.Kind)
		}
		if v.Stored.Status != 201 || string(v.Stored.Body) != `{"id":"1"}` || v.Stored.ContentType != "application/json" {
			t.Errorf("replayed answer = %+v, want the first attempt's answer byte for byte", v.Stored)
		}
	},

	"a refused answer replays too": func(t *testing.T, s idempotency.Store, ctx context.Context) {
		// The store does not know which status is a refusal and which is a success,
		// and it must not care: a 422 the caller was given is the answer to that key,
		// and re-running the write to discover it fails again is the second write.
		for _, status := range []int{422, 429, 503} {
			same := scope("refused " + strconv.Itoa(status))
			first, err := s.Claim(ctx, same, start)
			if err != nil {
				t.Fatalf("claim: %v", err)
			}
			if err := s.Complete(ctx, same, first.Claim, idempotency.Answer{Status: status, Body: []byte("no")}, start); err != nil {
				t.Fatalf("complete: %v", err)
			}
			v, err := s.Claim(ctx, same, start)
			if err != nil {
				t.Fatalf("replay claim: %v", err)
			}
			if v.Kind != idempotency.Replay || v.Stored.Status != status {
				t.Errorf("a stored %d replayed as %+v, want the same status", status, v)
			}
		}
	},

	"a different body is a conflict, not a replay": func(t *testing.T, s idempotency.Store, ctx context.Context) {
		one := scope("conflict")
		first, err := s.Claim(ctx, one, start)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if err := s.Complete(ctx, one, first.Claim, idempotency.Answer{Status: 201, Body: []byte("first")}, start); err != nil {
			t.Fatalf("complete: %v", err)
		}
		different := one
		different.Fingerprint = sum("other bytes")
		v, err := s.Claim(ctx, different, start.Add(time.Minute))
		if err != nil {
			t.Fatalf("conflicting claim: %v", err)
		}
		if v.Kind != idempotency.ConflictBody {
			t.Fatalf("the same key with another body = %v, want ConflictBody", v.Kind)
		}
		// And the record the conflict did not touch still replays: refusing the
		// second caller must not spend the first one's answer.
		v, err = s.Claim(ctx, one, start.Add(time.Minute))
		if err != nil {
			t.Fatalf("claim after a conflict: %v", err)
		}
		if v.Kind != idempotency.Replay || string(v.Stored.Body) != "first" {
			t.Errorf("a conflict disturbed the stored answer: %+v", v)
		}
	},

	"a conflict is a conflict while the first is still in flight": func(t *testing.T, s idempotency.Store, ctx context.Context) {
		one := scope("in-flight-conflict")
		if _, err := s.Claim(ctx, one, start); err != nil {
			t.Fatalf("claim: %v", err)
		}
		different := one
		different.Fingerprint = sum("other bytes")
		v, err := s.Claim(ctx, different, start.Add(time.Second))
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		// The answer that names the caller's own mistake outranks the one that says
		// "wait": no amount of waiting makes the second request the same write.
		if v.Kind != idempotency.ConflictBody {
			t.Errorf("a different body under a claim in flight = %v, want ConflictBody", v.Kind)
		}
	},

	"in flight is refused with the lease left to run": func(t *testing.T, s idempotency.Store, ctx context.Context) {
		one := scope("in-flight")
		if _, err := s.Claim(ctx, one, start); err != nil {
			t.Fatalf("claim: %v", err)
		}
		v, err := s.Claim(ctx, one, start.Add(time.Minute))
		if err != nil {
			t.Fatalf("second claim: %v", err)
		}
		if v.Kind != idempotency.InFlight {
			t.Fatalf("a retry while the first runs = %v, want InFlight", v.Kind)
		}
		if v.Lease <= 0 || v.Lease > idempotency.Lease {
			t.Errorf("lease = %s, want what is left of %s", v.Lease, idempotency.Lease)
		}
	},

	"a claim older than the lease is taken over once": func(t *testing.T, s idempotency.Store, ctx context.Context) {
		one := scope("takeover")
		if _, err := s.Claim(ctx, one, start); err != nil {
			t.Fatalf("claim: %v", err)
		}
		later := start.Add(idempotency.Lease + time.Minute)
		first, err := s.Claim(ctx, one, later)
		if err != nil {
			t.Fatalf("claim after the lease: %v", err)
		}
		if first.Kind != idempotency.First {
			t.Fatalf("an abandoned claim = %v, want First: a pod that died mid-write would freeze the key for the whole retention", first.Kind)
		}
		second, err := s.Claim(ctx, one, later)
		if err != nil {
			t.Fatalf("claim against the new holder: %v", err)
		}
		if second.Kind != idempotency.InFlight {
			t.Errorf("the second taker = %v, want InFlight: two retries of one abandoned write must not both run", second.Kind)
		}
	},

	"an answer from an attempt that lost the claim is dropped": func(t *testing.T, s idempotency.Store, ctx context.Context) {
		one := scope("lost")
		abandoned, err := s.Claim(ctx, one, start)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		later := start.Add(idempotency.Lease + time.Minute)
		winner, err := s.Claim(ctx, one, later)
		if err != nil {
			t.Fatalf("takeover: %v", err)
		}
		if err := s.Complete(ctx, one, winner.Claim, idempotency.Answer{Status: 201, Body: []byte("winner")}, later); err != nil {
			t.Fatalf("complete: %v", err)
		}
		// The abandoned attempt finishes late and tries to land its own answer.
		err = s.Complete(ctx, one, abandoned.Claim, idempotency.Answer{Status: 201, Body: []byte("loser")}, later)
		if !errors.Is(err, idempotency.ErrLostClaim) {
			t.Errorf("a completion from the lost attempt = %v, want ErrLostClaim", err)
		}
		v, err := s.Claim(ctx, one, later)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if v.Kind != idempotency.Replay || string(v.Stored.Body) != "winner" {
			t.Errorf("the lost attempt overwrote the winner: %+v", v)
		}
	},

	"a key is one caller's, on one route, with one verb, in one tenant": func(t *testing.T, s idempotency.Store, ctx context.Context) {
		first := scope("shared")
		if _, err := s.Claim(ctx, first, start); err != nil {
			t.Fatalf("claim: %v", err)
		}
		for name, other := range map[string]idempotency.Scope{
			"another tenant": {Tenant: other, Caller: ada, Method: "POST", Route: "/tasks", Key: "shared", Fingerprint: first.Fingerprint},
			"another caller": {Tenant: acme, Caller: bob, Method: "POST", Route: "/tasks", Key: "shared", Fingerprint: first.Fingerprint},
			"another route":  {Tenant: acme, Caller: ada, Method: "POST", Route: "/projects", Key: "shared", Fingerprint: first.Fingerprint},
			"another verb":   {Tenant: acme, Caller: ada, Method: "PATCH", Route: "/tasks", Key: "shared", Fingerprint: first.Fingerprint},
		} {
			v, err := s.Claim(ctx, other, start)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			// Each is First rather than InFlight: a key reused across identities is
			// not the same write, and refusing it would be a 409 with nothing to wait
			// for. And none of them carries the first caller's answer.
			if v.Kind != idempotency.First {
				t.Errorf("%s under the same key = %v, want First", name, v.Kind)
			}
		}
	},

	"a spent key is spent": func(t *testing.T, s idempotency.Store, ctx context.Context) {
		// The zero answer is the record of a write whose answer could not be kept.
		// It must never be re-run: the cost of losing an answer is a 409, and the
		// cost of forgetting the record would be the second write.
		one := scope("spent")
		first, err := s.Claim(ctx, one, start)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if err := s.Complete(ctx, one, first.Claim, idempotency.Answer{}, start); err != nil {
			t.Fatalf("complete as spent: %v", err)
		}
		v, err := s.Claim(ctx, one, start.Add(time.Minute))
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if v.Kind != idempotency.Spent {
			t.Fatalf("a spent key = %v, want Spent", v.Kind)
		}
	},
}

// TestPurgeKeepsWhatIsInsideTheDay asks the retention of the store that has to
// enforce it. The memory implementation is bounded by its own map and is not the
// deployment, so this is Postgres's own case rather than one the conformance suite
// shares — the same split kit/limit makes with TestPurgeDropsWindowsThatClosedLongAgo.
//
// It is anchored on the wall clock rather than on the suite's synthetic instant,
// because the sweep is Postgres's own now(): the store takes the caller's clock for
// a lease and the database's for a day, and a case that aged a row against a clock
// neither clock reads would assert a number nobody computed.
func TestPurgeKeepsWhatIsInsideTheDay(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	ctx := httpx.WithConn(t.Context(), conn)
	s := idempotency.Postgres(httpx.ConnFrom)
	now := time.Now().UTC()

	// A completed record a day and an hour past its claim, and a completed one
	// inside the day. Completed rather than claimed, because an incomplete claim
	// older than the lease has its own answer — the takeover — and this case is the
	// retention's, not that one's.
	aged, spent := scope("yesterday"), scope("fresh")
	for _, c := range []struct {
		scope idempotency.Scope
		at    time.Time
	}{{aged, now.Add(-25 * time.Hour)}, {spent, now.Add(-23 * time.Hour)}} {
		v, err := s.Claim(ctx, c.scope, c.at)
		if err != nil {
			t.Fatalf("claim at %s: %v", c.at, err)
		}
		if err := s.Complete(ctx, c.scope, v.Claim, idempotency.Answer{Status: 201, Body: []byte("held")}, c.at.Add(time.Hour)); err != nil {
			t.Fatalf("complete: %v", err)
		}
	}
	if err := idempotency.Purge(t.Context(), conn); err != nil {
		t.Fatalf("purge: %v", err)
	}
	var left int
	if err := admin.QueryRow("SELECT count(*) FROM platformkit_idempotency").Scan(&left); err != nil {
		t.Fatalf("count what the sweep left: %v", err)
	}
	if left != 1 {
		t.Errorf("the purge left %d records, want the one inside the day", left)
	}
	if v, err := s.Claim(ctx, spent, now); err != nil {
		t.Fatalf("claim the record inside the day: %v", err)
	} else if v.Kind != idempotency.Replay || string(v.Stored.Body) != "held" {
		t.Errorf("a record inside the day was purged: %+v", v)
	}
	if v, err := s.Claim(ctx, aged, now); err != nil {
		t.Fatalf("claim the aged record: %v", err)
	} else if v.Kind != idempotency.First {
		t.Errorf("a record a day and an hour old survived the purge: %+v", v)
	}
}

// TestAContextWithNoConnectionIsAnError, and not a silent allowance: kit/httpx
// refuses a keyed write it cannot record, and a store that answered "claimed"
// without a row behind it would be a promise nobody kept.
func TestAContextWithNoConnectionIsAnError(t *testing.T) {
	s := idempotency.Postgres(httpx.ConnFrom)
	if _, err := s.Claim(t.Context(), scope("k"), start); !errors.Is(err, idempotency.ErrNoConnection) {
		t.Errorf("Claim with no connection = %v, want ErrNoConnection", err)
	}
	err := s.Complete(t.Context(), scope("k"), uuid.New(), idempotency.Answer{Status: 201}, start)
	if !errors.Is(err, idempotency.ErrNoConnection) {
		t.Errorf("Complete with no connection = %v, want ErrNoConnection", err)
	}
}

// TestTheRecordIsNoTenantVisible asks the row-level-security half of the record:
// the door is kit/idempotency and its capability, and a tenant transaction cannot
// read a record at all — not another tenant's and not its own. The claim is written
// under a tenant's context and lands in no tenant's scope, because the row is
// protocol state written by a package that holds a system token; what is asserted
// is the other direction, that no tenant can read it back.
func TestTheRecordIsNoTenantVisible(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: acme, Slug: "acme"}), conn)
	s := idempotency.Postgres(httpx.ConnFrom)
	if _, err := s.Claim(ctx, scope("rls"), start); err != nil {
		t.Fatalf("claim under a tenant context: %v", err)
	}

	var seen int
	err := db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: acme, Slug: "acme"}), conn,
		func(_ context.Context, tx db.Tx[db.Tenant]) error {
			rows, err := tx.DB().Raw("SELECT count(*) FROM platformkit_idempotency").Rows()
			if err != nil {
				return err
			}
			defer rows.Close()
			if !rows.Next() {
				return rows.Err()
			}
			return rows.Scan(&seen)
		})
	if err != nil {
		t.Fatalf("read the table as a tenant: %v", err)
	}
	if seen != 0 {
		t.Errorf("a tenant transaction read %d idempotency records, want none: the door is the package", seen)
	}

	var owner int
	row := admin.QueryRow("SELECT count(*) FROM platformkit_idempotency WHERE key = 'rls' AND tenant = $1", acme)
	if err := row.Scan(&owner); err != nil {
		t.Fatalf("count as the schema owner: %v", err)
	}
	if owner != 1 {
		t.Errorf("a keyed write left %d records, want the one it promised", owner)
	}
}

func inMemory(t *testing.T) (idempotency.Store, context.Context) {
	t.Helper()
	return idempotency.Memory(nil), t.Context()
}

func inPostgres(t *testing.T) (idempotency.Store, context.Context) {
	t.Helper()
	_, conn := dbtest.Schema(t)
	return idempotency.Postgres(httpx.ConnFrom), httpx.WithConn(t.Context(), conn)
}

// scope is one keyed write of one caller of one tenant at one route.
func scope(key string) idempotency.Scope {
	return idempotency.Scope{
		Tenant:      acme,
		Caller:      ada,
		Method:      "POST",
		Route:       "/tasks",
		Key:         key,
		Fingerprint: sum("the same body every time"),
	}
}

// sum is a body digest, standing in for the middleware's fingerprint.
func sum(body string) [sha256.Size]byte {
	return sha256.Sum256([]byte(body))
}
