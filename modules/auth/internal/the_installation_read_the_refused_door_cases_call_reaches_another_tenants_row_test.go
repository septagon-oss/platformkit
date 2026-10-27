package internal_test

// The refused-account-email cases compare installationRows before a call with the same reading after,
// and their claim holds only while that reading reaches a row whatever tenant id carries it: taken
// behind a tenant, the row-level-security policy hides the row, so a count of zero answers for the
// policy as much as for the write. The premise is pinned for the tables the delivery's own helper
// reads, but not for these two helpers, which is what those cases actually call. A helper that took
// the tenant setting instead of the system one would leave every assertion in them true and the
// brief's "no token, no outbox event" unproved. So this asks the premise of the helpers themselves,
// on the two token tables: a row filed for one tenant must move the installation's count and not the
// tenant the refused doors ask about.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

func TestTheInstallationReadTheRefusedDoorCasesCallReachesAnotherTenantsRow(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	seed(t, conn, acme)
	seed(t, conn, globex)
	person(t, conn, "ada@acme.localhost")
	bob := personAt(t, conn, globex, "bob@globex.localhost")

	before, behindA := installationRows(t, conn), countsByTable(t, conn)
	pendingResetToken(t, conn, globex, bob)
	if err := db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`INSERT INTO verification_tokens (token_hash, tenant_id, user_id, email, expires_at)
			VALUES (?, ?, ?, ?, now() + interval '60 minutes')`,
			contracts.Hash("a verification link"), globex.ID, bob, "bob@globex.localhost").Error
	}); err != nil {
		t.Fatalf("file a pending verification for globex: %v", err)
	}

	for _, table := range []string{"password_tokens", "verification_tokens"} {
		if grew := installationRows(t, conn)[table] - before[table]; grew != 1 {
			t.Errorf("the installation's count of %s moved by %d where one tenant filed one row: the reading the refused-door cases compare does not reach another tenant's row, so the zero they assert would answer for a policy rather than for the refusal", table, grew)
		}
		if moved := countsByTable(t, conn)[table] - behindA[table]; moved != 0 {
			t.Errorf("acme's count of %s moved by %d on a row only globex has: the tenant reading is not the narrow one the before-and-after comparisons take", table, moved)
		}
	}
}
