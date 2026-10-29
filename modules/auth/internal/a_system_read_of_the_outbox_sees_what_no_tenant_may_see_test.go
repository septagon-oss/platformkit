package internal_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestASystemReadOfTheOutboxSeesWhatNoTenantMaySee asks the event outbox what it holds of two
// tenants, as each tenant and then as the installation. The outbox is tenant-owned, so each
// tenant counts the one row filed under its own id and never the other's; the installation,
// which sets no tenant between the count and the policy, counts both.
//
// This is the reading behind "the refused door emitted no event": such a claim compares one
// count taken before the call with one taken after, so it holds just as happily when the
// count answers 0 because the policy showed nothing. The mail record carries the same premise
// and is asked separately; the outbox carries the account-email event names this flow could
// have published, which no tenant-scoped comparison can see going quiet.
func TestASystemReadOfTheOutboxSeesWhatNoTenantMaySee(t *testing.T) {
	_, conn := dbtest.Schema(t)
	for _, filing := range []struct {
		tenant tenancy.Tenant
		name   string
	}{{acme, refusedEvents[0]}, {globex, refusedEvents[1]}} {
		err := db.Run(tenancy.WithTenant(t.Context(), filing.tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Exec("INSERT INTO platformkit_outbox (tenant_id, name, payload) VALUES (?, ?, '{}'::jsonb)",
				filing.tenant.ID.String(), filing.name).Error
		})
		if err != nil {
			t.Fatalf("file %s's %s: %v", filing.tenant.Slug, filing.name, err)
		}
	}
	own := map[string]int64{}
	for _, tenant := range []tenancy.Tenant{acme, globex} {
		var rows int64
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE name IN ?", refusedEvents).Row().Scan(&rows)
		})
		if err != nil {
			t.Fatalf("count as %s: %v", tenant.Slug, err)
		}
		own[tenant.Slug] = rows
	}
	all := int64(-1)
	err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE name IN ?", refusedEvents).Row().Scan(&all)
	})
	if err != nil {
		t.Fatalf("count as the installation: %v", err)
	}
	if own["acme"] != 1 || own["globex"] != 1 {
		t.Errorf("the tenants counted %d and %d events of their own, want 1 each: one tenant must not read the other's",
			own["acme"], own["globex"])
	}
	if all != 2 {
		t.Errorf("the installation counted %d events where two tenants file one each: a count with no tenant set must answer for every row in the table, whatever id carries it",
			all)
	}
}
