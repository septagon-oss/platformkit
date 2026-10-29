package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tours"
	"github.com/septagon-oss/platformkit/modules/tours/contracts"
	"github.com/septagon-oss/platformkit/modules/tours/internal"
)

func TestStartRefusesAnUndeclaredTour(t *testing.T) {
	_, conn := dbtest.Schema(t, tours.Migrations)
	svc := internal.NewService(func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }, allowingTourPolicy{}, appname.MustParse("tour-test"))
	tenant, person := tenancy.Tenant{ID: uuid.New()}, uuid.New()
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), tenant), person)
	var startErr error
	var count int64
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Start(ctx, tx, contracts.Start{
			TourID: "not-in-the-composition", Edition: 1, StepIDs: []string{"invented-step"},
			ContractHash: "caller-chosen-hash", Source: "manual",
		})
		startErr = err
		if err := tx.DB().Model(&contracts.Run{}).Count(&count).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(startErr, crud.ErrNotFound) {
		t.Errorf("starting an undeclared tour error = %v, want not found", startErr)
	}
	if count != 0 {
		t.Errorf("refused undeclared tour left %d run rows, want none", count)
	}
}
