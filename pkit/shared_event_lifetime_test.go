package pkit_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestClosingOneRuntimePreservesTheOtherRuntimesEventContract(t *testing.T) {
	cfg := onOneDatabase(t)
	deployment := buildDeployment(cfg, app.All)
	first, err := pkit.NewApp("collect").Use(doors, desk, postedModule[postedNumber]("ledger")).Build(t.Context(), deployment)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	type sameNumber struct {
		Number int64 `json:"number"`
	}
	second, err := pkit.NewApp("collect").Use(doors, desk, postedModule[sameNumber]("ledger")).Build(t.Context(), deployment)
	if err != nil {
		t.Fatalf("equivalent event payload prevented a second lifecycle: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	for range 2 {
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New(), Slug: "acme"})
	publish := func(payload any) error {
		return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, "ledger.posted", payload)
		})
	}
	if err := publish(postedNumber{Number: 7}); err != nil {
		t.Fatalf("surviving lifecycle cannot publish its declared payload: %v", err)
	}
	if err := publish(postedText{Number: "wrong"}); err == nil || !strings.Contains(err.Error(), "ledger.posted") {
		t.Errorf("closing another runtime removed the survivor's payload guard: %v", err)
	}
	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	var count int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = 'ledger.posted'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("outbox contains %d rows; only the valid payload may commit", count)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := pkit.NewApp("collect").Use(doors, desk, postedModule[postedText]("ledger")).Build(t.Context(), deployment)
	if err != nil {
		t.Fatalf("closing the last runtime retained its old payload contract: %v", err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
}
