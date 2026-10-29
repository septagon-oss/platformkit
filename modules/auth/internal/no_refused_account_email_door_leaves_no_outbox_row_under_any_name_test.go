package internal_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// TestNoRefusedAccountEmailDoorLeavesNoOutboxRowUnderAnyName counts every row of the
// outbox rather than the four names the refused-door cases name. The invariant is that
// a refusal commits no event, and four names are a list of what this flow was expected
// to write: an event filed under a fifth name, one this module declares and nobody
// counted, would leave every count here at zero and still hand a worker a row. The
// accepting-side case asks whether the counted names are ones an accepting door files;
// this asks the other half, which is whether anything outside them appears.
//
// Each door is asked about an address nobody has and about one somebody has, since an
// address somebody holds is the only way the sign-in-link door is reached at all. Both
// counts come from the same installation as the tenant under test, so a row the seeding
// owed is not counted against a refusal.
func TestNoRefusedAccountEmailDoorLeavesNoOutboxRowUnderAnyName(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, undeliverable)
	const known = "someone@acme.localhost"
	person(t, conn, known)
	before := outboxRows(t, conn)
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
	if after := outboxRows(t, conn); after != before {
		t.Errorf("refused account-email doors left %d outbox rows where the installation held %d: one row under any name is an event a worker will read", after, before)
	}
}

// outboxRows counts every outbox row the tenant under test can see, under every name.
func outboxRows(t *testing.T, conn *db.Conn) int64 {
	t.Helper()
	var rows int64
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM platformkit_outbox").Row().Scan(&rows)
	})
	if err != nil {
		t.Fatalf("count the outbox rows: %v", err)
	}
	return rows
}
