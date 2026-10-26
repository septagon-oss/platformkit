package internal_test

import (
	"context"
	"io/fs"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestFailedPasswordResendPreservesCredentialsFromBeforeDeliveryTracking(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	mailer := &verificationFailingMailer{}
	_, _, svc := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) { d.Mailer = mailer })
	const address = "legacy-password-link@example.com"
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
	if got := len(mailer.box.Sent()); got != 1 {
		t.Fatalf("initial messages = %d, want one", got)
	}
	token := authtest.TokenIn(mailer.box.Sent()[0].Body)
	if token == "" {
		t.Fatal("delivered message contains no credential")
	}
	// Restore the table shape that held the delivered link before the upgrade,
	// then apply the delivery-column migration to that populated table.
	exec(t, admin, "UPDATE password_tokens SET created_at = created_at - interval '10 minutes'")
	exec(t, admin, "ALTER TABLE password_tokens DROP COLUMN sent_at")
	migration, err := fs.ReadFile(auth.Migrations.Files, "000055_password_reset_sent_at.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, admin, string(migration))
	var before int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&before); err != nil {
		t.Fatal(err)
	}
	mailer.fail = true
	if err := resend(); err == nil || mailer.token == "" || mailer.token == token {
		t.Fatalf("replacement did not reach a refused transport: %v", err)
	}
	var after, retained int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("refused resend changed outbox count from %d to %d", before, after)
	}
	if err := admin.QueryRowContext(t.Context(),
		"SELECT count(*) FROM password_tokens WHERE token_hash = $1 AND expires_at > clock_timestamp()",
		contracts.Hash(token)).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != 1 {
		t.Error("failed resend removed the unexpired credential delivered before delivery tracking")
	}
	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		session, err := svc.Reset(ctx, tx, token, authtest.Password, nobody)
		if err == nil && session == nil {
			t.Error("successful reset returned no session")
		}
		return err
	})
	if err != nil {
		t.Errorf("previously delivered link stopped working after a refused resend: %v", err)
	}
}
