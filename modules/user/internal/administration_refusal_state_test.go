package internal_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

func TestAdministrationRefusalPreservesTheRowAndPublishesOnlyTheAttempt(t *testing.T) {
	conn, ctx := tenantWith(t)
	svc := newService()
	id := administratorIn(t, ctx, conn, svc, "owner@example.com")
	for _, attempt := range []string{"roles", "status"} {
		t.Run(attempt, func(t *testing.T) {
			var before []string
			if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				before = outbox(t, tx)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				var row *contracts.User
				var err error
				if attempt == "roles" {
					row, err = svc.SetRoles(ctx, tx, id, nil)
				} else {
					row, err = svc.Deactivate(ctx, tx, id)
				}
				if row != nil {
					t.Errorf("refused mutation returned a row: %+v", row)
				}
				return err
			})
			if !errors.Is(err, crud.ErrInvalid) {
				t.Fatalf("last administrator mutation = %v, want invalid", err)
			}
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				u, err := svc.Get(ctx, tx, id)
				if err != nil {
					return err
				}
				if !u.CanAdminister([]string{usertest.Administering}) {
					t.Errorf("refusal changed administrator: %+v", u)
				}
				want := append(slices.Clone(before), contracts.EventAdministrationRefused)
				if got := outbox(t, tx); !slices.Equal(got, want) {
					t.Errorf("events = %v, want %v", got, want)
				}
				var count int64
				if err := tx.DB().Table("platformkit_outbox").Where(
					"name = ? AND payload->>'userId' = ? AND payload->>'attempt' = ?",
					contracts.EventAdministrationRefused, id.String(), attempt).Count(&count).Error; err != nil {
					return err
				}
				if count != 1 {
					t.Errorf("matching refusal records = %d, want one", count)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := db.Run(tenancy.WithTenant(ctx, globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, operation := range []string{"read", "roles", "status"} {
			var row *contracts.User
			var err error
			switch operation {
			case "read":
				row, err = svc.Get(ctx, tx, id)
			case "roles":
				row, err = svc.SetRoles(ctx, tx, id, nil)
			case "status":
				row, err = svc.Deactivate(ctx, tx, id)
			}
			if !errors.Is(err, crud.ErrNotFound) || row != nil {
				t.Errorf("foreign %s returned row %v, error %v", operation, row, err)
			}
		}
		if got := outbox(t, tx); len(got) != 0 {
			t.Errorf("foreign tenant can read or caused events: %v", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
