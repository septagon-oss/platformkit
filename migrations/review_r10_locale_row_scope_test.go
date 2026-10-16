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
//   * a tenant-scoped session writes nothing, not even beside its own id, because
//     `WITH CHECK (platformkit_is_system())` is the half that keeps which languages
//     a customer is served in the operator's declaration;
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

	// A tenant of this installation cannot say which languages it is served in, not
	// even for itself: the write is refused by the policy, and nothing it refused
	// stands behind.
	if err := db.Run(tenancy.WithTenant(ctx, tenancy.Tenant{ID: a, Slug: "r10-languages-a"}), app,
		func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Exec(`INSERT INTO tenant_locales (tenant_id, locale) VALUES (?, ?)`,
				a, "de").Error
		}); err == nil {
		t.Error("a tenant-scoped session wrote a language row: WITH CHECK (platformkit_is_system()) did not bind it")
	}
	got, err := readLocales(ctx, app, a, "r10-languages-a")
	if err != nil {
		t.Fatalf("read after the refused write: %v", err)
	}
	if got != "en,pt-PT" {
		t.Errorf("the refused write left [%s] behind, want the two rows it was refused", got)
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
