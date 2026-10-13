package internal_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/site/contracts/sitetest"
	"github.com/septagon-oss/platformkit/modules/site/internal"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

var errRollback = errors.New("rolled back on purpose")

const outbox = "platformkit_outbox"

// bareService is the service the cases that are not about colour build: an
// installation that names no accent of its own, so a site with no colour of its
// own is answered no colour and every colour these cases assert is one a caller
// wrote. The case above builds its service from the suite's palette instead, so
// the two implementations resolve against the same answer.
func bareService() *internal.Service { return internal.NewService(nil) }

// TestServiceConforms runs the same suite the fake runs, against the real
// service, a real Postgres and a real tenant transaction.
func TestServiceConforms(t *testing.T) {
	sitetest.RunService(t, func(t *testing.T, run func(sitetest.Fixture)) {
		_, conn := dbtest.Schema(t, site.Migrations)
		svc := internal.NewService(sitetest.InstallationPalette)
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(sitetest.Fixture{
				Ctx: ctx, Tx: tx, Service: svc, Locked: svc, Palette: sitetest.InstallationPalette,
				Published: func() []string {
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

// TestSavingTheReadsAnswerStoresTheAbsenceOfAColour is the half of the round trip
// the conformance suite cannot see, because that suite may only ask the service:
// the row keeps the empty colour after a save of a colour nobody chose, so the
// tenant keeps following the installation the day its accent moves. The read above
// proves what a caller is shown; this one proves what is kept, and that
// migrations/000054 moved the column's own default along with the entity's.
func TestSavingTheReadsAnswerStoresTheAbsenceOfAColour(t *testing.T) {
	admin, conn := dbtest.Schema(t, site.Migrations)
	svc := internal.NewService(sitetest.InstallationPalette)

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.Save(ctx, tx, &contracts.SiteSettings{Title: "Acme"}); err != nil {
			return err
		}
		shown, err := svc.Settings(ctx, tx)
		if err != nil {
			return err
		}
		if shown.PrimaryColor != sitetest.InstallationPalette.Accent(contracts.ThemeLight) {
			t.Errorf("the read answers %q, want the installation's accent", shown.PrimaryColor)
		}
		if _, err := svc.Save(ctx, tx, shown); err != nil {
			return fmt.Errorf("echo the read's answer: %w", err)
		}
		var stored string
		var revision int64
		// btrim because char(7) is blank-padded: the padding is how the column shows a
		// short value, not a colour, and Service.stored trims it the same way.
		if err := tx.DB().Raw(`SELECT btrim(primary_color), revision FROM site_settings`).Row().Scan(&stored, &revision); err != nil {
			return err
		}
		if stored != contracts.DefaultPrimaryColor {
			t.Errorf("the row holds %q, want the empty colour: the palette's accent is not this tenant's to keep",
				stored)
		}
		if revision != 1 {
			t.Errorf("the row is at revision %d, want 1: echoing a read writes nothing", revision)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("the case's transaction: %v", err)
	}

	var heldDefault string
	// table_schema = current_schema(): one database serves the whole suite, and an
	// unqualified information_schema query answers about whichever other test's
	// site_settings it reaches first — another schema still on the old default makes
	// this case fail in a full run while passing alone.
	if err := admin.QueryRowContext(t.Context(), `SELECT column_default FROM information_schema.columns
		WHERE table_schema = current_schema()
		AND table_name = 'site_settings' AND column_name = 'primary_color'`).Scan(&heldDefault); err != nil {
		t.Fatalf("read the column default: %v", err)
	}
	if !strings.HasPrefix(heldDefault, "''") {
		t.Errorf("the column defaults to %s; migrations/000054 moves it to the empty colour the contracts name",
			heldDefault)
	}
}

// TestOneSitePerTenant is the invariant the routes are shaped around, kept by
// the database rather than by the service: the unique index in
// migrations/000018 refuses a second row whatever Go believes.
func TestOneSitePerTenant(t *testing.T) {
	admin, conn := dbtest.Schema(t, site.Migrations)
	svc := bareService()

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
	admin, conn := dbtest.Schema(t, site.Migrations)
	svc := bareService()

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
	admin, conn := dbtest.Schema(t, site.Migrations)
	svc := bareService()

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.Save(ctx, tx, &contracts.SiteSettings{Title: "Acme"}); err != nil {
			return err
		}
		var title string
		if err := tx.DB().Table("site_settings").Select("title").Row().Scan(&title); err != nil {
			return err
		}
		if title != "Acme" {
			t.Fatal("the settings were not stored before the intentional rollback")
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("settings rollback = %v, want the failure after a successful save", err)
	}

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
