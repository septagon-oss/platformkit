package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/site/contracts/sitetest"
	"github.com/septagon-oss/platformkit/modules/site/internal"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

var errRollback = errors.New("rolled back on purpose")

const outbox = "platformkit_outbox"

// TestServiceConforms runs the same suite the fake runs, against the real
// service, a real Postgres and a real tenant transaction.
func TestServiceConforms(t *testing.T) {
	sitetest.RunService(t, func(t *testing.T, run func(sitetest.Fixture)) {
		_, conn := dbtest.Schema(t)
		svc := internal.NewService()
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(sitetest.Fixture{Ctx: ctx, Tx: tx, Service: svc, Published: func() []string {
				var names []string
				err := tx.DB().Raw(`SELECT name FROM `+outbox+` WHERE tenant_id = ? ORDER BY created_at, id`, acme.ID).
					Scan(&names).Error
				if err != nil {
					t.Fatalf("read the outbox: %v", err)
				}
				return names
			}})
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the case's transaction: %v", err)
		}
	})
}

// TestOneSitePerTenant is the invariant the routes are shaped around, kept by
// the database rather than by the service: the unique index in
// migrations/000018 refuses a second row whatever Go believes.
func TestOneSitePerTenant(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	svc := internal.NewService()

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.Save(ctx, tx, &contracts.SiteSettings{Title: "Acme"}); err != nil {
			return err
		}
		// A second save is an update of the one row, whatever it says about
		// identity: a body carrying somebody else's id does not make a row.
		_, err := svc.Save(ctx, tx, &contracts.SiteSettings{
			Base: crud.Base{ID: uuid.New()}, Title: "Acme Corporation",
		})
		return err
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	var rows int
	var title string
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM site_settings`).Scan(&rows); err != nil {
		t.Fatalf("count the rows: %v", err)
	}
	if err := admin.QueryRowContext(t.Context(), `SELECT title FROM site_settings`).Scan(&title); err != nil {
		t.Fatalf("read the row: %v", err)
	}
	if rows != 1 || title != "Acme Corporation" {
		t.Errorf("there are %d rows saying %q, want the one this tenant has", rows, title)
	}
}

// TestSavingPublishesInTheCallersTransaction is what the conformance suite
// cannot see, because the fake has no outbox: the change and the event that
// describes it are one transaction, and an identical save writes neither.
func TestSavingPublishesInTheCallersTransaction(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	svc := internal.NewService()

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for range 2 { // the second is the same form submitted twice
			settings := &contracts.SiteSettings{
				Title: " Acme ", Theme: contracts.ThemeDark,
				Nav: contracts.Nav{{Label: "About", Path: "/about-us"}},
			}
			if _, err := svc.Save(ctx, tx, settings); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	var events int
	var title string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE name = $1`, contracts.EventSettingsUpdated).Scan(&events); err != nil {
		t.Fatalf("count the events: %v", err)
	}
	if err := admin.QueryRowContext(t.Context(), `SELECT title FROM site_settings`).Scan(&title); err != nil {
		t.Fatalf("read the row: %v", err)
	}
	if events != 1 {
		t.Errorf("%d events, want one: the second save changed nothing, whitespace included", events)
	}
	if title != "Acme" {
		t.Errorf("the title is %q; it is trimmed, so two callers cannot disagree about whitespace", title)
	}
}

// TestARolledBackSaveLeavesNothing: a transaction that does not commit leaves
// neither the settings nor the event.
func TestARolledBackSaveLeavesNothing(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	svc := internal.NewService()

	_ = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.Save(ctx, tx, &contracts.SiteSettings{Title: "Acme"}); err != nil {
			return err
		}
		return context.Canceled
	})

	var rows, events int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM site_settings`).Scan(&rows); err != nil {
		t.Fatalf("count the rows: %v", err)
	}
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&events); err != nil {
		t.Fatalf("count the outbox: %v", err)
	}
	if rows != 0 || events != 0 {
		t.Errorf("after the rollback there are %d rows and %d events; want none of either", rows, events)
	}
}
