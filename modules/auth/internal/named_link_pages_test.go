package internal_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
)

// A composition that names its pages has the invitation, the reset and the
// confirmation mailed to them on the tenant's host, and the reset notice leads to
// the same set-password page with no token on it.
func TestNamedPagesAreWhereTheMailedLinksAndTheResetNoticeLead(t *testing.T) {
	const setPassword, confirmEmail = "/account/password", "/account/confirm"
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.LinkPages = auth.LinkPages{SetPassword: setPassword, ConfirmEmail: confirmEmail}
	})

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := realUsers().Invite(ctx, tx, "grace@acme.localhost", "")
		return err
	})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	worker(t, conn)
	leadsTo(t, "the invitation", mailTo(t, "grace@acme.localhost"), "http://"+host+setPassword+"?token=")

	person(t, conn, "ada@acme.localhost")
	if res := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", `{"email":"ada@acme.localhost"}`); res.Code != http.StatusOK {
		t.Fatalf("asking for the link = %d, want 200", res.Code)
	}
	worker(t, conn)
	leadsTo(t, "the reset", mailTo(t, "ada@acme.localhost"), "http://"+host+setPassword+"?token=")
	for _, n := range notices.Sent() {
		if n.Link != setPassword {
			t.Errorf("a set-password notice leads to %q, want %q and no token", n.Link, setPassword)
		}
	}
	if len(notices.Sent()) == 0 {
		t.Error("no notice was raised beside the reset mail")
	}

	if res := call(t, router, http.MethodPost, "/api/v1/public/auth/register", approvalBody(t, "new@acme.localhost", nil)); res.Code != http.StatusAccepted {
		t.Fatalf("signing up = %d, want 202", res.Code)
	}
	worker(t, conn)
	leadsTo(t, "the confirmation", mailTo(t, "new@acme.localhost"), "http://"+host+confirmEmail+"?token=")
}

// mailTo is the last message the mailbox holds for one address.
func mailTo(t *testing.T, to string) notificationcontracts.Message {
	t.Helper()
	sent := mailbox.Sent()
	for i := len(sent) - 1; i >= 0; i-- {
		if sent[i].To == contracts.EmailKey(to) {
			return sent[i]
		}
	}
	t.Fatalf("no mail went to %s; the mailbox holds %d", to, len(sent))
	return notificationcontracts.Message{}
}

// leadsTo checks a mail carries one live link and that it opens the named page.
func leadsTo(t *testing.T, what string, m notificationcontracts.Message, link string) {
	t.Helper()
	if !strings.Contains(m.Body, link) || len(authtest.TokenIn(m.Body)) != 43 {
		t.Errorf("%s mail does not lead to %s with a token:\n%s", what, link, m.Body)
	}
}
