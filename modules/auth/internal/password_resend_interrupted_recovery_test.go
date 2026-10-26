package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestInterruptedPasswordResendAllowsImmediateRecovery(t *testing.T) {
	for _, delivery := range []string{"marked", "unmarked"} {
		t.Run(delivery, func(t *testing.T) {
			admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
			mailer := &interruptedInvitationMailer{}
			_, _, svc := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) { d.Mailer = mailer })
			const address = "interrupted-recovery@example.com"
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
			if len(mailer.box.Sent()) != 1 {
				t.Fatal("initial delivery did not send one message")
			}
			original := authtest.TokenIn(mailer.box.Sent()[0].Body)
			exec(t, admin, "UPDATE password_tokens SET sent_at = sent_at - interval '10 minutes'")
			if delivery == "unmarked" {
				exec(t, admin, "UPDATE password_tokens SET sent_at = NULL")
			}
			mailer.interrupt = true
			var interrupted bool
			func() {
				defer func() { interrupted = recover() != nil }()
				if err := resend(); err != nil {
					t.Errorf("resend did not reach the transport: %v", err)
				}
			}()
			if !interrupted || mailer.attempts != 2 || len(mailer.box.Sent()) != 1 {
				t.Fatal("replacement did not reach the interrupted transport")
			}
			var retained int
			if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM password_tokens WHERE token_hash = $1", contracts.Hash(original)).Scan(&retained); err != nil {
				t.Fatal(err)
			}
			if retained != 1 {
				t.Error("interruption removed the held credential")
			}
			if err := resend(); err != nil {
				t.Fatalf("immediate recovery: %v", err)
			}
			if err := resend(); err != nil {
				t.Fatalf("replayed recovery: %v", err)
			}
			if mailer.attempts != 3 || len(mailer.box.Sent()) != 2 {
				t.Fatal("recovery did not send exactly one replacement")
			}
			replacement := authtest.TokenIn(mailer.box.Sent()[1].Body)
			if original == "" || replacement == "" || original == replacement {
				t.Fatal("delivered messages did not contain distinct credentials")
			}
			for i, token := range []string{original, replacement, replacement} {
				err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					session, err := svc.Reset(ctx, tx, token, authtest.Password, nobody)
					if (err == nil) != (session != nil) {
						t.Error("reset returned a session inconsistent with its result")
					}
					return err
				})
				if i == 1 && err != nil {
					t.Errorf("recovered replacement was not spendable: %v", err)
				}
				if i != 1 && !errors.Is(err, contracts.ErrCredentials) {
					t.Errorf("superseded or spent credential was not refused: %v", err)
				}
			}
		})
	}
}
