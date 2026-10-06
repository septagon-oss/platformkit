package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/translation"
	"github.com/septagon-oss/platformkit/modules/translation/internal"
)

// TestAnotherTenantCannotRemoveOrOverwriteTheFirstTenantsTranslation: a second
// tenant naming the first tenant's record id deletes nothing, forgets nothing
// and overwrites nothing; its save lands in its own tenant.
func TestAnotherTenantCannotRemoveOrOverwriteTheFirstTenantsTranslation(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	svc := internal.NewService(nil, nil)
	id := uuid.New()
	save := func(ctx context.Context, tx db.Tx[db.Tenant], value string) error {
		return svc.Save(ctx, tx, rest.SaveQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id,
			Values: map[string]string{"title": value}, Expected: map[string]int64{"title": 0},
			Source: map[string]string{"title": "About us."},
		})
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return save(ctx, tx, "Sobre nós.")
	}); err != nil {
		t.Fatalf("Acme's save: %v", err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), other), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := svc.Untranslate(ctx, tx, rest.ReviewQuery{Module: "pages", Entity: "page", Locale: "pt-PT",
			RecordID: id, Fields: []string{"title"}}); err != nil {
			return err
		}
		if err := svc.ForgetRecord(ctx, tx, "pages", "page", id); err != nil {
			return err
		}
		return save(ctx, tx, "Outra coisa.")
	}); err != nil {
		t.Fatalf("the other tenant's writes: %v", err)
	}
	var mine []string
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT value FROM translations WHERE record_id = ?`, id).Scan(&mine).Error
	}); err != nil {
		t.Fatalf("reading Acme's rows: %v", err)
	}
	if len(mine) != 1 || mine[0] != "Sobre nós." {
		t.Errorf("Acme's translation after the other tenant's writes: %v, want [Sobre nós.]", mine)
	}
}
