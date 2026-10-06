package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestAnInvitationLeavesOnlyAfterItsTokenCommits(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		name := "commit"
		if rollback {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
			users, box := realUsers(), &authtest.Mailbox{}
			svc := internal.NewService(users, nil, delivery(box))
			seed(t, conn, acme)
			ctx := tenancy.WithTenant(t.Context(), acme)
			var person uuid.UUID
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				u, err := users.Invite(ctx, tx, "invited@acme.example.com", "Invited")
				if err == nil {
					person = u.ID
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}

			// The invitation event's consumer runs after the user has committed.
			// Keep its issuing transaction open using the callback's lifetime,
			// without delaying a process or relying on scheduler timing.
			err := db.Run(ctx, conn, func(issuing context.Context, tx db.Tx[db.Tenant]) error {
				if err := svc.Offer(issuing, tx, person); err != nil {
					return err
				}
				if sent := box.Sent(); len(sent) != 0 {
					t.Errorf("%d invitation mails escaped before the token committed", len(sent))
					// Use the original context, not issuing: this is the recipient's
					// independent transaction, exactly as a second HTTP request is.
					redeemErr := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
						_, err := svc.Reset(ctx, tx, authtest.TokenIn(sent[0].Body), authtest.Password, contracts.Client{})
						return err
					})
					t.Logf("recipient spending the already-visible link before commit: %v", redeemErr)
				}
				if rollback {
					return errRollback
				}
				return nil
			})
			if rollback {
				if !errors.Is(err, errRollback) {
					t.Fatalf("rollback: %v", err)
				}
				var tokens int
				row(t, admin, "SELECT count(*) FROM password_tokens").Scan(&tokens)
				if tokens != 0 || len(box.Sent()) != 0 {
					t.Errorf("rolled-back issuance left %d tokens and sent %d unusable invitation mails; want neither", tokens, len(box.Sent()))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(box.Sent()) != 1 {
				t.Fatalf("committed invitation sent %d mails, want one", len(box.Sent()))
			}
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				session, err := svc.Reset(ctx, tx, authtest.TokenIn(box.Sent()[0].Body), authtest.Password, contracts.Client{})
				if err == nil && session == nil {
					t.Error("committed invitation opened no session")
				}
				return err
			}); err != nil {
				t.Fatalf("spend committed invitation: %v", err)
			}
		})
	}
}
