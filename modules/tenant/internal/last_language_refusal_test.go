package internal_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
)

func TestRemovingTheLastTenantLanguageWritesNothing(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	svc := internal.NewServiceWithLocaleManager(nil, []string{"en", "pt-PT"}, allowedLocaleManager{})
	var tenant *contracts.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		tenant, err = svc.Create(ctx, tx, contracts.NewTenant{
			Slug: "acme", Name: "Acme", Host: "acme.example.com",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var initialEvents int64
	if err := owner.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE tenant_id = $1`, tenant.ID).
		Scan(&initialEvents); err != nil {
		t.Fatal(err)
	}

	// An empty declaration would remove the sole language. Commit the surrounding
	// transaction after the refusal so an accidental write cannot hide in rollback.
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant.Tenancy()), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			row, err := svc.SetOwnLocale(ctx, tx, 0, contracts.SetLocale{})
			if row != nil {
				t.Errorf("refusal returned a stale tenant row: %+v", row)
			}
			if err == nil || !errors.Is(err, crud.ErrInvalid) || !strings.Contains(err.Error(), "language tag") {
				t.Errorf("removing the last language gave %v, want a stated invalid-language reason", err)
			}
			return nil
		}); err != nil {
		t.Fatal(err)
	}

	var current string
	var stored string
	var revision, rows, events, localeEvents int64
	if err := owner.QueryRowContext(t.Context(),
		`SELECT default_locale, locale_revision FROM tenants WHERE id = $1`, tenant.ID).
		Scan(&current, &revision); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(t.Context(),
		`SELECT count(*), COALESCE(string_agg(locale, ',' ORDER BY locale), '')
		 FROM tenant_locales WHERE tenant_id = $1`, tenant.ID).
		Scan(&rows, &stored); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(t.Context(),
		`SELECT count(*), count(*) FILTER (WHERE name = 'tenant.locale_set')
		 FROM platformkit_outbox WHERE tenant_id = $1`, tenant.ID).
		Scan(&events, &localeEvents); err != nil {
		t.Fatal(err)
	}
	if current != "en" || revision != 0 || rows != 1 || stored != "en" ||
		events != initialEvents || localeEvents != 0 {
		t.Errorf("a refused removal left default=%q revision=%d locale rows=%d (%q) events=%d (initial %d, locale %d)",
			current, revision, rows, stored, events, initialEvents, localeEvents)
	}

	// A legacy tenant may have no locale row. That absence must not prevent the
	// next valid declaration from installing its own set.
	if _, err := owner.ExecContext(t.Context(),
		`DELETE FROM tenant_locales WHERE tenant_id = $1`, tenant.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant.Tenancy()), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			row, err := svc.SetOwnLocale(ctx, tx, 0, contracts.SetLocale{Default: "pt-PT"})
			if err != nil {
				return err
			}
			if row == nil || row.DefaultLocale != "pt-PT" || row.LocaleRevision != 1 {
				t.Errorf("valid declaration after a missing row returned %+v", row)
			}
			return nil
		}); err != nil {
		t.Fatalf("a valid declaration after a missing locale row was refused: %v", err)
	}
}
