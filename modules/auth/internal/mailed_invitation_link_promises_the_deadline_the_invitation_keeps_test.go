package internal_test

import (
	"context"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
)

// What a mailed invitation link promises has to be the moment the invitation
// itself stops, not merely the moment the credential it minted keeps: acceptance
// is refused at the invitation's own deadline whatever the credential row says. A
// build that mints and states "the send, plus a lifetime" therefore still mails,
// still records a sent row, still leaves one live credential, and still states the
// deadline its own row keeps — only the promise is now ten minutes wider than the
// invitation allows, and the person meets a refusal inside the hour the body named.
// Reading both rows in one query, keyed by the token that link carries, is what
// tells those two builds apart.
func TestMailedInvitationLinkPromisesTheDeadlineTheInvitationKeeps(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	f.Advance(10 * time.Minute)
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return f.Links.OfferInvitation(ctx, tx, u.ID, u.Invitation.Generation, *u.Invitation.DeliveryID)
	}); err != nil {
		t.Fatalf("the handoff returned %v, want the accepted send acknowledged", err)
	}
	sent := f.Mail.Sent()
	if len(sent) != 1 || authtest.TokenIn(sent[0].Body) == "" {
		t.Fatalf("%d mailed links, want the one this handoff mailed", len(sent))
	}
	var kept, allowed time.Time
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT t.expires_at, p.invitation_expires_at FROM password_tokens t"+
			" JOIN users p ON p.id = t.user_id WHERE t.token_hash = ?",
			contracts.Hash(authtest.TokenIn(sent[0].Body))).Row().Scan(&kept, &allowed)
	}); err != nil {
		t.Fatalf("the credential the mailed link carries is not in the table: %v", err)
	}
	if !kept.Equal(allowed) {
		t.Errorf("the credential the mailed link carries keeps %s while the invitation it belongs to allows %s,"+
			" want one deadline for both", kept.UTC().Format(time.RFC3339), allowed.UTC().Format(time.RFC3339))
	}
}
