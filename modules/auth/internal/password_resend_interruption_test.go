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

func TestInterruptedPasswordResendPreservesTheHeldCredential(t *testing.T) {
	for _, delivery := range []string{"marked", "unmarked"} {
		t.Run(delivery, func(t *testing.T) {
			admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
			mailer := &interruptedInvitationMailer{}
			notify, _ := notification.New(notification.Deps{})
			_, _, svc := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
				d.Mailer, d.Notify = mailer, notify
			})
			const address = "interrupted-resend@example.com"
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
				t.Fatal("initial offer did not deliver exactly one message")
			}
			token := authtest.TokenIn(mailer.box.Sent()[0].Body)
			if token == "" {
				t.Fatal("initial mail contains no credential")
			}
			exec(t, admin, "UPDATE password_tokens SET sent_at = sent_at - interval '10 minutes'")
			if delivery == "unmarked" {
				exec(t, admin, "UPDATE password_tokens SET sent_at = NULL")
			}
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
			mailer.interrupt = true
			var interrupted bool
			func() {
				defer func() { interrupted = recover() != nil }()
				if err := resend(); err != nil {
					t.Errorf("resend stopped before reaching the transport: %v", err)
				}
			}()
			if !interrupted || mailer.attempts != 2 || len(mailer.box.Sent()) != 1 {
				t.Fatal("replacement did not reach an interrupted transport without acceptance")
			}
			if snapshot() != before {
				t.Error("interrupted resend changed the held credential, notices or outbox")
			}
			other := deliveryContext(t.Context(), conn, globex)
			beforeReset := snapshot()
			for i, resetCtx := range []context.Context{other, ctx, ctx} {
				err := db.Run(resetCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					session, err := svc.Reset(ctx, tx, token, authtest.Password, nobody)
					if err != nil && session != nil {
						t.Error("refused reset returned a session")
					}
					if err == nil && session == nil {
						t.Error("accepted reset returned no session")
					}
					return err
				})
				if i == 1 && err != nil {
					t.Errorf("original link stopped working after interruption: %v", err)
				}
				if i != 1 && !errors.Is(err, contracts.ErrCredentials) {
					t.Errorf("other tenant or replay was not refused: %v", err)
				}
				if i == 0 && snapshot() != beforeReset {
					t.Error("another tenant's refused reset changed state")
				}
			}
		})
	}
}
