package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/site/internal"
)

// TestARefusedAccentWritesNothingAndSaysNothing is the write-refusal invariant
// run against the real service, a real Postgres and the real outbox: an accent
// outside the contrast band is refused with crud.ErrInvalid, returns no row at
// all, leaves the stored colour and its revision exactly where they were, and
// puts nothing in the outbox. The conformance case asserts the refusal; this one
// asserts the silence and the untouched row behind it.
func TestARefusedAccentWritesNothingAndSaysNothing(t *testing.T) {
	_, conn := dbtest.Schema(t, site.Migrations)
	svc := internal.NewService()

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		saved, err := svc.Save(ctx, tx, &contracts.SiteSettings{Title: "Acme", PrimaryColor: "#b45309"})
		if err != nil {
			t.Fatalf("the save this case starts from: %v", err)
		}
		events := func() int64 {
			var n int64
			if err := tx.DB().Raw(`SELECT count(*) FROM `+outbox+` WHERE tenant_id = ?`, acme.ID).Scan(&n).Error; err != nil {
				t.Fatalf("count the outbox: %v", err)
			}
			return n
		}
		before := events()

		out, err := svc.Save(ctx, tx, &contracts.SiteSettings{Title: "Acme", PrimaryColor: "#ff8800"})
		if !errors.Is(err, crud.ErrInvalid) {
			t.Fatalf("saving an accent that reads 2.08:1 on the light canvas: error is %v, want crud.ErrInvalid", err)
		}
		if out != nil {
			t.Errorf("a refused save returned a row: %+v", out)
		}

		got, err := svc.Settings(ctx, tx)
		if err != nil {
			t.Fatalf("Settings after the refusal: %v", err)
		}
		if got.PrimaryColor != "#b45309" || got.Revision != saved.Revision {
			t.Errorf("after the refusal the row reads %q at revision %d, want %q at revision %d: a refused write moves nothing",
				got.PrimaryColor, got.Revision, "#b45309", saved.Revision)
		}
		if after := events(); after != before {
			t.Errorf("the refusal put %d event(s) in the outbox; a refused mutation emits nothing", after-before)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("the case's transaction: %v", err)
	}
}
