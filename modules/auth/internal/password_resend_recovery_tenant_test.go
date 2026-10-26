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

func TestRecoveredPasswordResendIsScopedToItsTenant(t *testing.T) {
	for _, delivery := range []string{"marked", "unmarked"} {
		t.Run(delivery, func(t *testing.T) {
			admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
			mailer := &interruptedInvitationMailer{}
			_, _, svc := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) { d.Mailer = mailer })
			const address = "tenant-recovery@example.com"
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
			exec(t, admin, "UPDATE password_tokens SET sent_at = sent_at - interval '10 minutes'")
			if delivery == "unmarked" {
				exec(t, admin, "UPDATE password_tokens SET sent_at = NULL")
			}
			mailer.interrupt = true
			func() {
				defer func() {
					if recover() == nil {
						t.Error("replacement did not reach the interrupted transport")
					}
				}()
				if err := resend(); err != nil {
					t.Errorf("resend stopped before the transport: %v", err)
				}
			}()
			if err := resend(); err != nil {
				t.Fatal(err)
			}
			if mailer.attempts != 3 || len(mailer.box.Sent()) != 2 {
				t.Fatal("recovery did not deliver exactly one replacement")
			}
			token := authtest.TokenIn(mailer.box.Sent()[1].Body)
			if token == "" {
				t.Fatal("recovered message contains no credential")
			}
			snapshot := func() string {
				t.Helper()
				var state string
				if err := admin.QueryRowContext(t.Context(),
					"SELECT json_build_array((SELECT json_agg(p) FROM password_tokens p),"+
						" (SELECT count(*) FROM sessions), (SELECT count(*) FROM platformkit_outbox))::text").Scan(&state); err != nil {
					t.Fatal(err)
				}
				return state
			}
			before := snapshot()
			err := db.Run(deliveryContext(t.Context(), conn, globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				session, err := svc.Reset(ctx, tx, token, authtest.Password, nobody)
				if session != nil {
					t.Error("another tenant received a session from the recovered link")
				}
				return err
			})
			if !errors.Is(err, contracts.ErrCredentials) || snapshot() != before {
				t.Fatalf("foreign reset must refuse without changing credentials, sessions or events: %v", err)
			}
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				session, err := svc.Reset(ctx, tx, token, authtest.Password, nobody)
				if err == nil && session == nil {
					t.Error("owner received no session from the recovered link")
				}
				return err
			}); err != nil {
				t.Fatalf("foreign refusal consumed the owner's recovered link: %v", err)
			}
		})
	}
}
