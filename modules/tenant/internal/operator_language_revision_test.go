package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
)

func TestOperatorLanguageWriteRefusesAStaleDeclaration(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	svc := internal.NewServiceWithLocaleManager(nil, []string{"en", "pt-PT"}, allowedLocaleManager{})
	var tenant *contracts.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		tenant, err = svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// The operator read revision zero before the tenant administrator's edit.
	// Decode the wire declaration so the case stays valid when the command gains
	// the expectedRevision field its stale-read refusal requires.
	var stale contracts.SetLocale
	if err := json.Unmarshal([]byte(`{"default":"en","supported":["pt-PT"],"expectedRevision":0}`), &stale); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant.Tenancy()), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.SetOwnLocale(ctx, tx, 0, contracts.SetLocale{Default: "pt-PT"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		row, err := svc.SetLocale(ctx, tx, tenant.ID, stale)
		if !errors.Is(err, crud.ErrConflict) {
			t.Errorf("stale operator declaration returned %v, want conflict", err)
		}
		if row != nil {
			t.Errorf("stale operator declaration returned a row at revision %d", row.LocaleRevision)
		}
		// Commit after the refusal: an erroneous mutation cannot hide in rollback.
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var language string
	var revision, events int64
	if err := owner.QueryRowContext(t.Context(), `SELECT default_locale, locale_revision FROM tenants WHERE id=$1`, tenant.ID).Scan(&language, &revision); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox WHERE tenant_id=$1 AND name='tenant.locale_set'`, tenant.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if language != "pt-PT" || revision != 1 || events != 1 {
		t.Errorf("stale operator write left default=%q revision=%d events=%d, want pt-PT/1/1", language, revision, events)
	}
}
