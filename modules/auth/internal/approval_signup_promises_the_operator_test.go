package internal_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// TestApprovalSignupStaysOpenWhereAnEmailDoorRefuses is the one public /register that
// a mail-less installation still answers with an acceptance, mounted beside the doors
// that refuse: the same installation, the same carrier that says it files rather than
// dials, the same path. The three modes of that path differ in what their
// acknowledgment promises a person. Email and invitation sign-up answer 202 for a
// confirmation link, so the installation that cannot mail refuses them; approval
// sign-up answers 202 for an operator's decision, and the account it commits already
// holds the password its person chose — the credential goes to the user owner inside
// the request, and Service.Offer sends a set-password link only to a person in
// user.invited, never to this pending one. So nothing is owed to that person by mail,
// and the door that owes nothing stays open.
//
// The case is the boundary rather than either half of it: an installation that also
// refused here would be refusing a promise it can keep, and one that accepted the
// emailed sign-ups would be answering 202 for a link that will never arrive.
func TestApprovalSignupStaysOpenWhereAnEmailDoorRefuses(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, approvalSignup, undeliverable)
	before := countsByTable(t, conn)

	res := call(t, router, http.MethodPost, "/api/v1/public/auth/register",
		approvalBody(t, "pending@acme.localhost", nil), sameSite)
	if res.Code != http.StatusAccepted {
		t.Fatalf("approval signup in a mail-less installation answered %d, want the acceptance its acknowledgment honours", res.Code)
	}

	// The account exists, is pending, and can already prove who it is with the
	// password it was given: this is the assertion that makes the door's promise one
	// the installation keeps.
	held := account(t, conn, "pending@acme.localhost")
	if held.Status != user.StatusPending {
		t.Errorf("the accepted signup holds status %q, want the pending state an operator decides on", held.Status)
	}
	if !held.CheckPassword(authtest.Password) {
		t.Error("the accepted signup holds no password, so its person would need a link this installation cannot send")
	}

	// And nothing that promises a mail was written: none of the account-email events,
	// nothing handed to the carrier, no delivery record, no token of any kind. The
	// pending account is the only row this request is allowed to leave.
	if n := eventsPublished(t, conn); n != 0 {
		t.Errorf("approval signup published %d account-email events, want none: this door promises an operator, not a mail", n)
	}
	if n := len(mailbox.Sent()); n != 0 {
		t.Errorf("approval signup handed %d messages to the carrier, want none", n)
	}
	after := countsByTable(t, conn)
	if after["users"] != before["users"]+1 {
		t.Errorf("users holds %d rows where it held %d: an accepted signup owes exactly one account", after["users"], before["users"])
	}
	for _, table := range []string{"password_tokens", "verification_tokens", "direct_mail_deliveries"} {
		if after[table] != before[table] {
			t.Errorf("%s holds %d rows where it held %d before a signup that mails nothing", table, after[table], before[table])
		}
	}

	// The same installation, the same moment: a door whose acknowledgment is a link
	// refuses. One value, one answer, and the difference between the two doors is
	// what they promised, not which one happened to be asked.
	if got := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot",
		`{"email":"pending@acme.localhost"}`, sameSite); got.Code != http.StatusServiceUnavailable {
		t.Errorf("the forgotten-password door answered %d in the installation that accepted the approval signup, want the refusal of a link nothing will send", got.Code)
	}
}

// account reads the row a public signup left, as the tenant under test.
func account(t *testing.T, conn *db.Conn, email string) *user.User {
	t.Helper()
	var held *user.User
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		held, err = realUsers().ByEmail(ctx, tx, email)
		return err
	})
	if err != nil {
		t.Fatalf("read the account a signup committed: %v", err)
	}
	return held
}
