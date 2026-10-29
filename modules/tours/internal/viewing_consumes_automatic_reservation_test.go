package internal_test

import (
	"context"
	"database/sql"
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

func TestViewingAutomaticRunConsumesReservation(t *testing.T) {
	_, conn := dbtest.Schema(t, tours.Migrations)
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	app := appname.MustParse("tour-test")
	service := internal.NewService(func() time.Time { return at }, allowingTourPolicy{}, app)
	tenant, person := tenancy.Tenant{ID: uuid.New()}, uuid.New()
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), tenant), person)

	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		run, err := service.Start(ctx, tx, contracts.Start{
			TourID: "intro", Edition: 1, StepIDs: []string{"welcome"}, ContractHash: "contract", Source: "automatic",
		})
		if err != nil {
			return err
		}
		if _, err := service.Act(ctx, tx, person, run.ID, contracts.Action{
			Kind: "viewed", Step: run.CurrentStep, ExpectedRevision: run.Revision,
		}); err != nil {
			return err
		}

		var consumedAt sql.NullTime
		if err := tx.DB().Raw("SELECT consumed_at FROM tours_reservations WHERE tenant_id = ? AND run_id = ?", tenant.ID, run.ID).Scan(&consumedAt).Error; err != nil {
			return err
		}
		if !consumedAt.Valid || !consumedAt.Time.Equal(at) {
			t.Errorf("automatic reservation consumed_at = %v, want %s after viewing", consumedAt, at)
		}
		var lastViewedAt sql.NullTime
		if err := tx.DB().Raw("SELECT last_automatic_viewed_at FROM tours_person_frequency WHERE tenant_id = ? AND app = ? AND person_id = ?", tenant.ID, app.String(), person).Scan(&lastViewedAt).Error; err != nil {
			return err
		}
		if !lastViewedAt.Valid || !lastViewedAt.Time.Equal(at) {
			t.Errorf("last automatic view = %v, want %s", lastViewedAt, at)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
