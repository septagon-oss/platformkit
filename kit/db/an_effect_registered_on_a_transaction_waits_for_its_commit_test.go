package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// An effect that cannot be a row and must not be observable before its rows are
// (a one-time link modules/auth mails) is registered on the transaction that makes
// it spendable and runs on that transaction's commit — never before it, never after
// a rollback.

func TestAnEffectRunsOnTheCommitAndSeesWhatItCommitted(t *testing.T) {
	admin, app, acme := acmeThings(t)
	var ran, rowsDuring int
	var saw int
	err := db.Run(tenancy.WithTenant(t.Context(), acme), app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := insert(tx.DB(), acme.ID, "the-row"); err != nil {
			return err
		}
		if err := db.AfterCommit(ctx, func(ctx context.Context) error {
			ran++
			// The row is committed and unreachable from here, so the only way to read
			// it is the way any other transaction reads it: one of its own.
			return db.Run(ctx, app, func(_ context.Context, again db.Tx[db.Tenant]) error {
				return again.DB().Raw("SELECT count(*) FROM things").Scan(&saw).Error
			})
		}); err != nil {
			return err
		}
		rowsDuring = committed(t, admin)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if ran != 1 {
		t.Fatalf("the effect ran %d times, want once on the commit", ran)
	}
	if rowsDuring != 0 {
		t.Errorf("the row was committed %d rows deep while its transaction was still open", rowsDuring)
	}
	if saw != 1 {
		t.Errorf("the effect read %d rows after the commit, want the one it waited for", saw)
	}
}

func TestNoEffectRunsOnARollbackAFailedCommitOrAnEndedOwner(t *testing.T) {
	_, app, acme := acmeThings(t)
	own := errors.New("the work refused")
	cases := map[string]func(t *testing.T, register func(context.Context) error) error{
		"the handler refused": func(t *testing.T, register func(context.Context) error) error {
			return db.Run(tenancy.WithTenant(t.Context(), acme), app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
				if err := register(ctx); err != nil {
					return err
				}
				return own
			})
		},
		"a lazy owner ended first": func(t *testing.T, register func(context.Context) error) error {
			ctx, p, cancel := lazy(t, app, acme)
			tx, err := p.Tx(ctx)
			if err != nil {
				return err
			}
			if err := register(ctx); err != nil {
				return err
			}
			_ = tx.DB().Exec("SELECT 1").Error
			cancel()
			return p.Close(true)
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			var ran bool
			err := run(t, func(ctx context.Context) error {
				return db.AfterCommit(ctx, func(context.Context) error { ran = true; return nil })
			})
			if ran {
				t.Error("the effect ran although its transaction never committed")
			}
			if errors.Is(err, db.ErrEffectNotRun) {
				t.Errorf("a transaction that did not commit is reported as %v, not as its own refusal", err)
			}
			if !errors.Is(err, own) && name != "a lazy owner ended first" && !errors.Is(err, context.Canceled) {
				t.Errorf("refusal = %v, want the caller's own error", err)
			}
		})
	}
}

func TestANestedFrameDefersToTheCommitAndNotToItsOwnCall(t *testing.T) {
	_, app, acme := acmeThings(t)
	ctx := tenancy.WithTenant(t.Context(), acme)
	var ran int
	err := db.Run(ctx, app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		err := db.Run(ctx, app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
			return db.AfterCommit(ctx, func(context.Context) error { ran++; return nil })
		})
		if err != nil {
			return err
		}
		if ran != 0 {
			t.Errorf("the effect ran at the nested frame's return, before anything committed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if ran != 1 {
		t.Errorf("the effect ran %d times, want once under the one commit", ran)
	}
}

func TestEveryEffectRunsAfterOneFailedAndTheirFailuresComeBackTogether(t *testing.T) {
	_, app, acme := acmeThings(t)
	refused := errors.New("the transport refused")
	var second bool
	err := db.Run(tenancy.WithTenant(t.Context(), acme), app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
		if err := db.AfterCommit(ctx, func(context.Context) error { return refused }); err != nil {
			return err
		}
		return db.AfterCommit(ctx, func(context.Context) error { second = true; return nil })
	})
	if !second {
		t.Error("an effect behind a failed one did not run: the two were registered by different work")
	}
	if !errors.Is(err, db.ErrEffectNotRun) {
		t.Fatalf("a commit that stood over an effect that did not run is reported as %v", err)
	}
	if !errors.Is(err, refused) || !strings.Contains(err.Error(), "1 of its 2") {
		t.Errorf("the failures that came back are %v, want the transport's words and both counts", err)
	}
}

func TestClosingTheSameTransactionTwiceRunsEachEffectOnce(t *testing.T) {
	_, app, acme := acmeThings(t)
	ctx, p, _ := lazy(t, app, acme)
	if _, err := p.Tx(ctx); err != nil {
		t.Fatal(err)
	}
	var ran int
	if err := db.AfterCommit(ctx, func(context.Context) error { ran++; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(true); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(true); err != nil {
		t.Fatal(err)
	}
	if ran != 1 {
		t.Errorf("the effect ran %d times for one commit", ran)
	}
}

func TestNothingIsDeferredOverNoTransactionOrASystemOne(t *testing.T) {
	_, app, acme := acmeThings(t)
	nop := func(context.Context) error { return nil }
	if err := db.AfterCommit(tenancy.WithTenant(t.Context(), acme), nop); !errors.Is(err, db.ErrNoTransactionToDefer) {
		t.Errorf("outside any transaction: %v", err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
		return db.AfterCommit(ctx, nil)
	}); !errors.Is(err, db.ErrNoTransactionToDefer) {
		t.Errorf("a nil action: %v", err)
	}
	err := db.RunSystem(t.Context(), app, dbtest.SystemToken(), func(ctx context.Context, _ db.Tx[db.System]) error {
		return db.AfterCommit(ctx, nop)
	})
	if !errors.Is(err, db.ErrNoTransactionToDefer) {
		t.Errorf("a system transaction deferred an effect: %v", err)
	}
}
