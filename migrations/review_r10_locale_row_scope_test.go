package migrations_test

// Review round 10's pin: the languages table this task adds is scoped by the
// database, and that scoping has a case of its own.
//
// `migrations/000029_tenant_locale.up.sql` carries
// `COMMENT ON TABLE tenant_locales IS 'platformkit:tenant-scoping-exempt: …'`, which
// is what the generic "every table is scoped or exempt on purpose" gate reads — so
// that gate passes for a policy that leaks as readily as for one that does not. The
// case that decides the question reads rows: two tenants, one table, the application
// role, each tenant scoped by the kernel's own mechanism (`db.Run` over a context
// `tenancy.WithTenant` tagged, the only door that writes the tenant setting), because
// naming that setting from a test is what `scripts/check_gucs.sh` refuses outside
// `kit/db` — in prose as readily as in code, which this file learned by being refused.
//
// Which is also the verdict on the case round 9 wrote and this head does not carry:
// its scope came from a literal call that set that setting by hand, the GUC gate
// merged after it was written refused that even in a comment, and the file left with the
// assertion rather than the assertion moving to a door the kernel already opens.
// This file is that assertion, reached the way the delivery reaches it.
//
// What is claimed, in both directions:
//
//   * each tenant reads its own tags and none of the other's — the controls are the
//     rows themselves, so a policy widened to "every tenant" is caught by the
//     refusal, not by the absence of a row;
//   * a tenant-scoped session writes no row belonging to another tenant, and the
//     write it is refused leaves nothing behind. It may now write its own — see the
//     block below for what changed and what did not;
//   * the same write through a system transaction, the door `SetLocale` uses, lands.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestATenantReadsItsOwnLanguagesAndWritesNone(t *testing.T) {
	ctx := t.Context()
	_, app := dbtest.Schema(t)

	a, b := uuid.New(), uuid.New()
	if err := dbtest.System(ctx, app, func(_ context.Context, tx db.Tx[db.System]) error {
		for _, seed := range []struct {
			id      uuid.UUID
			slug    string
			locales []string
		}{
			{a, "r10-languages-a", []string{"en", "pt-PT"}},
			// The second tenant's only tag is one the first does not carry, so a
			// leaked row is a tag the reader has no declaration for rather than a
			// duplicate of a row it does.
			{b, "r10-languages-b", []string{"ja"}},
		} {
			if err := tx.DB().Exec(
				`INSERT INTO tenants (id, slug, name) VALUES (?, ?, ?)`,
				seed.id, seed.slug, seed.slug).Error; err != nil {
				return err
			}
			for _, locale := range seed.locales {
				if err := tx.DB().Exec(
					`INSERT INTO tenant_locales (tenant_id, locale) VALUES (?, ?)`,
					seed.id, locale).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("the control plane's own write: %v", err)
	}

	for _, want := range []struct {
		who     uuid.UUID
		slug    string
		locales string
	}{
		{a, "r10-languages-a", "en,pt-PT"},
		{b, "r10-languages-b", "ja"},
	} {
		got, err := readLocales(ctx, app, want.who, want.slug)
		if err != nil {
			t.Fatalf("%s reads its own languages: %v", want.slug, err)
		}
		if got != want.locales {
			t.Errorf("a session scoped to %s reads [%s] from tenant_locales, want [%s] and no other tenant's row",
				want.slug, got, want.locales)
		}
	}

	// What this pin refuses today is a tenant naming somebody else.
	//
	// The block that used to sit here refused a tenant's own row as well: `WITH
	// CHECK (platformkit_is_system())` made every language row the operator's to
	// write. T-0196's fourth section gave that back to the customer — a tenant's
	// administrator holds `tenant:locale_manage` and `Service.SetOwnLocale` writes
	// the set through it — and `migrations/000031_tenant_locale_self_service.up.sql`
	// rewrote these policies to `platformkit_is_system() OR tenant_id =
	// platformkit_current_tenant_id()`. So the sentence that carries the security
	// property now is the scope, not the table: the session below is one tenant's,
	// and the row it names is another tenant's, which is the write no version of
	// this policy has ever allowed and the one a widened `USING` clause would let
	// through.
	//
	// What the tenant *may* do to its own rows is not this file's question: the
	// column rule a row policy cannot state is `tenants_language_only`, the BEFORE
	// UPDATE trigger, and `modules/tenant/internal/self_service_test.go` proves it at
	// the application role rather than inferring it from the SQL.
	if err := db.Run(tenancy.WithTenant(ctx, tenancy.Tenant{ID: a, Slug: "r10-languages-a"}), app,
		func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Exec(`INSERT INTO tenant_locales (tenant_id, locale) VALUES (?, ?)`,
				b, "de").Error
		}); err == nil {
		t.Error("a session scoped to r10-languages-a wrote a language row for r10-languages-b: the policy's own-tenant check does not bind the row it is given")
	}
	for _, want := range []struct {
		who     uuid.UUID
		slug    string
		locales string
	}{
		{a, "r10-languages-a", "en,pt-PT"},
		{b, "r10-languages-b", "ja"},
	} {
		got, err := readLocales(ctx, app, want.who, want.slug)
		if err != nil {
			t.Fatalf("read after the refused write: %v", err)
		}
		if got != want.locales {
			t.Errorf("the refused write left [%s] on %s, want the rows it was refused", got, want.slug)
		}
	}
}

// readLocales is one tenant's own read of the table, at the application role, with
// the scoping the application uses and no other door.
func readLocales(ctx context.Context, app *db.Conn, id uuid.UUID, slug string) (string, error) {
	out := ""
	err := db.Run(tenancy.WithTenant(ctx, tenancy.Tenant{ID: id, Slug: slug}), app,
		func(_ context.Context, tx db.Tx[db.Tenant]) error {
			rows, err := tx.DB().Raw(
				`SELECT locale FROM tenant_locales WHERE tenant_id = ? OR tenant_id IS NOT NULL ORDER BY locale`,
				id).Rows()
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var locale string
				if err := rows.Scan(&locale); err != nil {
					return err
				}
				if out != "" {
					out += ","
				}
				out += locale
			}
			return rows.Err()
		})
	return out, err
}
