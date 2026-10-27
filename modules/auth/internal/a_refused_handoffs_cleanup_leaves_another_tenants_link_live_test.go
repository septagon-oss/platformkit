package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// The cleanup a refused handoff makes is one DELETE on password_tokens naming the
// person, the invitation generation, and no tenant. What bounds that statement inside
// another customer's rows is the table's own row-level security policy — the same guard
// the module's hourly purge leans on — so this asks the tenancy question of that one
// statement: one tenant's refusal retires the credential its own refused handoff minted
// and leaves the identically-keyed credential of another tenant standing and spendable.
// Both people are invited at the same generation number, which is what makes the key one
// the DELETE could act on, and nothing is spent and no clock moves before the counts, so
// a row in the table is a usable link.
func TestARefusedHandoffsCleanupLeavesAnotherTenantsLinkLive(t *testing.T) {
	f := invitationSQL(t)
	globex := globexSession(t, f)
	abroad := invitedIn(t, f, globex, "one.person@example.com")
	abroadToken := offeredIn(t, f, globex, abroad)
	if abroadToken == "" {
		t.Fatal("the link mailed in the other tenant carries no credential, so no credential there could be reached by a cleanup here")
	}
	refused := invitedIn(t, f, f.ctx, "one.person@acme.example.com")
	if refused.Invitation.Generation != abroad.Invitation.Generation {
		t.Fatalf("the two invited people hold generations %d and %d, want the same generation number the cleanup keys on",
			refused.Invitation.Generation, abroad.Invitation.Generation)
	}
	f.Mail.Failure = errors.New("smtp 421 4.7.0 the relay would not take this tenant's mail")
	minted := offeredIn(t, f, f.ctx, refused)
	f.Mail.Failure = nil
	if minted == "" {
		t.Fatal("the refused handoff mailed nothing at all, so no cleanup of a minted credential ran here")
	}
	if gone := liveTokens(t, f, f.ctx, refused.ID); gone != 0 {
		t.Errorf("%d credentials of %s stand after their own handoff was refused, want the credential that handoff minted deleted",
			gone, refused.Invitation.Recipient)
	}
	if kept := liveTokens(t, f, globex, abroad.ID); kept != 1 {
		t.Errorf("%d credentials of %s stand in the other tenant after this tenant's handoff was refused, want the link mailed there untouched",
			kept, abroad.Invitation.Recipient)
	}
	var got *user.User
	if err := db.Run(globex, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := f.Auth.Reset(ctx, tx, abroadToken, "a sufficiently strong password", contracts.Client{}); err != nil {
			return err
		}
		var err error
		got, err = f.Users.Get(ctx, tx, abroad.ID)
		return err
	}); err != nil {
		t.Fatalf("%s spent the link mailed in the other tenant and got %v, want that acceptance to go through",
			abroad.Invitation.Recipient, err)
	}
	if got.Status != user.StatusActive || got.Invitation.Phase != user.InvitationAccepted {
		t.Errorf("%s reads status %q in phase %q after spending the link mailed in their tenant, want an accepted membership",
			abroad.Invitation.Recipient, got.Status, got.Invitation.Phase)
	}
}

// globexSession is this fixture's transaction door into a second tenant, with an
// administrator of that tenant behind it: an invitation is only asked for by someone who
// can grant one.
func globexSession(t *testing.T, f invitationSQLFixture) context.Context {
	t.Helper()
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), globex), f.conn)
	actor := &user.User{Email: "administrator@globex.example.com", Status: user.StatusActive, Roles: user.Roles{"admin"}}
	if err := db.Run(ctx, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return crud.Create(ctx, tx, actor)
	}); err != nil {
		t.Fatalf("the other tenant's administrator could not be written: %v", err)
	}
	return tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: actor.ID})
}

// invitedIn invites one person at the chosen address through the door handed to it and
// answers with the person as committed.
func invitedIn(t *testing.T, f invitationSQLFixture, ctx context.Context, email string) *user.User {
	t.Helper()
	var u *user.User
	if err := db.Run(ctx, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		u, err = f.Commands.CreateInvitation(ctx, tx, user.InvitationIntent{Email: email, Roles: []string{"editor"}})
		return err
	}); err != nil {
		t.Fatalf("the invitation of %s returned %v, want a pending person a link can be mailed to", email, err)
	}
	return u
}

// offeredIn asks for one handoff of that person's current invitation generation through
// the door handed to it, and answers with the credential its mailed link carries or ""
// when no message left. The fake records a message before it refuses it, so a credential
// here means a link was minted whether or not the transport took the send.
func offeredIn(t *testing.T, f invitationSQLFixture, ctx context.Context, u *user.User) string {
	t.Helper()
	before := len(f.Mail.Sent())
	if err := db.Run(ctx, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return f.Links.OfferInvitation(ctx, tx, u.ID, u.Invitation.Generation, *u.Invitation.DeliveryID)
	}); err != nil {
		t.Fatalf("the handoff of %s returned %v, want the send taken or refused rather than the command failed", u.Email, err)
	}
	sent := f.Mail.Sent()
	if len(sent) <= before {
		return ""
	}
	return authtest.TokenIn(sent[len(sent)-1].Body)
}

// liveTokens counts the set-password rows one tenant can see for one of its own people:
// each is a link that has not been spent, because spending deletes the row.
func liveTokens(t *testing.T, f invitationSQLFixture, ctx context.Context, id uuid.UUID) int64 {
	t.Helper()
	var rows int64
	if err := db.Run(ctx, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FROM password_tokens WHERE user_id = $1`, id).Row().Scan(&rows)
	}); err != nil {
		t.Fatal(err)
	}
	return rows
}
