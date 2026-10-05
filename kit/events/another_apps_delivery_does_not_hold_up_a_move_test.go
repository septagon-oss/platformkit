package events_test

// Two apps on one database share the two ledger tables. A delivery of the other
// app — already scoped, in its own tenant, under its own durable — touches no row
// this app's move reads or writes, so it is not a reason for the move to refuse:
// under steady traffic of the other app such a refusal is every attempt, and the
// window the move exists to close stays open while this app's consumers run.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestAnotherAppsDeliveryMidClaimDoesNotRefuseTheMove: academy's delivery holds
// an open claim under academy's durable in academy's tenant; collect's move of its
// own tenant's unscoped claim completes.
func TestAnotherAppsDeliveryMidClaimDoesNotRefuseTheMove(t *testing.T) {
	_, conn := dbtest.Schema(t)
	shop := tenantID(t, conn, "collect-shop", "collect")
	school := tenancy.Tenant{ID: uuid.New(), Slug: "academy-school"}
	placeTenant(t, conn, school.ID, school.Slug, "academy")
	id := uuid.New()
	claimRow(t, conn, appname.Durable(appname.Name(""), ledgerModule, ledgerEvent), id, shop)

	held, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		err := db.Run(tenancy.WithTenant(t.Context(), school), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			if err := tx.DB().Exec(`INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES (?, ?, ?)`,
				uuid.New(), appname.Durable("academy", ledgerModule, ledgerEvent), school.ID).Error; err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
		if err != nil {
			t.Errorf("academy's transaction holding a claim: %v", err)
		}
	}()
	<-held

	report, err := events.MoveLedger(t.Context(), conn, "collect", "boot")
	close(release)
	<-done
	if err != nil {
		t.Fatalf("collect's move refused over academy's delivery, which touches none of its rows: %v", err)
	}
	if report.Claims != 1 {
		t.Errorf("collect's move reported %+v, want its tenant's one claim", report)
	}
	scoped := appname.Durable("collect", ledgerModule, ledgerEvent)
	if got := durables(t, conn, "platformkit_handled", id); len(got) != 1 || got[0] != scoped {
		t.Errorf("collect's claim sits at %v, want %q", got, scoped)
	}
}
