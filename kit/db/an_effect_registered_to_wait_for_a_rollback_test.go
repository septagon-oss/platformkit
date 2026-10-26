package db_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// An effect can exist because the work it stood beside was discarded: the bytes no committed
// row will ever name. Only an ending the database confirmed runs them — a COMMIT whose answer
// never came may have committed, so nothing runs then.

// mustDefer registers one effect on the transaction ctx is inside, failing the test if the
// registration is refused. ask is db.AfterCommit or db.AfterRollback.
func mustDefer(t *testing.T, ctx context.Context, ask func(context.Context, func(context.Context) error) error, fn func(context.Context) error) {
	t.Helper()
	if err := ask(ctx, fn); err != nil {
		t.Fatal(err)
	}
}

func TestARolledBackTransactionRunsTheEffectsWaitingForTheRollback(t *testing.T) {
	_, app, acme := acmeThings(t)
	ctx, p, _ := lazy(t, app, acme)
	insertThrough(t, ctx, p, acme, "gone")
	var ran []string
	mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { ran = append(ran, "first"); return nil })
	mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { ran = append(ran, "second"); return nil })
	mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { ran = append(ran, "commit"); return nil })
	if err := p.Rollback(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ran, ",") != "first,second" {
		t.Errorf("the effects that ran are %v, want the two waiting for the rollback, in the order they registered", ran)
	}
}

func TestEndingTheSameTransactionTwiceRunsEachRollbackEffectOnce(t *testing.T) {
	_, app, acme := acmeThings(t)
	ctx, p, _ := lazy(t, app, acme)
	insertThrough(t, ctx, p, acme, "once")
	var ran int
	mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { ran++; return nil })
	for range 3 {
		if err := p.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
	if ran != 1 {
		t.Errorf("the effect ran %d times for one rolled back transaction, want once", ran)
	}
}

func TestACommitRunsTheEffectsWaitingForItAndNoneWaitingForARollback(t *testing.T) {
	_, app, acme := acmeThings(t)
	var waited, dropped int
	err := db.Run(tenancy.WithTenant(t.Context(), acme), app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
		mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { dropped++; return nil })
		mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { waited++; return nil })
		return nil
	})
	if err != nil || waited != 1 || dropped != 0 {
		t.Errorf("the transaction answered %v and ran %d waiting for the commit and %d for the rollback; want no error, 1 and 0", err, waited, dropped)
	}
}

func TestAPanicInTheTransactionRunsTheEffectsWaitingForTheRollback(t *testing.T) {
	_, app, acme := acmeThings(t)
	var ran int
	func() {
		defer func() {
			if r := recover(); r != "the work fell over" {
				t.Errorf("the panic came back as %v, want the handler's own value", r)
			}
		}()
		_ = db.Run(tenancy.WithTenant(t.Context(), acme), app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
			mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { ran++; return nil })
			panic("the work fell over")
		})
	}()
	if ran != 1 {
		t.Errorf("the effect ran %d times, want once on the rollback the panic forced", ran)
	}
}

// Neither queue runs when the COMMIT itself is refused: PostgreSQL may have committed what it
// then wrote off. The device is a deferrable unique key the transaction violates twice, so the
// refusal can only arrive when the constraint is checked at commit.
func TestNeitherQueueRunsWhenTheCommitItselfFails(t *testing.T) {
	admin, app, acme := acmeThings(t)
	exec(t, t.Context(), admin, `CREATE TABLE marks (tenant_id uuid, name text, UNIQUE (tenant_id, name) DEFERRABLE INITIALLY DEFERRED)`)
	var ran int
	err := db.Run(tenancy.WithTenant(t.Context(), acme), app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, twice := range []string{"twice", "twice"} {
			if err := tx.DB().Exec("INSERT INTO marks VALUES (?, ?)", acme.ID, twice).Error; err != nil {
				return err
			}
		}
		mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { ran++; return nil })
		mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { ran += 100; return nil })
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "SQLSTATE 23505") {
		t.Fatalf("the commit answered %v, want the deferred key's refusal", err)
	}
	if ran != 0 {
		t.Errorf("%d effects ran although the COMMIT's outcome is unknown", ran)
	}
	if errors.Is(err, db.ErrEffectNotRun) {
		t.Errorf("an unanswered COMMIT is reported as %v, which is the promise a commit keeps", err)
	}
}

