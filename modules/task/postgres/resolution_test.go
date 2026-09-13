package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/internal"
	"github.com/septagon-oss/platformkit/modules/task/postgres"
	"github.com/septagon-oss/platformkit/modules/task/resolution"
)

type policyFunc func(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error)

func (f policyFunc) Decide(ctx context.Context, r tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	return f(ctx, r)
}
func allow(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	return tenancy.PolicyDecision{Allowed: true}, nil
}

func seed(t *testing.T, conn *db.Conn, tenant tenancy.Tenant) *contracts.Task {
	t.Helper()
	row := &contracts.Task{Title: "Portable resolution", Priority: contracts.PriorityHigh}
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return crud.Create(ctx, tx, row) }); err != nil {
		t.Fatal(err)
	}
	return row
}

func resolver(t *testing.T, conn *db.Conn, policy tenancy.Policy) *resolution.Service {
	t.Helper()
	store, err := postgres.NewResolutionStore(conn)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := resolution.New(store, policy, db.Now)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func assertUnresolved(t *testing.T, admin *sql.DB, id uuid.UUID) {
	t.Helper()
	var status string
	var count int
	if err := admin.QueryRowContext(t.Context(), "SELECT status FROM tasks WHERE id=$1", id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE payload->>'taskId'=$1", id.String()).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if status != contracts.StatusOpen || count != 0 {
		t.Fatalf("task=%s, events=%d after refusal", status, count)
	}
}

func TestStandaloneResolutionBindsExplicitActorAndClearsInheritedAttribution(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New()}
	user := uuid.New()
	for _, actor := range []tenancy.PolicyActor{
		{Kind: tenancy.PolicyUser, ID: user.String()}, {Kind: tenancy.PolicySystem, ID: "maintenance"}, {Kind: tenancy.PolicyPublic},
	} {
		row := seed(t, conn, tenant)
		ctx := tenancy.WithActor(t.Context(), uuid.New())
		in := resolution.Command{Tenant: tenant, Actor: actor, TaskID: row.ID, Resolution: " fixed "}
		got, err := resolver(t, conn, policyFunc(allow)).Resolve(ctx, in)
		if err != nil || !got.Changed || got.ResolvedAt == nil {
			t.Fatalf("Resolve = %+v, %v", got, err)
		}
		var saved, eventAt time.Time
		var attributed sql.NullString
		if err := admin.QueryRowContext(ctx, "SELECT resolved_at FROM tasks WHERE id=$1", row.ID).Scan(&saved); err != nil {
			t.Fatal(err)
		}
		if err := admin.QueryRowContext(ctx, "SELECT actor::text, (payload->>'at')::timestamptz FROM platformkit_outbox WHERE name='task.resolved' AND payload->>'taskId'=$1", row.ID.String()).Scan(&attributed, &eventAt); err != nil {
			t.Fatal(err)
		}
		if !saved.Equal(*got.ResolvedAt) || !eventAt.Equal(saved) {
			t.Fatal("returned, stored and event times differ")
		}
		if actor.Kind == tenancy.PolicyUser {
			if !attributed.Valid || attributed.String != user.String() {
				t.Fatalf("actor = %+v", attributed)
			}
		} else if attributed.Valid {
			t.Fatalf("non-user inherited actor %s", attributed.String)
		}
		*got.ResolvedAt = time.Time{}
		again, err := resolver(t, conn, policyFunc(allow)).Resolve(ctx, in)
		if err != nil || again.Changed || again.ResolvedAt == nil || !again.ResolvedAt.Equal(saved) {
			t.Fatalf("retry = %+v, %v", again, err)
		}
	}
	for _, actor := range []tenancy.PolicyActor{{Kind: tenancy.PolicyUser, ID: "opaque-user"}, {Kind: tenancy.PolicyUser, ID: uuid.Nil.String()}, {Kind: tenancy.PolicySystem}, {Kind: tenancy.PolicyPublic, ID: "impersonated"}} {
		row := seed(t, conn, tenant)
		_, err := resolver(t, conn, policyFunc(allow)).Resolve(t.Context(), resolution.Command{Tenant: tenant, TaskID: row.ID, Actor: actor})
		if !errors.Is(err, tenancy.ErrInvalidPolicyRequest) {
			t.Fatalf("invalid actor = %v", err)
		}
		assertUnresolved(t, admin, row.ID)
	}
}

func TestStandaloneResolutionKeepsTenantPolicyAndOutboxFailuresAtomic(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New()}
	row := seed(t, conn, tenant)
	in := resolution.Command{Tenant: tenant, TaskID: row.ID, Actor: tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: uuid.NewString()}, Resolution: "fixed"}
	deny := policyFunc(func(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
		return tenancy.PolicyDecision{}, nil
	})
	if _, err := resolver(t, conn, deny).Resolve(t.Context(), in); !errors.Is(err, tenancy.ErrPolicyDenied) {
		t.Fatal(err)
	}
	assertUnresolved(t, admin, row.ID)
	foreign := in
	foreign.Tenant.ID = uuid.New()
	if _, err := resolver(t, conn, policyFunc(allow)).Resolve(t.Context(), foreign); !errors.Is(err, resolution.ErrNotFound) {
		t.Fatal(err)
	}
	assertUnresolved(t, admin, row.ID)
	deleted := seed(t, conn, tenant)
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return crud.Delete[*contracts.Task](tx, deleted.ID, true)
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{deleted.ID, uuid.New()} {
		missing := in
		missing.TaskID = id
		if _, err := resolver(t, conn, policyFunc(allow)).Resolve(t.Context(), missing); !errors.Is(err, resolution.ErrNotFound) {
			t.Fatalf("deleted/absent task returned %v", err)
		}
	}
	if _, err := admin.ExecContext(t.Context(), "ALTER TABLE platformkit_outbox ADD CONSTRAINT refuse_resolution CHECK (name <> 'task.resolved')"); err != nil {
		t.Fatal(err)
	}
	if got, err := resolver(t, conn, policyFunc(allow)).Resolve(t.Context(), in); err == nil || got.Changed || got.TaskID != uuid.Nil {
		t.Fatalf("outbox failure = %+v, %v", got, err)
	}
	assertUnresolved(t, admin, row.ID)
}

