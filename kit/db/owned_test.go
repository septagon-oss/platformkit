package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestOwnedTransactionCommitsOnlyItsExplicitTenant(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	createThings(t, t.Context(), admin)
	first, other := newTenant("first"), newTenant("other")
	ctx := tenancy.WithTenant(t.Context(), other)
	if err := db.RunOwned(ctx, conn, first, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if db.TenantOf(tx) != first {
			t.Fatal("ambient tenant overrode the explicit tenant")
		}
		if err := db.RunOwned(ctx, conn, first, noTenantWork); !errors.Is(err, db.ErrAmbientTransaction) {
			t.Fatalf("nested ownership = %v", err)
		}
		return insert(tx.DB(), first.ID, "committed")
	}); err != nil {
		t.Fatal(err)
	}
	if got := countAs(t, tenancy.WithTenant(t.Context(), first), conn); got != 1 {
		t.Fatalf("committed rows = %d", got)
	}
	if got := countAs(t, ctx, conn); got != 0 {
		t.Fatalf("another tenant saw %d rows", got)
	}
	rollback := errors.New("later work failed")
	err := db.RunOwned(t.Context(), conn, first, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		if err := insert(tx.DB(), first.ID, "rolled back"); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) || countAs(t, tenancy.WithTenant(t.Context(), first), conn) != 1 {
		t.Fatalf("callback error did not roll back: %v", err)
	}
	func() {
		defer func() {
			if recover() != "later panic" {
				t.Error("owned runner did not rethrow the callback panic")
			}
		}()
		_ = db.RunOwned(t.Context(), conn, first, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			if err := insert(tx.DB(), first.ID, "panicked"); err != nil {
				t.Fatal(err)
			}
			panic("later panic")
		})
	}()
	if countAs(t, tenancy.WithTenant(t.Context(), first), conn) != 1 {
		t.Fatal("panicked work committed")
	}
}

func TestOwnedTransactionRefusesEveryEnclosingOwner(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := newTenant("first")
	ctx := tenancy.WithTenant(t.Context(), tenant)
	token := syscap.NewSystemToken("kit/db test: owned transaction refusal")
	refuse := func(ctx context.Context) error {
		t.Helper()
		err := db.RunOwned(ctx, conn, tenant, func(context.Context, db.Tx[db.Tenant]) error {
			t.Fatal("owned callback ran within another owner")
			return nil
		})
		if !errors.Is(err, db.ErrAmbientTransaction) {
			t.Fatalf("owned call = %v", err)
		}
		return nil
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, _ db.Tx[db.Tenant]) error { return refuse(ctx) }); err != nil {
		t.Fatal(err)
	}
	if err := db.RunSystem(t.Context(), conn, token, func(ctx context.Context, _ db.Tx[db.System]) error { return refuse(ctx) }); err != nil {
		t.Fatal(err)
	}
	lazy, pending, err := db.Lazy(ctx, conn, token)
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close(false)
	_ = refuse(lazy)
	if _, err := pending.Tx(lazy); err != nil {
		t.Fatal(err)
	}
	_ = refuse(lazy)
}

func TestOwnedTransactionScopesContextAndNestedWorkToItsExplicitTenant(t *testing.T) {
	_, conn := dbtest.Schema(t)
	want := newTenant("owned")
	for name, incoming := range map[string]context.Context{
		"absent":    t.Context(),
		"different": tenancy.WithTenant(t.Context(), newTenant("other")),
	} {
		t.Run(name, func(t *testing.T) {
			err := db.RunOwned(incoming, conn, want, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				if got, ok := tenancy.FromContext(ctx); !ok || got != want {
					t.Error("callback context does not carry the explicit transaction tenant")
				}
				return db.Run(ctx, conn, func(_ context.Context, nested db.Tx[db.Tenant]) error {
					if nested.DB() != tx.DB() || db.TenantOf(nested) != want {
						t.Error("nested work did not join the explicit tenant transaction")
					}
					return nil
				})
			})
			if err != nil {
				t.Fatalf("nested work in owned transaction: %v", err)
			}
		})
	}
}