// A commit the settings re-read refuses is rolled back, and the database confirmed that, so
// the work waiting for a rollback runs and the work waiting for the commit does not.
func TestARolledBackCommitRunsTheWorkWaitingForTheRollback(t *testing.T) {
	_, app, acme := acmeThings(t)
	var ran int
	err := db.Run(tenancy.WithTenant(t.Context(), acme), app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Exec("SELECT set_config('platformkit.system_access', 'true', true)").Error; err != nil {
			return err
		}
		mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { ran++; return nil })
		return nil
	})
	if !errors.Is(err, db.ErrScopeTampered) || ran != 1 {
		t.Errorf("the tampered transaction answered %v and ran %d of its effects, want ErrScopeTampered and the rollback's work alone", err, ran)
	}
}

func TestAReopenedTransactionRunsNoneOfTheEffectsTheRolledBackOneCarried(t *testing.T) {
	admin, app, acme := acmeThings(t)
	ctx, p, _ := lazy(t, app, acme)
	t.Cleanup(func() { _ = p.Rollback() })
	insertThrough(t, ctx, p, acme, "rolled back")
	var rolledBack, owedByNew, waited int
	mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { rolledBack++; return nil })
	mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { waited++; return nil })
	if err := p.Rollback(); err != nil {
		t.Fatal(err)
	}
	// A live request can open another transaction through the same Pending.
	insertThrough(t, ctx, p, acme, "committed")
	mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { owedByNew++; return nil })
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := committed(t, admin); n != 1 || rolledBack != 1 || owedByNew != 0 || waited != 0 {
		t.Errorf("committed rows = %d and effects that ran: the rolled back transaction's = %d, the new one's rollback = %d, what waited for its commit = %d; want 1, 1, 0 and 0",
			n, rolledBack, owedByNew, waited)
	}
}

func TestANestedFrameRegistersItsRollbackEffectOnTheOuterTransaction(t *testing.T) {
	_, app, acme := acmeThings(t)
	var ran int
	own := errors.New("the outer work refused")
	err := db.Run(tenancy.WithTenant(t.Context(), acme), app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
		err := db.Run(ctx, app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
			return db.AfterRollback(ctx, func(context.Context) error { ran++; return nil })
		})
		if err != nil {
			return err
		}
		if ran != 0 {
			t.Errorf("the effect ran at the nested frame's return, before anything was settled")
		}
		return own
	})
	if !errors.Is(err, own) || ran != 1 {
		t.Errorf("the outer transaction answered %v and ran %d effects, want its own refusal and one effect at its rollback", err, ran)
	}
}

