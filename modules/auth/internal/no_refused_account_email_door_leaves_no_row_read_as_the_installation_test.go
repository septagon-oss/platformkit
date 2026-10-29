package internal_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// nothingAnEmailRefusalMayHold is what this flow's refusal is refused the right to
// write: the account, the token and the event, each counted as the installation counts
// it, under every name and whichever tenant id the row carries.
var nothingAnEmailRefusalMayHold = []string{"users", "password_tokens", "verification_tokens", "platformkit_outbox"}

// TestNoRefusedAccountEmailDoorLeavesNoRowReadAsTheInstallation asks the six account-email
// refusals of the installation rather than of the tenant that asked. A policy is silent
// about the rows it hides, so a count taken behind a tenant answers for the policy as
// much as for the refusal, and this flow is the one whose refusal must write nothing: no
// account, no token, no event. This package reads those tables as the tenant under test,
// and reads the outbox as the installation under four event names somebody expected;
// nobody reads all four tables unnamed, as one installation.
//
// Two premises are stated rather than assumed. The seeding has to leave rows the
// installation can count, or a zero would be the empty schema talking. And every door has
// to have been refused, because a request the deployment never answered leaves nothing
// behind for a different reason than the one under test.
func TestNoRefusedAccountEmailDoorLeavesNoRowReadAsTheInstallation(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, undeliverable)
	const known = "someone@acme.localhost"
	person(t, conn, known)

	before := installationRows(t, conn)
	if before["users"] == 0 || before["platformkit_outbox"] == 0 {
		t.Fatalf("the installation held %v after a person was seeded: with nothing to count, every comparison below answers for the policy rather than the refusal", before)
	}

	for _, email := range []string{"nobody@example.com", known} {
		for _, door := range []struct{ path, body string }{
			{"/api/v1/public/auth/register", approvalBody(t, email, nil)},
			{"/api/v1/public/auth/resend-verification", `{"email":"` + email + `"}`},
			{"/api/v1/public/auth/password/forgot", `{"email":"` + email + `"}`},
		} {
			res := call(t, router, http.MethodPost, door.path, door.body, sameSite)
			if res.Code != http.StatusServiceUnavailable {
				t.Fatalf("%s asked about %s answered %d, want the refusal this deployment owes", door.path, email, res.Code)
			}
		}
	}

	for table, was := range installationRows(t, conn) {
		if before[table] != was {
			t.Errorf("%s holds %d rows across every tenant where the installation held %d before a refused account-email door: a refusal that commits leaves an address in a table it was never spoken of", table, was, before[table])
		}
	}
}

// installationRows counts those tables the way the relay reads them, with no tenant
// setting between the count and the policy.
func installationRows(t *testing.T, conn *db.Conn) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		for _, table := range nothingAnEmailRefusalMayHold {
			var rows int64
			if err := tx.DB().Raw("SELECT count(*) FROM " + table).Row().Scan(&rows); err != nil {
				return err
			}
			out[table] = rows
		}
		return nil
	})
	if err != nil {
		t.Fatalf("count every tenant's rows: %v", err)
	}
	return out
}
