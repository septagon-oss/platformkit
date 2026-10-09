package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
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

func TestARefusedPasswordLeavesTheInvitationUsable(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users, box := realUsers(), &authtest.Mailbox{}
	svc := internal.NewService(users, nil, delivery(box))
	seed(t, conn, acme)
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := users.Invite(ctx, tx, "chooses.password@acme.example.com", "Chooses Password")
		if err != nil {
			return err
		}
		return svc.Offer(ctx, tx, u.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if sent := box.Sent(); len(sent) != 1 {
		t.Fatalf("invitation sent %d messages, want one", len(sent))
	}
	token := authtest.TokenIn(box.Sent()[0].Body)
	if token == "" {
		t.Fatal("invitation has no token")
	}

	// Compare committed rows, including every event, after the caller propagates
	// the refusal through the authoritative transaction as the HTTP handler does.
	snapshot := func() string {
		t.Helper()
		var state string
		row(t, admin, `SELECT json_build_object(
			'users', (SELECT json_agg(u ORDER BY id) FROM users u),
			'tokens', (SELECT json_agg(p ORDER BY user_id) FROM password_tokens p),
			'sessions', (SELECT json_agg(s ORDER BY id_hash) FROM sessions s),
			'events', (SELECT json_agg(e ORDER BY id) FROM platformkit_outbox e))::text`).Scan(&state)
		return state
	}
	before := snapshot()
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		session, err := svc.Reset(ctx, tx, token, "short", contracts.Client{})
		if session != nil {
			t.Error("a refused password returned a session")
		}
		return err
	})
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("short password = %v, want invalid", err)
	}
	if snapshot() != before {
		t.Error("a refused password changed committed users, tokens, sessions or events")
	}

	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		session, err := svc.Reset(ctx, tx, token, authtest.Password, contracts.Client{})
		if err == nil && session == nil {
			t.Error("accepting the invitation returned no session")
		}
		return err
	}); err != nil {
		t.Fatalf("retrying the same invitation with a valid password: %v", err)
	}
	if len(box.Sent()) != 1 {
		t.Error("retrying acceptance sent another invitation")
	}
}
