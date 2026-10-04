package internal_test

// A reviewer's case, T-0111 review round 1.
//
// Two paths write the same fact — which languages a tenant that nobody chose a
// language for is served in — and they do not agree.
//
//   migrations/000027 backfills `tenant_locales` from `tenants.default_locale`,
//   one row per tenant, and says why in its own words: "Every tenant that exists
//   today is served in the one language it was served in yesterday … an empty set
//   reads as 'this tenant declared nothing', and a deployment must not change
//   which languages it answers in because somebody ran a migration."
//
//   Service.Create writes the installation's whole set instead (modules/tenant/
//   internal/service.go, `s.serve(tx, t.ID, s.served(t.DefaultLocale)...)`, fed by
//   Deps.Languages, which apps/platformkit hands as `installed.Languages()`).
//
// So on one installation, at one moment, a row that predates the migration answers
// a Portuguese browser in English and an identical row created after it answers
// that browser in Portuguese. The set is a declaration, and on the create path
// nobody declared it: `SetLocale` is the command that says which languages a
// tenant's people are served in, and it has not been called.
//
// The assertion below is the one the migration states. It can be satisfied only by
// making Create seed the default alone — the migration cannot seed the
// installation's set, because SQL does not know which catalogues the composition
// holds — so this is a case that pins the inconsistency rather than a taste.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestATenantNobodyChoseALanguageForIsServedInTheOneItsCopyIsWrittenIn(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	// The installation speaks two languages, which is what apps/platformkit hands
	// it: `xtext.Catalog.Languages()` of the reference composition.
	svc := internal.NewService(nil, []string{"en", "pt-PT"})
	installed(t, conn, svc)

	var created *contracts.Tenant
	var stored []string
	var resolved tenancy.Tenant
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		created, err = svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"})
		if err != nil {
			return err
		}
		if resolved, err = svc.ByHost(ctx, tx, "acme.example.com"); err != nil {
			return err
		}
		return tx.DB().Table("tenant_locales").Where("tenant_id = ?", created.ID).
			Order("locale").Pluck("locale", &stored).Error
	})
	if err != nil {
		t.Fatalf("create a tenant: %v", err)
	}

	if created.DefaultLocale != "en" {
		t.Fatalf("a new tenant's default = %q, want the language the copy is written in", created.DefaultLocale)
	}
	// What every request actually reads: the host resolution ui/page negotiates over.
	if len(resolved.Languages.Preferred()) > 1 {
		t.Errorf("resolving acme.example.com offers %v to a browser; a tenant whose operator never spoke has "+
			"no second language to offer",
			resolved.Languages.Preferred())
	}

	if len(stored) != 1 || stored[0] != "en" {
		t.Errorf("a tenant nobody declared a language for is served in %v; migrations/000027 gives a "+
			"tenant that already existed exactly its default language, so one of the two paths is wrong "+
			"and a Portuguese browser is answered differently by two identical tenants", stored)
	}
	if len(created.Locales) != 0 {
		t.Errorf("Create declared %v as languages besides the default %q for a tenant whose only "+
			"declaration is its slug, its name and one host; SetLocale is the command that declares them",
			created.Locales, created.DefaultLocale)
	}
}
