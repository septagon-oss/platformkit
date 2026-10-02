package internal_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

// TestWritingTheSameProviderTwiceSaysItOnce pins the sentence every command on
// this Service carries in its own contract: "Setting the same values again
// changes nothing and publishes nothing, as every other command here does."
//
// Nothing below origin/main's provider commit exercised it — the setup calls
// SetOIDC once per tenant and the trail case switches the issuer, which is a
// different values. Two operators applying the same configuration twice, an
// install script retried after a timeout, and a control plane that replays its
// last write on a 5xx all land here. The claim is that the trail reads as one
// change either way; the failure is a trail that says a company changed its
// identity provider when nobody did, which is the kind of entry that makes an
// audit trail something nobody trusts at 3am.
//
// The case is stated as counts, so it passes for any cure — a comparison in Go,
// a re-read of the row, a payload digest — and asks only that the number of
// events tracks the changes and not the calls. Clearing twice is the same
// question from the other side: the second clear finds no provider, refuses to
// act, and must therefore say nothing.
func TestWritingTheSameProviderTwiceSaysItOnce(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	acme, _ := twoTenants(t, conn)
	svc := internal.NewService(nil, tenanttest.InstallationLanguages())

	// The write that adds nothing: the tenant's own settings, handed back to it
	// unchanged, twice more.
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if _, err := svc.SetOIDC(ctx, tx, acme.ID, acmePeople); err != nil {
			return err
		}
		_, err := svc.SetOIDC(ctx, tx, acme.ID, acmePeople)
		return err
	})
	if err != nil {
		t.Fatalf("write acme's provider again: %v", err)
	}

	if got := providerEvents(t, admin, acme, contracts.EventOIDCSet); got != 1 {
		t.Errorf("the trail holds %d %s rows after three identical writes, want the one that changed anything",
			got, contracts.EventOIDCSet)
	}

	// The read still answers the same provider: nothing was written either.
	var issuer string
	err = db.Run(tenancy.WithTenant(t.Context(), acme.Tenancy()), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			settings, ok, err := svc.OIDCOf(ctx, tx)
			if err != nil || !ok {
				return err
			}
			issuer = settings.Issuer
			return nil
		})
	if err != nil {
		t.Fatalf("read acme's provider back: %v", err)
	}
	if issuer != acmePeople.Issuer {
		t.Errorf("after the repeated writes the tenant reads issuer %q, want %q", issuer, acmePeople.Issuer)
	}

	// And clearing: one clear publishes, the second finds nothing to clear and
	// says nothing.
	for range 2 {
		err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			_, err := svc.ClearOIDC(ctx, tx, acme.ID)
			return err
		})
		if err != nil {
			t.Fatalf("clear acme's provider: %v", err)
		}
	}
	if got := providerEvents(t, admin, acme, contracts.EventOIDCCleared); got != 1 {
		t.Errorf("the trail holds %d %s rows after two clears of one provider, want one",
			got, contracts.EventOIDCCleared)
	}
}

// providerEvents counts the trail rows one tenant holds for one event name. It
// reads the outbox as the trail itself is read — by name and by tenant — and not
// through any helper the command under test wrote.
func providerEvents(t *testing.T, admin *sql.DB, tenant *contracts.Tenant, name string) int {
	t.Helper()
	var n int
	err := admin.QueryRowContext(context.Background(),
		`SELECT count(*) FROM platformkit_outbox WHERE name = $1 AND tenant_id = $2`, name, tenant.ID).Scan(&n)
	if err != nil {
		t.Fatalf("count the %s rows: %v", name, err)
	}
	return n
}
