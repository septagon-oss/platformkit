package idempotency_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type operationFixture struct {
	store idempotency.Store
	ops   idempotency.Operations
	ctx   context.Context
	run   func(context.Context, func(context.Context, db.Tx[db.Tenant]) error) error
}

func operationFixtureFor(t *testing.T, sql bool) operationFixture {
	t.Helper()
	build := inMemory
	if sql {
		build = inPostgres
	}
	store, ctx := build(t)
	f := operationFixture{store: store, ctx: tenancy.WithTenant(ctx, tenancy.Tenant{ID: acme})}
	if !sql {
		f.ops = store.(idempotency.Operations)
		f.run = func(ctx context.Context, fn func(context.Context, db.Tx[db.Tenant]) error) error {
			return fn(ctx, db.Tx[db.Tenant]{})
		}
	} else {
		f.ops = idempotency.PostgresOperations()
		conn, _ := httpx.ConnFrom(ctx)
		f.run = func(ctx context.Context, fn func(context.Context, db.Tx[db.Tenant]) error) error {
			return db.Run(ctx, conn, fn)
		}
	}
	return f
}

func (f operationFixture) reserve(t *testing.T, key string) (idempotency.Scope, idempotency.Verdict, idempotency.Receipt) {
	t.Helper()
	s := scope(key)
	s.Track = true
	v, err := f.store.Claim(f.ctx, s, start)
	if err != nil {
		t.Fatal(err)
	}
	r := idempotency.Receipt{Ref: v.OperationRef, ID: key, Tenant: acme, Caller: ada, Person: ada, Method: s.Method, Route: s.Route, Revision: 1, StartedAt: v.StartedAt}
	if err := f.run(f.ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return f.ops.Reserve(ctx, tx, r) }); err != nil {
		t.Fatal(err)
	}
	return s, v, r
}

