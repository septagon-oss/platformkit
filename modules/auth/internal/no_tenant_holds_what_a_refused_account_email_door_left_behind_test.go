package internal_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// TestNoTenantHoldsWhatARefusedAccountEmailDoorLeftBehind reads what the installation
// wrote, not what the caller's tenant may see. A policy is silent about a row it hides,
// and a record or an outbox event filed under a tenant that never asked is that kind of
// row: one address in a table it was never spoken of, for a mail that never went. The
// refusals are reported without stopping the case, so the read of the tables happens
// whichever way the doors answered.
func TestNoTenantHoldsWhatARefusedAccountEmailDoorLeftBehind(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.Mails = mailLedger()
		undeliverable(d)
	})
	records, events := countRecordsAndEvents(t, conn)
	for _, door := range []struct{ path, body string }{
		{"/api/v1/public/auth/register", approvalBody(t, "nobody@example.com", nil)},
		{"/api/v1/public/auth/resend-verification", `{"email":"nobody@example.com"}`},
		{"/api/v1/public/auth/password/forgot", `{"email":"nobody@example.com"}`},
	} {
		res := call(t, router, "POST", door.path, door.body, sameSite)
		if res.Code != http.StatusServiceUnavailable ||
			!strings.Contains(res.Body.String(), authtest.NoMailDetail) {
			t.Errorf("%s answered %d %q, want the refusal the deployment answers with", door.path, res.Code, res.Body.String())
		}
	}
	left, leftEvents := countRecordsAndEvents(t, conn)
	if left != records || leftEvents != events {
		t.Errorf("a refused door left %d mail records and %d account-email events where it found %d and %d",
			left, leftEvents, records, events)
	}
}

// countRecordsAndEvents is the record table and the account-email outbox rows read as the
// kernel reads them, with no tenant setting between the count and the policy, so a row
// answers whichever tenant id carries it.
func countRecordsAndEvents(t *testing.T, conn *db.Conn) (records, events int64) {
	t.Helper()
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := tx.DB().Raw("SELECT count(*) FROM direct_mail_deliveries").Row().Scan(&records); err != nil {
			return err
		}
		return tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE name IN ?", refusedEvents).Row().Scan(&events)
	})
	if err != nil {
		t.Fatalf("count the rows of every tenant: %v", err)
	}
	return records, events
}
