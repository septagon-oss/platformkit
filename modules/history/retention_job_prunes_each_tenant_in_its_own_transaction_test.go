package history_test

// The retention job as the manifest hands it to kit/jobs: one Run over every tenant
// the lister names, each pruned inside its own transaction, and a composition that
// wired no lister refused rather than left to keep versions forever.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/history"
	"github.com/septagon-oss/platformkit/modules/history/contracts"
)

// lister is the tenant list the job walks.
type lister []tenancy.Tenant

func (l lister) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) { return l, nil }

// TestTheRetentionJobPrunesEveryTenantAndKeepsTheNewestVersionOfEach runs the job
// the manifest declares, over two tenants holding ancient versions: the rule releases
// the older of each tenant's pair, the newest stands in both, and the two tenants'
// sweeps are the same tick.
func TestTheRetentionJobPrunesEveryTenantAndKeepsTheNewestVersionOfEach(t *testing.T) {
	_, app := dbtest.Schema(t, history.Migrations)
	svc, _, manifest, err := history.New(history.Deps{
		Tenants:   lister{acme, globex},
		Retention: contracts.Rule{Days: 90, Versions: 1},
	})
	if err != nil {
		t.Fatalf("build the module: %v", err)
	}
	if len(manifest.Jobs) != 1 || manifest.Jobs[0].Name != "history-retention" {
		t.Fatalf("the manifest declares the jobs %+v, want history-retention alone", manifest.Jobs)
	}
	record := uuid.New()
	ancient := time.Now().UTC().Add(-400 * 24 * time.Hour)
	for _, tenant := range []tenancy.Tenant{acme, globex} {
		for _, number := range []int64{1, 2} {
			err := db.Run(tenancy.WithTenant(t.Context(), tenant), app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := svc.Record(ctx, tx, contracts.Input{
					Module: "content", Entity: "page", Locale: "en", Record: record, Version: number,
					Fields: json.RawMessage(`{"title":"Ancient"}`), At: ancient.Add(time.Duration(number) * time.Minute),
				})
				return err
			})
			if err != nil {
				t.Fatalf("keep %s's version %d: %v", tenant.Slug, number, err)
			}
		}
	}

	if err := manifest.Jobs[0].Run(t.Context(), app); err != nil {
		t.Fatalf("the retention job refused: %v", err)
	}

	for _, tenant := range []tenancy.Tenant{acme, globex} {
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			items, err := svc.Page(ctx, tx, contracts.Query{
				Module: "content", Entity: "page", Locale: "en", Record: record, Limit: 10,
			})
			if err != nil {
				return err
			}
			if len(items) != 1 || items[0].Version != 2 {
				t.Errorf("%s's series stands at %d versions after the job, want version 2 alone", tenant.Slug, len(items))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestTheRetentionJobRefusesToRunWithNoTenantListerRatherThanKeepEverythingForever
// is the job's one boot-shaped refusal at run time: with nobody to list the tenants
// for, the job says so instead of silently retaining every version.
func TestTheRetentionJobRefusesToRunWithNoTenantListerRatherThanKeepEverythingForever(t *testing.T) {
	_, app := dbtest.Schema(t, history.Migrations)
	_, _, manifest, err := history.New(history.Deps{})
	if err != nil {
		t.Fatalf("build the module: %v", err)
	}
	if err := manifest.Jobs[0].Run(t.Context(), app); err == nil {
		t.Fatal("the retention job ran with no tenant lister and refused nothing")
	}
}
