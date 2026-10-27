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

// A registration racing the ending is honest on both sides of it: whatever the ending let in
// runs, and runs once, and whatever arrives after it was taken is refused. The count of accepted
// registrations is therefore the count of runs — a promise about an ending the database answered,
// so that answer is read first: leave the outcome unknown and running nothing is correct, and the
// count would accuse an ending that did nothing wrong.
func TestAnEffectRegisteredAsTheTransactionEndsRunsAtMostOnce(t *testing.T) {
	_, app, acme := acmeThings(t)
	for range 30 {
		ctx, p, _ := lazy(t, app, acme)
		if _, err := p.Tx(ctx); err != nil {
			t.Fatal(err)
		}
		var ran, accepted atomic.Int32
		done := make(chan struct{})
		go func() {
			defer close(done)
			for range 20 {
				if db.AfterRollback(ctx, func(context.Context) error { ran.Add(1); return nil }) == nil {
					accepted.Add(1)
				}
			}
		}()
		rollbackErr := p.Rollback()
		commitErr := p.Commit()
		<-done
		if rollbackErr != nil {
			t.Fatalf("the ending refused the ROLLBACK this test asked for: %v", rollbackErr)
		}
		if commitErr != nil {
			t.Fatalf("the ending after the rollback answered %v, want the no-op of a transaction already ended", commitErr)
		}
		if n, ok := ran.Load(), accepted.Load(); n != ok {
			t.Fatalf("one rolled back transaction ran %d of the %d effects it accepted, want every one of them once", n, ok)
		}
	}
}

// endingLogDebug runs fn with the default logger writing to a buffer that holds debug lines,
// and answers what that buffer holds. Debug, because the drop a confirmed ending promises is
// written there: a line about work the ending never held has to be visible here to be refused.
func endingLogDebug(t *testing.T, fn func()) string {
	t.Helper()
	var logs strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	fn()
	return logs.String()
}

// Work left behind by an ending is dropped on purpose — each outcome runs one queue and drops
// the other — but the work is memory and its transaction is over, so what vanished, and the
// ending that did it, are written down, at the level the ending's own answer decides. A
// confirmed ending drops the queue that never waited for it, which is what every ordinary
// commit and rollback does; an outcome the database left in doubt drops both, and its work is
// gone behind an answer that may never say so. An ending that dropped nothing writes nothing.
func TestAnEffectTheEndingDroppedIsNamedInTheLog(t *testing.T) {
	admin, app, acme := acmeThings(t)
	const message = "ended without the effects deferred to it"
	for _, shape := range []struct {
		name  string
		drive func()
		want  []string
	}{
		{
			name: "a confirmed rollback that dropped the work waiting for its commit",
			want: []string{"level=DEBUG", "effects=1", "committed=false", "rolled_back=true"},
			drive: func() {
				ctx, p, _ := lazy(t, app, acme)
				insertThrough(t, ctx, p, acme, "rolled back")
				mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { return nil })
				if err := p.Rollback(); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "a confirmed rollback with no work waiting at all",
			drive: func() {
				ctx, p, _ := lazy(t, app, acme)
				insertThrough(t, ctx, p, acme, "rolled back")
				if err := p.Rollback(); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "a COMMIT whose answer never came",
			want: []string{"level=WARN", "effects=1", "committed=false", "rolled_back=false"},
			drive: func() {
				exec(t, t.Context(), admin, `CREATE TABLE marks (tenant_id uuid, name text, UNIQUE (tenant_id, name) DEFERRABLE INITIALLY DEFERRED)`)
				err := db.Run(tenancy.WithTenant(t.Context(), acme), app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					for _, twice := range []string{"twice", "twice"} {
						if err := tx.DB().Exec("INSERT INTO marks VALUES (?, ?)", acme.ID, twice).Error; err != nil {
							return err
						}
					}
					mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { return nil })
					return nil
				})
				if err == nil || !strings.Contains(err.Error(), "SQLSTATE 23505") {
					t.Fatalf("the commit answered %v, want the deferred key's refusal", err)
				}
			},
		},
	} {
		out := endingLogDebug(t, shape.drive)
		written, wantLines := strings.Count(out, message), 0
		if len(shape.want) > 0 {
			wantLines = 1
		}
		if written != wantLines {
			t.Errorf("%s: %d line(s) about the effects it dropped, want %d: %s", shape.name, written, wantLines, out)
			continue
		}
		for _, attr := range shape.want {
			if !strings.Contains(out, attr) {
				t.Errorf("%s: the line about the dropped effects does not say %s: %s", shape.name, attr, out)
			}
		}
	}
}

// A Run handler's context names the transaction it was handed by value, queue pointers
// and all, so a registration made once Run has returned reaches a queue that ending took.
// It is refused: the caller is told the work will not run, rather than being answered nil
// for work no ending can reach. A frame held that long is nothing a running application
// does, which is why the promise needs a case of its own.
func TestARegistrationOnTheFrameRunHandedItsHandlerIsRefusedOnceThatFrameEnded(t *testing.T) {
	_, app, acme := acmeThings(t)
	refused := errors.New("the work was refused")
	for _, keep := range []bool{true, false} {
		ending := "a rollback"
		if keep {
			ending = "a commit"
		}
		var ran int
		var held context.Context
		err := db.Run(tenancy.WithTenant(t.Context(), acme), app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
			held = ctx
			if keep {
				return nil
			}
			return refused
		})
		if keep && err != nil {
			t.Fatalf("the ending that was to commit answered %v, want nil", err)
		}
		if !keep && !errors.Is(err, refused) {
			t.Fatalf("the ending that was to roll back answered %v, want its own refusal", err)
		}
		for name, ask := range map[string]func(context.Context, func(context.Context) error) error{
			"AfterRollback": db.AfterRollback,
			"AfterCommit":   db.AfterCommit,
		} {
			if err := ask(held, func(context.Context) error { ran++; return nil }); !errors.Is(err, db.ErrNoTransactionToDefer) {
				t.Errorf("%s on a frame ended by %s answered %v, want %v", name, ending, err, db.ErrNoTransactionToDefer)
			}
		}
		if ran != 0 {
			t.Errorf("%d effects registered after %s ran", ran, ending)
		}
	}
}
