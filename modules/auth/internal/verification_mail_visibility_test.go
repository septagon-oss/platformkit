package internal_test

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
)

type visibilityMailer struct {
	admin   *sql.DB
	token   string
	visible bool
	err     error
}

func (m *visibilityMailer) Send(ctx context.Context, message notificationcontracts.Message) error {
	m.token = authtest.TokenIn(message.Body)
	if m.token == "" {
		return nil
	}
	m.err = m.admin.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM verification_tokens WHERE token_hash = $1)",
		authcontracts.Hash(m.token)).Scan(&m.visible)
	return nil
}

func TestVerificationMailCarriesACommittedCredential(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	mailer := &visibilityMailer{admin: admin}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup,
		func(deps *auth.Deps) { deps.Mailer = mailer })

	const address = "visible-verification@example.com"
	res := call(t, router, http.MethodPost, "/api/v1/public/auth/register", approvalBody(t, address, nil))
	if res.Code != http.StatusAccepted {
		t.Fatalf("registration = %d, want 202: %s", res.Code, res.Body.String())
	}
	worker(t, conn)
	if mailer.token == "" {
		t.Fatal("the worker sent no verification credential")
	}
	if mailer.err != nil {
		t.Fatalf("read the credential from a separate connection: %v", mailer.err)
	}

	var committed bool
	if err := admin.QueryRowContext(t.Context(),
		"SELECT EXISTS (SELECT 1 FROM verification_tokens WHERE token_hash = $1)",
		authcontracts.Hash(mailer.token)).Scan(&committed); err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("the worker did not commit the credential it mailed")
	}
	if !mailer.visible {
		t.Error("the mailer received the link before its credential was visible to another transaction")
	}
}
