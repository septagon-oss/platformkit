package idempotency_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestCommandEffectHoldsTheClaimUntilItsTransactionEnds(t *testing.T) {
	for _, commit := range []bool{true, false} {
		name := "rollback"
		if commit {
			name = "commit"
		}
		t.Run(name, func(t *testing.T) {
			_, conn := dbtest.Schema(t)
			store := idempotency.Postgres(httpx.ConnFrom)
			ctx := httpx.WithConn(t.Context(), conn)
			s := scope("locked-effect")
			first, err := store.Claim(ctx, s, start)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Begin(ctx, s, first.Claim, start); err != nil {
				t.Fatal(err)
			}
			later := start.Add(idempotency.Lease + time.Second)
			rollback := errors.New("roll back the command")
			err = db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: s.Tenant, Slug: "acme"}), conn,
				func(markCtx context.Context, tx db.Tx[db.Tenant]) error {
					if err := store.Mark(markCtx, tx, s, first.Claim); err != nil {
						return err
					}
					held, err := store.Claim(ctx, s, later)
					if err != nil {
						return err
					}
					if held.Kind != idempotency.InFlight {
						t.Errorf("claim with uncommitted marker = %v, want InFlight", held.Kind)
					}
					if !commit {
						return rollback
					}
					return nil
				})
			if err != nil && !errors.Is(err, rollback) {
				t.Fatal(err)
			}
			resolved, err := store.Claim(ctx, s, later)
			if err != nil {
				t.Fatal(err)
			}
			want := idempotency.First
			if commit {
				want = idempotency.Spent
			}
			if resolved.Kind != want {
				t.Errorf("claim after %s = %v, want %v", name, resolved.Kind, want)
			}
			if !commit {
				if err := store.Complete(ctx, s, first.Claim, idempotency.Answer{Status: 201}, later); !errors.Is(err, idempotency.ErrLostClaim) {
					t.Errorf("stale completion = %v, want ErrLostClaim", err)
				}
			}
		})
	}
}
