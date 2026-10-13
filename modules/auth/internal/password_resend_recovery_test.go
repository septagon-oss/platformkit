package internal_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestFailedPasswordResendAllowsImmediateRecovery(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	mailer := &verificationFailingMailer{}
	_, _, svc := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) { d.Mailer = mailer })
	const address = "resend-recovery@example.com"
	inviteThenCommit(t, conn, address)
	ctx := deliveryContext(t.Context(), conn, acme)
	resend := func() error {
		return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return svc.Reissue(ctx, tx, address)
		})
	}
	if err := resend(); err != nil {
		t.Fatal(err)
	}
	original := authtest.TokenIn(mailer.box.Sent()[0].Body)
	exec(t, admin, "UPDATE password_tokens SET sent_at = sent_at - interval '10 minutes'")
	snapshot := func() string {
		t.Helper()
		var state string
		if err := admin.QueryRowContext(t.Context(),
			"SELECT json_build_array((SELECT json_agg(p) FROM password_tokens p),"+
				" (SELECT count(*) FROM notifications), (SELECT count(*) FROM platformkit_outbox))::text").Scan(&state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	before := snapshot()
	mailer.fail = true
	for range 2 {
		err := resend()
		if err == nil || mailer.token == "" || mailer.token == original {
			t.Fatalf("replacement did not reach a refused transport: %v", err)
		}
		if strings.Contains(err.Error(), mailer.token) {
			t.Error("delivery error exposed the replacement credential")
		}
		if snapshot() != before {
			t.Error("refused resend changed the credential, notices or outbox")
		}
	}
	mailer.fail = false
	if err := resend(); err != nil {
		t.Fatalf("immediate recovery: %v", err)
	}
	if err := resend(); err != nil {
		t.Fatalf("replay after recovery: %v", err)
	}
	if got := len(mailer.box.Sent()); got != 2 {
		t.Fatalf("accepted messages = %d, want the original and one replacement", got)
	}
	replacement := authtest.TokenIn(mailer.box.Sent()[1].Body)
	for i, token := range []string{original, replacement, replacement} {
		err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			session, err := svc.Reset(ctx, tx, token, authtest.Password, nobody)
			if err != nil && session != nil {
				t.Error("refused reset returned a session")
			}
			return err
		})
		if i == 1 && err != nil {
			t.Errorf("recovered replacement could not be spent: %v", err)
		}
		if i != 1 && !errors.Is(err, contracts.ErrCredentials) {
			t.Errorf("superseded or already spent credential: %v", err)
		}
	}
}