func TestNothingIsDeferredToARollbackOverNoTransactionOrASystemOne(t *testing.T) {
	_, app, acme := acmeThings(t)
	nop := func(context.Context) error { return nil }
	tenant := tenancy.WithTenant(t.Context(), acme)
	asked := map[string]func() error{
		"outside any transaction": func() error { return db.AfterRollback(tenant, nop) },
		"a nil action": func() error {
			return db.Run(tenant, app, func(ctx context.Context, _ db.Tx[db.Tenant]) error { return db.AfterRollback(ctx, nil) })
		},
		"a system transaction": func() error {
			return db.RunSystem(t.Context(), app, dbtest.SystemToken(), func(ctx context.Context, _ db.Tx[db.System]) error {
				return db.AfterRollback(ctx, nop)
			})
		},
		"a request whose transaction nobody opened": func() error {
			idle, _, _ := lazy(t, app, acme)
			return db.AfterRollback(idle, nop)
		},
		"a transaction that already ended": func() error {
			ctx, p, _ := lazy(t, app, acme)
			insertThrough(t, ctx, p, acme, "ended")
			if err := p.Rollback(); err != nil {
				t.Fatal(err)
			}
			return db.AfterRollback(ctx, nop)
		},
	}
	for name, ask := range asked {
		if err := ask(); !errors.Is(err, db.ErrNoTransactionToDefer) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// An owner that ended first is a confirmed rollback: database/sql had already discarded the
// transaction by the time our own ROLLBACK was refused as already done, and no COMMIT can be
// acknowledged from here. The effect is handed a context that outlives the owner so it can
// still reach the pool, another tenant's committed row proves the read ran under its own
// scope, and the caller's answer stays the owner's cause.
func TestAnOwnerThatEndedFirstRunsTheRollbackWorkOnAContextThatStillReachesThePool(t *testing.T) {
	admin, app, acme := acmeThings(t)
	other := newTenant("other")
	exec(t, t.Context(), admin, "INSERT INTO things (tenant_id, name) VALUES ('"+other.ID.String()+"', 'theirs')")
	base := app.Stats().InUse
	ctx, p, cancel := lazy(t, app, acme)
	insertThrough(t, ctx, p, acme, "hung up")
	var ran, rows int
	var inside error
	mustDefer(t, ctx, db.AfterRollback, func(ctx context.Context) error {
		ran++
		conn, ok := db.ConnFrom(ctx)
		if !ok {
			inside = errors.New("the effect was handed no pool to reach")
		} else {
			inside = db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				return tx.DB().Raw("SELECT count(*) FROM things").Scan(&rows).Error
			})
		}
		return inside
	})
	cancel()
	waitForPoolStat(t, "the hung-up transaction's connection back to the pool", func() bool {
		return app.Stats().InUse == base
	})
	err := p.Rollback()
	if ran != 1 || inside != nil {
		t.Fatalf("the effect reached the database %d times with %v, want once and no error", ran, inside)
	}
	if n := committed(t, admin); rows != 0 || n != 1 {
		t.Errorf("the effect read %d rows and %d are committed: want none read, its own rolled back and another tenant's refused, and only that tenant's row committed", rows, n)
	}
	if !errors.Is(err, context.Canceled) || errors.Is(err, db.ErrEffectNotRun) {
		t.Errorf("Rollback answered %v, want the owner's cause and not the promise a commit keeps", err)
	}
}

func TestARollbackEffectThatFailsIsLoggedAndChangesNothingTheCallerIsTold(t *testing.T) {
	_, app, acme := acmeThings(t)
	ctx, p, _ := lazy(t, app, acme)
	insertThrough(t, ctx, p, acme, "rolled back")
	refused := errors.New("the store refused")
	mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { return refused })
	var logs strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	if err := p.Rollback(); err != nil {
		t.Errorf("Rollback answered %v, want nil: an action's failure is not the caller's answer", err)
	}
	if out := logs.String(); !strings.Contains(out, "deferred to after the rollback failed") || !strings.Contains(out, refused.Error()) {
		t.Errorf("the log does not name the failed effect and its own error: %s", out)
	}
}

// Registration racing the ending is honest either way — the action is in the queue when the
// ending takes it, or it never runs — but it never runs twice.
func TestAnEffectRegisteredAsTheTransactionEndsRunsAtMostOnce(t *testing.T) {
	_, app, acme := acmeThings(t)
	for range 30 {
		ctx, p, _ := lazy(t, app, acme)
		if _, err := p.Tx(ctx); err != nil {
			t.Fatal(err)
		}
		var ran atomic.Int32
		done := make(chan struct{})
		go func() {
			defer close(done)
			for range 20 {
				_ = db.AfterRollback(ctx, func(context.Context) error { ran.Add(1); return nil })
			}
		}()
		_ = p.Rollback()
		_ = p.Commit()
		<-done
		if n := ran.Load(); n > 1 {
			t.Fatalf("one rolled back transaction ran its effect %d times, want at most once", n)
		}
	}
}
