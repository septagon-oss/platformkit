package main

// A reviewer's case, T-0111 review round 2 — the new `--language` flag of
// `platformkit bootstrap` under mutation.
//
// The flag is the newest write in the tree and the one with no caller to
// authorize: it runs inside `app.Bootstrap`'s transaction and reaches the module's
// own `SetLocale`, which — since the cure for round 1's Finding 2 — refuses a
// language this installation has no copy for. So there is now a way for the one
// write with no caller to fail halfway: the tenant, its host, its administrator and
// the language list are four writes in one transaction, and a deployment that is
// told "de-DE is not a language this installation has copy for" must be left exactly
// where it was, not with a tenant that has an administrator and no languages, or
// with a tenant that blocks the retry it needs to fix the command line.
//
// The assertion is the house rule, not a taste: a refused mutation writes nothing.
// It is read back from the tables the transaction wrote through, and then proved
// again at the only surface a deployment has — the same command, run again with a
// language that exists, has to work. `TestBootstrapRefusesAnInstallationThatAlready
// Exists` in app_test.go is the case that makes that second half meaningful: it is
// the presence of a tenant row, and nothing else, that a second bootstrap refuses.

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestABootstrapThatRefusesALanguageWritesNoInstallation(t *testing.T) {
	path, cfg := configure(t)
	t.Setenv("PLATFORMKIT_BOOTSTRAP_PASSWORD", adminPass)

	err := bootstrap([]string{
		"--config", path, "--tenant", "acme", "--host", acmeHost,
		"--name", "Acme Corporation", "--admin-email", adminEmail,
		"--language", "de-DE",
	})
	if err == nil {
		t.Fatal("bootstrap accepted a language the installation has no copy for; " +
			"a tenant served in a language nobody wrote copy for is a page that declares it and shows another")
	}
	if !strings.Contains(err.Error(), "de-DE") {
		t.Fatalf("the refusal says %q; it has to name the language it refused, in the language the "+
			"operator wrote it in, or the deployment cannot find what to change", err)
	}

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	counts := map[string]int64{}
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		for _, table := range []string{"tenants", "tenant_hosts", "tenant_locales"} {
			var n int64
			if err := tx.DB().Table(table).Count(&n).Error; err != nil {
				return err
			}
			counts[table] = n
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the installation back: %v", err)
	}
	for _, table := range []string{"tenants", "tenant_hosts", "tenant_locales"} {
		if counts[table] != 0 {
			t.Errorf("the refused bootstrap left %d row(s) in %s: the transaction did not roll back, "+
				"and the installation is now half-created", counts[table], table)
		}
	}

	// The other half of "writes nothing": the deployment's corrected command still
	// works. A tenant row left behind would make this the second installation.
	install(t, path)
}

// TestBootstrapSaysItsLanguagesOnce: the flag is repeatable, and what it says arrives
// at the module's own command rather than at the table — so the same rules apply as
// apply to a request: duplicates are dropped, the spelling a person typed is
// canonicalised, and the row set is the default plus the distinct languages declared
// beside it (`tenant_locales` stores the default as a row of its own, which is what
// migrations/000027 backfills and what a create writes, and `localesOf` takes it out
// again on the way read).

func TestBootstrapSaysItsLanguagesOnce(t *testing.T) {
	path, cfg := configure(t)
	t.Setenv("PLATFORMKIT_BOOTSTRAP_PASSWORD", adminPass)
	err := bootstrap([]string{
		"--config", path, "--tenant", "acme", "--host", acmeHost,
		"--name", "Acme Corporation", "--admin-email", adminEmail,
		"--language", "pt-PT", "--language", "pt-PT", "--language", "EN",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	c := compose(cfg)

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var stored []string
	var defaultLocale string
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := tx.DB().Table("tenant_locales").Order("locale").Pluck("locale", &stored).Error; err != nil {
			return err
		}
		return tx.DB().Table("tenants").Limit(1).Pluck("default_locale", &defaultLocale).Error
	})
	if err != nil {
		t.Fatalf("read the languages back: %v", err)
	}
	if defaultLocale != "en" {
		t.Fatalf("the tenant's default = %q, want the language the installation's copy is written in",
			defaultLocale)
	}
	if !slices.Contains(stored, defaultLocale) {
		t.Errorf("the tenant's own default %q is not among its stored languages %v: a create writes it and "+
			"migrations/000027 backfills it, so a bootstrap that went through SetLocale must keep it",
			defaultLocale, stored)
	}
	if len(stored) != 2 || !slices.Contains(stored, "pt-PT") {
		t.Errorf("a bootstrap that said pt-PT twice and EN once stored %v, want [%s pt-PT]: the flag list "+
			"belongs to SetLocale, which deduplicates and canonicalises, and not to the table, where a "+
			"duplicate is a refused insert and \"EN\" is a language no file is ever read for",
			stored, defaultLocale)
	}

	// And what one request actually reads: the resolution every page negotiates over.
	var resolved tenancy.Tenant
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		resolved, err = c.tenants.ByHost(ctx, tx, acmeHost)
		return err
	})
	if err != nil {
		t.Fatalf("resolve the tenant the bootstrap created: %v", err)
	}
	if resolved.Languages == nil {
		t.Fatalf("the host resolution carries no languages: the tenant the bootstrap created is served in nobody's copy")
	}
	if got := resolved.Languages.Preferred(); !slices.Equal(got, []string{"en", "pt-PT"}) {
		t.Errorf("resolving %s offers %v, want [en pt-PT]: the default first, the declared language behind it, "+
			"the default nowhere twice", acmeHost, got)
	}
}
