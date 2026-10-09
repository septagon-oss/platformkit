package internal_test

// A spent assertion id is transient security state: it exists to refuse the same
// assertion a second time, and it protects nothing past the moment the library would
// refuse the document anyway. So the row carries its own expiry — the assertion's
// NotOnOrAfter plus the library's clock skew, written by the claim — and the hourly
// sweep is what takes it.
//
// These are the two halves of that retention, which the sign-in cases cannot ask
// because a live assertion never has an expired row beside it: a row is taken when its
// own expiry has passed and not before, and one tenant's sweep touches no other
// tenant's row. The second half is the same RLS the sign-in depends on, seen from the
// one command in this module that runs over every tenant.

import (
	"context"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// rememberAssertion writes one spent row, in the tenant that owns it. Both timestamps
// arrive from the caller because the row's own CHECK refuses an expiry that does not
// follow its claim, which is the same rule the claim writes under.
func rememberAssertion(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, id string, claimedAt, expiresAt time.Time) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(
			"INSERT INTO saml_assertion_replays (tenant_id, assertion_id, claimed_at, expires_at) "+
				"VALUES (?, ?, ?, ?)", tenant.ID, id, claimedAt, expiresAt).Error
	})
	if err != nil {
		t.Fatalf("remembering %s's spent assertion %q: %v", tenant.Slug, id, err)
	}
}

// keptAssertion asks whether this tenant still holds a row for one assertion id. The
// read runs in the owning tenant's transaction, so the answer is what that tenant can
// see — which is the point of the second case below.
func keptAssertion(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, id string) bool {
	t.Helper()
	var kept int64
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM saml_assertion_replays WHERE assertion_id = $1", id).
			Scan(&kept).Error
	})
	if err != nil {
		t.Fatalf("reading %s's spent assertion %q: %v", tenant.Slug, id, err)
	}
	return kept > 0
}

func TestTheSweepTakesASpentAssertionOnlyAfterItsOwnExpiry(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	seed(t, conn, acme)
	svc := internal.NewService(realUsers(), nil, internal.Delivery{})
	// The two rows are the two sides of one column: either alone passes a purge that
	// deletes everything, or nothing.
	spent := db.Now()
	rememberAssertion(t, conn, acme, "spent-its-window", spent.Add(-10*time.Minute), spent.Add(-time.Minute))
	rememberAssertion(t, conn, acme, "window-still-open", spent, spent.Add(time.Hour))

	purge(t, t.Context(), conn, svc)

	if keptAssertion(t, conn, acme, "spent-its-window") {
		t.Error("the sweep left a row whose own expiry had passed: the assertion it protects is one " +
			"the library would already refuse, so the row is a growing table for nothing")
	}
	if !keptAssertion(t, conn, acme, "window-still-open") {
		t.Error("the sweep took a row whose expiry has not passed: the assertion is still presentable, " +
			"and the row is the only thing standing between it and a second session")
	}
}

func TestTheSweepsOneTenantTakesNoOtherTenantsSpentAssertions(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	seed(t, conn, acme)
	seed(t, conn, globex)
	svc := internal.NewService(realUsers(), nil, internal.Delivery{})
	// Both rows are past their window; only one tenant's turn arrives below.
	spent := db.Now().Add(-10 * time.Minute)
	rememberAssertion(t, conn, acme, "acme-spent", spent, spent.Add(9*time.Minute))
	rememberAssertion(t, conn, globex, "globex-spent", spent, spent.Add(9*time.Minute))

	purge(t, t.Context(), conn, svc)

	if keptAssertion(t, conn, acme, "acme-spent") {
		t.Error("acme's sweep left its own expired row")
	}
	if !keptAssertion(t, conn, globex, "globex-spent") {
		t.Error("acme's sweep deleted globex's row: the purge runs under the same row-level security " +
			"as every other statement in this module, and a per-tenant job is not a grant")
	}
}
