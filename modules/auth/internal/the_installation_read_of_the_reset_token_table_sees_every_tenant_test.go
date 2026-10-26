package internal_test

// A refused account-email case compares one count before a call with the same count after, and a
// count is worth only what the reading behind it can see: taken as the tenant under test it answers
// for the row-level-security policy as much as for the write, while taken as the installation, which
// sets nothing between itself and the policy, it answers for the write alone. The outbox and the
// mail record each have a case for that premise; the reset-token table, which holds the "no token"
// half of "no account, no token, no outbox event", had none, and its count sits at zero before such
// a call and zero after it whatever the reading can reach. So this asks the premise directly.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// pendingResetToken files one row of password_tokens the way the module that mints the
// credential does: hashed, expiring an hour out, under the tenant named.
func pendingResetToken(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, person uuid.UUID) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(
			"INSERT INTO password_tokens (token_hash, tenant_id, user_id, expires_at) VALUES (?, ?, ?, now() + interval '60 minutes')",
			contracts.Hash(tenant.Slug+" reset token"), tenant.ID, person).Error
	})
	if err != nil {
		t.Fatalf("file a pending reset token for %s: %v", tenant.Slug, err)
	}
}

// TestTheInstallationReadOfTheResetTokenTableSeesEveryTenant counts that table twice over: as
// each tenant, which must show one row and not two, and as the installation, which must show
// both. The tenant half is what the package's own counts take; the installation half is what the
// refused-door cases take, and this is the premise they rest on and did not state.
func TestTheInstallationReadOfTheResetTokenTableSeesEveryTenant(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	seed(t, conn, globex)
	ada := person(t, conn, "ada@acme.localhost")
	bob := personAt(t, conn, globex, "bob@globex.localhost")
	pendingResetToken(t, conn, acme, ada)
	pendingResetToken(t, conn, globex, bob)

	held := func(tenant tenancy.Tenant) int64 {
		t.Helper()
		var rows int64
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Raw("SELECT count(*) FROM password_tokens").Row().Scan(&rows)
		})
		if err != nil {
			t.Fatalf("count password_tokens as %s: %v", tenant.Slug, err)
		}
		return rows
	}
	if own := held(acme); own != 1 {
		t.Errorf("acme counted %d pending reset tokens where it files one: a tenant that cannot read its own row cannot be shown to have left none behind", own)
	}
	if own := held(globex); own != 1 {
		t.Errorf("globex counted %d pending reset tokens where it files one: a count of zero here answers for the policy rather than for the row", own)
	}

	var all int64
	err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw("SELECT count(*) FROM password_tokens").Row().Scan(&all)
	})
	if err != nil {
		t.Fatalf("count password_tokens as the installation: %v", err)
	}
	if all != 2 {
		t.Errorf("the installation counted %d pending reset tokens where two tenants file one each: the read behind \"a refusal left no token\" must answer for every row of the table, whichever tenant id carries it", all)
	}
}
