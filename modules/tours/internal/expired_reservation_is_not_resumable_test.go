package internal_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tours"
	"github.com/septagon-oss/platformkit/modules/tours/contracts"
	"github.com/septagon-oss/platformkit/modules/tours/internal"
)

func TestExpiredUnseenReservationIsNotResumable(t *testing.T) {
	_, conn := dbtest.Schema(t, tours.Migrations)
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	svc := internal.NewService(func() time.Time { return now }, allowingTourPolicy{}, appname.MustParse("tour-test"))
	tenant, person := tenancy.Tenant{ID: uuid.New()}, uuid.New()
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), tenant), person)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Start(ctx, tx, contracts.Start{
			TourID: "intro", Edition: 1, StepIDs: []string{"welcome"},
			ContractHash: "contract", Source: "automatic",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Minute)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		runs, err := svc.Resumable(ctx, tx)
		if err != nil {
			return err
		}
		if len(runs) != 0 {
			t.Fatalf("resumable runs after reservation expiry = %d, want none", len(runs))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
