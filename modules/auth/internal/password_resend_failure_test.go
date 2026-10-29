package internal_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestFailedPasswordResendPreservesTheDeliveredCredential(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	mailer := &verificationFailingMailer{}
	_, _, svc := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) { d.Mailer = mailer })
	const address = "retained-password-link@example.com"
	inviteThenCommit(t, conn, address)
	ctx := deliveryContext(t.Context(), conn, acme)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Reissue(ctx, tx, address)
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(mailer.box.Sent()); got != 1 {
		t.Fatalf("initial delivery sent %d messages, want one", got)
	}
	token := authtest.TokenIn(mailer.box.Sent()[0].Body)
	if token == "" {
		t.Fatal("initial delivery contains no credential")
	}
	// The first delivery is outside the resend cooldown but still inside its
	// one-hour lifetime. The next transport call refuses the replacement.
	exec(t, admin, "UPDATE password_tokens SET created_at = created_at - interval '10 minutes',"+
		" sent_at = sent_at - interval '10 minutes'")
	var before int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&before); err != nil {
		t.Fatal(err)
	}
	mailer.fail = true
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Reissue(ctx, tx, address)
	})
	if err == nil || mailer.token == "" || mailer.token == token {
		t.Fatalf("replacement did not reach a refused transport: %v", err)
	}
	var after, retained int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Errorf("refused resend changed outbox count from %d to %d", before, after)
	}
	if err := admin.QueryRowContext(t.Context(),
		"SELECT count(*) FROM password_tokens WHERE token_hash = $1 AND expires_at > clock_timestamp()",
		contracts.Hash(token)).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != 1 {
		t.Error("failed resend removed the unexpired credential already delivered to the person")
	}
	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		session, err := svc.Reset(ctx, tx, token, authtest.Password, nobody)
		if err == nil && session == nil {
			t.Error("successful reset returned no session")
		}
		return err
	})
	if err != nil {
		t.Errorf("the original mailed link stopped working after a refused resend: %v", err)
	}
}
