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

func TestDeliveredPasswordRotationKeepsOneSpendableReplacement(t *testing.T) {
	for _, mode := range []string{"delivery rollback and replay", "concurrent resends"} {
		t.Run(mode, func(t *testing.T) {
			admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
			_, _, svc := mountConfigured(t, conn, auth.OIDC{}, false)
			const address = "rotated-password-link@example.com"
			inviteThenCommit(t, conn, address)
			ctx := deliveryContext(t.Context(), conn, acme)
			resend := func(rollback bool) error {
				return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					if err := svc.Reissue(ctx, tx, address); err != nil {
						return err
					}
					if rollback {
						return errRollback
					}
					return nil
				})
			}
			if err := resend(false); err != nil {
				t.Fatal(err)
			}
			if got := len(mailbox.Sent()); got != 1 {
				t.Fatalf("initial messages = %d, want one", got)
			}
			original := authtest.TokenIn(mailbox.Sent()[0].Body)
			exec(t, admin, "UPDATE password_tokens SET sent_at = sent_at - interval '10 minutes'")
			if mode == "delivery rollback and replay" {
				if err := resend(true); !errors.Is(err, errRollback) {
					t.Fatalf("delivery rollback = %v", err)
				}
			} else {
				start, results := make(chan struct{}), make(chan error, 2)
				for range 2 {
					go func() {
						<-start
						results <- resend(false)
					}()
				}
				close(start)
				for range 2 {
					if err := <-results; err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := resend(false); err != nil {
				t.Fatalf("replay = %v", err)
			}
			if got := len(mailbox.Sent()); got != 2 {
				t.Fatalf("messages after rotation and replay = %d, want two", got)
			}
			replacement := authtest.TokenIn(mailbox.Sent()[1].Body)
			if original == "" || replacement == "" || original == replacement {
				t.Fatal("messages did not carry distinct credentials")
			}
			for _, token := range []string{original, replacement} {
				err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					session, err := svc.Reset(ctx, tx, token, authtest.Password, nobody)
					if err != nil && session != nil {
						t.Error("refused reset returned a session")
					}
					if err == nil && session == nil {
						t.Error("accepted reset returned no session")
					}
					return err
				})
				if token == original && !errors.Is(err, contracts.ErrCredentials) {
					t.Errorf("superseded link = %v, want ErrCredentials", err)
				}
				if token == replacement && err != nil {
					t.Errorf("mailed replacement is not spendable: %v", err)
				}
			}
		})
	}
}
