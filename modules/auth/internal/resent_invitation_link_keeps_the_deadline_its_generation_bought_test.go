package internal_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// A first send inserts the credential row; a resend moves the invitation to a new
// deadline and updates that same row, keyed by tenant and person. Every case that
// reads a mailed link reads the first send, so none sees what that update leaves
// behind: a resent body naming a moment already past, with acceptance refused
// inside the hour it promised. Reading the credential row, the invitation row and
// the body of the link a resend mails, keyed by the token that link carries, names
// the drift; under a build that states a stale deadline for a resent generation
// alone, this is the only case in the package that refuses it.
func TestResentInvitationLinkKeepsTheDeadlineItsGenerationBought(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	authtest.InvitationOffer(t, f.InvitationFixture, u)
	first := u.Invitation.ExpiresAt
	f.Advance(contracts.ResetInterval)
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.Commands.ResendInvitation(ctx, tx, u.ID, u.Invitation.Revision)
		return err
	}); err != nil {
		t.Fatalf("the resend returned %v, want the current revision accepted", err)
	}
	var resent *usercontracts.User
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		resent, err = f.Users.Get(ctx, tx, u.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	token := authtest.InvitationOffer(t, f.InvitationFixture, resent)
	body := f.Mail.Sent()[len(f.Mail.Sent())-1].Body
	if token == "" || !strings.Contains(body, "?token=") {
		t.Fatalf("%q mailed for the resent generation, want a link carrying its credential", body)
	}
	var kept, allowed time.Time
	var generation int64
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT t.expires_at, p.invitation_expires_at, t.invitation_generation FROM password_tokens t"+
			" JOIN users p ON p.id = t.user_id WHERE t.token_hash = ?", contracts.Hash(token)).Row().Scan(&kept, &allowed, &generation)
	}); err != nil {
		t.Fatalf("the credential the resent link carries is not in the table: %v", err)
	}
	stated := kept.UTC().Format(time.RFC3339)
	if generation != resent.Invitation.Generation || !kept.Equal(allowed) ||
		!kept.Equal(first.Add(contracts.ResetInterval)) || !strings.Contains(body, stated) {
		t.Errorf("the resent link carries a credential that keeps %s while the invitation allows %s and the body names %v,"+
			" want one deadline %s for row, invitation and body alike", stated, allowed.UTC().Format(time.RFC3339),
			resent.Invitation.ExpiresAt.UTC().Format(time.RFC3339), first.Add(contracts.ResetInterval).UTC().Format(time.RFC3339))
	}
}
