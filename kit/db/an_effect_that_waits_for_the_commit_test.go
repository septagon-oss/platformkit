package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestAnEffectThatMayOnlyHappenOnceTheRowsCommit is the kernel half of what
// modules/auth needs to mail a link it must not store: an effect that cannot be a
// row — the outbox keeps a payload for a week and the audit trail copies every one
// — and that must not be observable while the rows that authorise it are still
// uncommitted. What is worth proving here is the ordering and nothing else: an
// action that ran before the commit would see no row, and one that runs after a
// rollback is an effect the application has already forgotten.
func TestAnEffectThatMayOnlyHappenOnceTheRowsCommit(t *testing.T) {
	ctx := t.Context()
	admin, app := dbtest.Schema(t)
	createThings(t, ctx, admin)

	t.Run("runs after the commit, in the order it was registered", func(t *testing.T) {
		acme := newTenant("acme")
		var order []string
		err := db.Run(tenancy.WithTenant(ctx, acme), app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := insert(tx.DB(), acme.ID, "kept"); err != nil {
				return err
			}
			if err := db.AfterCommit(ctx, func(context.Context) error {
				order = append(order, "second")
				return nil
			}); err != nil {
				return err
			}
			if err := db.AfterCommit(ctx, func(context.Context) error {
				order = append(order, "third")
				return nil
			}); err != nil {
				return err
			}
			order = append(order, "first")
			return nil
		})
		if err != nil {
			t.Fatalf("commit: %v", err)
		}
		if got := strings.Join(order, ","); got != "first,second,third" {
			t.Errorf("order = %q, want the transaction, then each action as it was registered", got)
		}
	})

	t.Run("the commit is over: what it reads is what landed", func(t *testing.T) {
		acme := newTenant("committed")
		ctxAcme := tenancy.WithTenant(ctx, acme)
		var (
			inside, after int
			ran           bool
		)
		err := db.Run(ctxAcme, app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := insert(tx.DB(), acme.ID, "committed-while-it-waits"); err != nil {
				return err
			}
			if err := db.AfterCommit(ctx, func(ctx context.Context) error {
				ran = true
				// No transaction is carried to the action, so this opens its own and
				// can only see what the commit above already made visible.
				return db.Run(ctx, app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
					after = count(t, tx.DB())
					return nil
				})
			}); err != nil {
				return err
			}
			inside = count(t, tx.DB())
			return nil
		})
		if err != nil {
			t.Fatalf("commit: %v", err)
		}
		if !ran {
			t.Fatal("the action never ran")
		}
		if inside != 1 || after != 1 {
			t.Errorf("rows inside the transaction %d, read after its commit %d, want one and one", inside, after)
		}
	})

	t.Run("a rollback runs nothing", func(t *testing.T) {
		acme := newTenant("rolled-back")
		ctxAcme := tenancy.WithTenant(ctx, acme)
		var ran bool
		boom := errors.New("the handler refused")
		err := db.Run(ctxAcme, app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := insert(tx.DB(), acme.ID, "rolled-back"); err != nil {
				return err
			}
			if err := db.AfterCommit(ctx, func(context.Context) error {
				ran = true
				return nil
			}); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("rollback = %v, want %v", err, boom)
		}
		if ran {
			t.Error("a rolled-back transaction ran the effect it had deferred")
		}
		if n := countAs(t, ctxAcme, app); n != 0 {
			t.Errorf("rows after the rollback = %d, want none", n)
		}
	})

	t.Run("an effect that fails says so over a commit that did", func(t *testing.T) {
		acme := newTenant("in-but-not-out")
		ctxAcme := tenancy.WithTenant(ctx, acme)
		boom := errors.New("the mail server refused")
		err := db.Run(ctxAcme, app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := insert(tx.DB(), acme.ID, "in-but-not-out"); err != nil {
				return err
			}
			return db.AfterCommit(ctx, func(context.Context) error { return boom })
		})
		if !errors.Is(err, boom) {
			t.Errorf("Run = %v, want it to carry %v: the rows are in and the effect is not", err, boom)
		}
		if n := countAs(t, ctxAcme, app); n != 1 {
			t.Errorf("rows after a failed action = %d, want the commit to stand", n)
		}
	})

	t.Run("nothing defers an effect outside a tenant transaction", func(t *testing.T) {
		ctxAcme := tenancy.WithTenant(ctx, newTenant("nowhere"))
		refuses := func(context.Context) error { return nil }
		if err := db.AfterCommit(ctxAcme, refuses); !errors.Is(err, db.ErrNoTransactionToDefer) {
			t.Errorf("no transaction at all = %v, want %v", err, db.ErrNoTransactionToDefer)
		}
		token := syscap.NewSystemToken("kit/db test: an effect nobody may defer across tenants")
		err := db.RunSystem(ctx, app, token, func(ctx context.Context, tx db.Tx[db.System]) error {
			return db.AfterCommit(ctx, refuses)
		})
		if !errors.Is(err, db.ErrNoTransactionToDefer) {
			t.Errorf("a system transaction = %v, want %v", err, db.ErrNoTransactionToDefer)
		}
	})
}