func TestOperationStoresAgree(t *testing.T) {
	for _, sql := range []bool{false, true} {
		name := "memory"
		if sql {
			name = "postgres"
		}
		t.Run(name, func(t *testing.T) {
			t.Run("retry and offline replay retain occurrence", func(t *testing.T) {
				f := operationFixtureFor(t, sql)
				s, v, r := f.reserve(t, "phone.persisted-key")
				if err := f.store.Begin(f.ctx, s, v.Claim, start); err != nil {
					t.Fatal(err)
				}
				if err := f.run(f.ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					got, changed, err := f.ops.Transition(ctx, tx, r.Operation(), 1, request.Succeeded, start.Add(time.Second))
					if err == nil && (!changed || got.Revision != 2) {
						t.Errorf("transition = %+v, changed %v", got, changed)
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
				if err := f.store.Complete(f.ctx, s, v.Claim, idempotency.Answer{Status: 201}, start.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				retry, err := f.store.Claim(f.ctx, s, start.Add(time.Minute))
				if err != nil {
					t.Fatal(err)
				}
				if retry.Kind != idempotency.Replay || retry.OperationRef != r.Ref || !retry.StartedAt.Equal(r.StartedAt) {
					t.Fatalf("retry = %+v", retry)
				}
			})
			t.Run("lease takeover retains reservation", func(t *testing.T) {
				f := operationFixtureFor(t, sql)
				s, first, r := f.reserve(t, "abandoned")
				later, err := f.store.Claim(f.ctx, s, start.Add(idempotency.Lease+time.Second))
				if err != nil {
					t.Fatal(err)
				}
				if later.Kind != idempotency.First || later.Claim == first.Claim || later.OperationRef != r.Ref || !later.StartedAt.Equal(start) {
					t.Fatalf("takeover = %+v", later)
				}
				if err := f.run(f.ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return f.ops.Reserve(ctx, tx, r) }); err != nil {
					t.Fatal(err)
				}
			})
			t.Run("unknown and approval identities outlive the answer", func(t *testing.T) {
				for _, outcome := range []request.Outcome{"", request.PendingApproval} {
					f := operationFixtureFor(t, sql)
					s, v, r := f.reserve(t, "held")
					if err := f.store.Begin(f.ctx, s, v.Claim, start); err != nil {
						t.Fatal(err)
					}
					if outcome != "" {
						if err := f.run(f.ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
							_, _, err := f.ops.Transition(ctx, tx, r.Operation(), 1, outcome, start)
							return err
						}); err != nil {
							t.Fatal(err)
						}
					}
					if err := f.store.Complete(f.ctx, s, v.Claim, idempotency.Answer{Status: 202}, start); err != nil {
						t.Fatal(err)
					}
					retry, err := f.store.Claim(f.ctx, s, start.Add(48*time.Hour))
					if err != nil {
						t.Fatal(err)
					}
					if retry.Kind != idempotency.Spent || retry.OperationRef != r.Ref {
						t.Errorf("aged %q = %+v", outcome, retry)
					}
				}
			})
			t.Run("ownership scope and immutable reservation", func(t *testing.T) {
				f := operationFixtureFor(t, sql)
				_, _, r := f.reserve(t, "private")
				for _, person := range []uuid.UUID{bob, uuid.Nil} {
					err := f.run(f.ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
						got, err := f.ops.Get(ctx, tx, r.Ref, person, start)
						if got.Ref != uuid.Nil {
							t.Errorf("refused read returned %+v", got)
						}
						return err
					})
					if !errors.Is(err, crud.ErrNotFound) {
						t.Errorf("person read = %v", err)
					}
				}
				wrong := tenancy.WithTenant(f.ctx, tenancy.Tenant{ID: other})
				err := f.run(wrong, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					_, err := f.ops.Get(ctx, tx, r.Ref, ada, start)
					return err
				})
				if !errors.Is(err, crud.ErrNotFound) {
					t.Errorf("wrong tenant read = %v", err)
				}
				err = f.run(wrong, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					_, _, err := f.ops.Transition(ctx, tx, r.Operation(), 1, request.Succeeded, start)
					return err
				})
				if !errors.Is(err, crud.ErrNotFound) {
					t.Errorf("wrong tenant write = %v", err)
				}
				forged := r
				forged.Person = bob
				err = f.run(f.ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return f.ops.Reserve(ctx, tx, forged) })
				if !errors.Is(err, crud.ErrConflict) {
					t.Errorf("rebind = %v", err)
				}
			})
			t.Run("stale terminal and clock refusals return nothing", func(t *testing.T) {
				f := operationFixtureFor(t, sql)
				_, _, r := f.reserve(t, "terminal")
				if err := f.run(f.ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					_, _, err := f.ops.Transition(ctx, tx, r.Operation(), 1, request.Succeeded, start.Add(time.Second))
					return err
				}); err != nil {
					t.Fatal(err)
				}
				for _, in := range []struct {
					revision int64
					outcome  request.Outcome
					at       time.Time
				}{
					{1, request.Succeeded, start.Add(time.Second)}, {2, request.Failed, start.Add(time.Second)}, {2, request.Succeeded, time.Time{}}, {2, request.Succeeded, start.Add(-time.Second)},
				} {
					err := f.run(f.ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
						got, changed, err := f.ops.Transition(ctx, tx, r.Operation(), in.revision, in.outcome, in.at)
						if got.Ref != uuid.Nil || changed {
							t.Errorf("refusal returned %+v, changed %v", got, changed)
						}
						return err
					})
					if err == nil {
						t.Error("invalid transition accepted")
					}
				}
				if err := f.run(f.ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					got, changed, err := f.ops.Transition(ctx, tx, r.Operation(), 2, request.Succeeded, start.Add(time.Second))
					if changed || got.Revision != 2 {
						t.Errorf("no-op = %+v changed %v", got, changed)
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
			})
			t.Run("conflicting completions have one winner", func(t *testing.T) {
				f := operationFixtureFor(t, sql)
				_, _, r := f.reserve(t, "race")
				var wg sync.WaitGroup
				results := make(chan error, 2)
				for _, outcome := range []request.Outcome{request.Succeeded, request.Failed} {
					wg.Go(func() {
						results <- f.run(f.ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
							_, _, err := f.ops.Transition(ctx, tx, r.Operation(), 1, outcome, start)
							return err
						})
					})
				}
				wg.Wait()
				close(results)
				wins, refusals := 0, 0
				for err := range results {
					if err == nil {
						wins++
					} else if errors.Is(err, crud.ErrConflict) {
						refusals++
					} else {
						t.Error(err)
					}
				}
				if wins != 1 || refusals != 1 {
					t.Errorf("wins %d refusals %d", wins, refusals)
				}
			})
		})
	}
}

func TestPurgeRetainsUnknownAndPendingAndRetiresTerminalPairs(t *testing.T) {
	f := operationFixtureFor(t, true)
	conn, _ := httpx.ConnFrom(f.ctx)
	for _, outcome := range []request.Outcome{"", request.PendingApproval, request.Succeeded, request.Failed} {
		s, v, r := f.reserve(t, "purge."+string(outcome))
		if err := f.store.Begin(f.ctx, s, v.Claim, start); err != nil {
			t.Fatal(err)
		}
		if outcome != "" {
			if err := f.run(f.ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, _, err := f.ops.Transition(ctx, tx, r.Operation(), 1, outcome, start)
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := idempotency.PurgeAt(f.ctx, conn, start.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []request.Outcome{"", request.PendingApproval, request.Succeeded, request.Failed} {
		s := scope("purge." + string(outcome))
		s.Track = true
		v, err := f.store.Claim(f.ctx, s, start.Add(48*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		want := idempotency.First
		if outcome == "" || outcome == request.PendingApproval {
			want = idempotency.Spent
		}
		if v.Kind != want {
			t.Errorf("purged %q = %+v", outcome, v)
		}
	}
	if err := idempotency.PurgeAt(f.ctx, conn, start.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
}