func TestCallerBoundResolutionRollsBackWithLaterWorkAndPreservesLegacyActor(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New()}
	row := seed(t, conn, tenant)
	actor := tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: uuid.NewString()}
	failure := errors.New("later product check refused")
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		locked, err := postgres.LockResolution(ctx, tx, row.ID, actor)
		if err != nil {
			return err
		}
		// The host's policy check is explicit and occurs after this lock.
		facts := locked.Facts()
		if _, err := tenancy.RequirePolicy(ctx, policyFunc(allow), tenancy.PolicyRequest{Tenant: tenant, Actor: actor, Action: "task:resolve", Resource: tenancy.PolicyResource{TenantID: facts.TenantID, Kind: "task", ID: facts.TaskID.String()}}); err != nil {
			return err
		}
		if _, err := resolution.StageAuthorized(ctx, locked, resolution.Command{Tenant: tenant, TaskID: row.ID, Resolution: "fixed"}, db.Now); err != nil {
			return err
		}
		if _, err := resolver(t, conn, policyFunc(allow)).Resolve(ctx, resolution.Command{Tenant: tenant, TaskID: row.ID, Actor: actor}); !errors.Is(err, db.ErrAmbientTransaction) {
			t.Fatalf("standalone resolver joined caller transaction: %v", err)
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	assertUnresolved(t, admin, row.ID)
	for _, hasActor := range []bool{false, true} {
		row := seed(t, conn, tenant)
		user := uuid.New()
		ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), tenant), tenancy.Principal{UserID: user})
		if hasActor {
			ctx = tenancy.WithActor(ctx, user)
		}
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := internal.NewService().Resolve(ctx, tx, row.ID, "legacy")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		var got sql.NullString
		if err := admin.QueryRowContext(ctx, "SELECT actor::text FROM platformkit_outbox WHERE payload->>'taskId'=$1", row.ID.String()).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got.Valid != hasActor || (hasActor && got.String != user.String()) {
			t.Fatalf("legacy actor changed: %+v", got)
		}
	}
}

func TestStandaloneResolutionWaitsForTheCallerTransaction(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[rollback], func(t *testing.T) {
			admin, conn := dbtest.Schema(t)
			tenant := tenancy.Tenant{ID: uuid.New()}
			row := seed(t, conn, tenant)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			locked := make(chan int, 1)
			firstDone, nextDone := make(chan error, 1), make(chan error, 1)
			var workers sync.WaitGroup
			defer func() { unblock(); cancel(); workers.Wait() }()
			abort := errors.New("outer rollback")
			workers.Go(func() {
				firstDone <- db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					if _, err := internal.NewService().Resolve(ctx, tx, row.ID, "fixed"); err != nil {
						return err
					}
					var pid int
					if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
						return err
					}
					locked <- pid
					select {
					case <-release:
						if rollback {
							return abort
						}
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			})
			var holder int
			select {
			case holder = <-locked:
			case err := <-firstDone:
				t.Fatalf("holder failed: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var got resolution.Result
			policy := policyFunc(func(_ context.Context, r tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
				want := contracts.StatusResolved
				if rollback {
					want = contracts.StatusOpen
				}
				if r.Resource.Attributes["status"] != want {
					t.Errorf("policy saw stale status: %+v", r.Resource)
				}
				return tenancy.PolicyDecision{Allowed: true}, nil
			})
			svc := resolver(t, conn, policy)
			workers.Go(func() {
				var err error
				got, err = svc.Resolve(ctx, resolution.Command{Tenant: tenant, TaskID: row.ID, Actor: tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: uuid.NewString()}, Resolution: "fixed"})
				nextDone <- err
			})
			for tick := time.Tick(10 * time.Millisecond); ; {
				var waiting bool
				if err := admin.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))", holder).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				select {
				case err := <-nextDone:
					t.Fatalf("contender did not wait: %v", err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-tick:
				}
			}
			unblock()
			if err := <-firstDone; (rollback && !errors.Is(err, abort)) || (!rollback && err != nil) {
				t.Fatal(err)
			}
			if err := <-nextDone; err != nil || got.Changed != rollback || got.Resolution != "fixed" {
				t.Fatalf("contender = %+v, %v", got, err)
			}
			var count int
			if err := admin.QueryRowContext(ctx, "SELECT count(*) FROM platformkit_outbox WHERE name='task.resolved'").Scan(&count); err != nil || count != 1 {
				t.Fatalf("events = %d, %v", count, err)
			}
		})
	}
}
