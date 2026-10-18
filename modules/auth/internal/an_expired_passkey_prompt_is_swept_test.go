package internal_test

// A ceremony row is a nonce with an expiry; the module's hourly Purge is what
// takes rows whose expiry has passed (sessions, tokens, first-factor windows).
// A passkey prompt nobody answered is one of those rows, and the sweep takes it
// once its window has closed — and leaves one that is still open.

import (
	"context"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestAnExpiredPasskeyPromptIsSwept(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, svc := mountOn(t, conn, auth.OIDC{})
	sweeper, ok := svc.(interface {
		Purge(context.Context, db.Tx[db.Tenant]) (int64, error)
	})
	if !ok {
		t.Fatal("the auth service has no Purge")
	}
	setPasskeySignIn(t, conn, true)
	for range 2 {
		if res := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", ""); res.Code != http.StatusOK {
			t.Fatalf("begin a passkey sign-in = %d %s", res.Code, res.Body.String())
		}
	}
	// Age one of the two: its window closed an hour ago.
	exec(t, admin, `UPDATE passkey_challenges SET created_at = now() - interval '2 hours', expires_at = now() - interval '1 hour'
		WHERE id = (SELECT id FROM passkey_challenges LIMIT 1)`)

	err := db.Run(tenancy.WithTenant(httpx.WithConn(t.Context(), conn), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := sweeper.Purge(ctx, tx)
		return err
	})
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if left := countRows(t, admin, "passkey_challenges"); left != 1 {
		t.Errorf("%d passkey prompts are left after the sweep, want 1: the expired one taken, the open one kept", left)
	}
}
