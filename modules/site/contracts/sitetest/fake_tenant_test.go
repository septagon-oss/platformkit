package sitetest_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/site/contracts/sitetest"
)

// The two tenants one fake is driven as, and the fixture's own tenant-less
// corner. Row-level security gives each tenant its own settings row; the fake
// claims the same, and this is the case that says whether it does.
var (
	tenantA = tenancy.Tenant{ID: uuid.New(), Slug: "a", Name: "A"}
	tenantB = tenancy.Tenant{ID: uuid.New(), Slug: "b", Name: "B"}
)

// TestTheFakeKeepsEachTenantOnItsOwnRow is the floor contenttest's fake got from
// review 1's finding 4 and this package did not: sitetest.Fake kept one
// *contracts.SiteSettings and read the tenant on the context nowhere, so one
// instance handed any tenant the settings another tenant had saved, while its
// Skip[porttest.Elsewhere] reason told the harness there was no second tenant
// here to be refused.
//
// It reaches the same rule as the real service from the other side: a tenant
// reads and writes its own row and nothing else's. The tenant-less context a
// fixture seeds with is its own row too — which is why the suite, whose fixture
// names no tenant, still runs against this fake.
func TestTheFakeKeepsEachTenantOnItsOwnRow(t *testing.T) {
	fake := sitetest.NewFake()
	ctx := t.Context()

	// Reachability: a tenant reads back what it saved. Without this leg, a fake
	// that answered every tenant with nothing would pass everything below.
	save(t, fake, with(ctx, tenantA), "Acme Journal")
	if got := read(t, fake, with(ctx, tenantA)); got.Title != "Acme Journal" {
		t.Fatalf("tenant A read title=%q after saving %q; a partition nobody can write is not a partition",
			got.Title, "Acme Journal")
	}

	// The finding: B reads A's row as the database would return it — nothing.
	if got := read(t, fake, with(ctx, tenantB)); got.Title != "" {
		t.Errorf("tenant B read title=%q tagline=%q; tenant A saved that, and row-level security "+
			"gives the real service one settings row per tenant, which the fake claims too",
			got.Title, got.Tagline)
	}

	// And B's own save lands in B's row, not A's: two tenants over one fake
	// neither share a row nor lose one.
	save(t, fake, with(ctx, tenantB), "Other Journal")
	if got := read(t, fake, with(ctx, tenantA)); got.Title != "Acme Journal" {
		t.Errorf("tenant A read title=%q after tenant B saved; one tenant's save overwrote another's row", got.Title)
	}
	if got := read(t, fake, with(ctx, tenantB)); got.Title != "Other Journal" {
		t.Errorf("tenant B read title=%q after saving its own; a partition that cannot be written is empty in name only", got.Title)
	}

	// The fixture's corner is a partition of its own, not everybody's: it is what
	// lets a case seed without building a request, and it is not a hole.
	if got := read(t, fake, ctx); got.Title != "" {
		t.Errorf("a context naming no tenant read title=%q; the two tenants above each saved a row and the "+
			"fixture's own corner is a third", got.Title)
	}
}

func with(ctx context.Context, tenant tenancy.Tenant) context.Context {
	return tenancy.WithTenant(ctx, tenant)
}

func save(t *testing.T, fake *sitetest.Fake, ctx context.Context, title string) {
	t.Helper()
	if _, err := fake.Save(ctx, db.Tx[db.Tenant]{}, &contracts.SiteSettings{
		Title: title, HomeSlug: "home", Theme: "system", PrimaryColor: "#0000aa",
	}); err != nil {
		t.Fatalf("save %q: %v", title, err)
	}
}

func read(t *testing.T, fake *sitetest.Fake, ctx context.Context) contracts.SiteSettings {
	t.Helper()
	got, err := fake.Settings(ctx, db.Tx[db.Tenant]{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return *got
}
