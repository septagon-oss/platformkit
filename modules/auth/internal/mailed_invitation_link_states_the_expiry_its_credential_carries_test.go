package internal_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
)

// The mailed invitation link tells the person the moment it stops working, and
// the credential it minted carries that moment in its own row. The two are said
// in one transaction from one value, so a build that quotes any other deadline
// — a fixed lifetime, the moment of the send — still mails, still records a
// sent row and still leaves one live credential: only this sentence drifts, and
// it drifts into sending a person to a link that refuses them before the hour
// its own body named.
func TestMailedInvitationLinkStatesTheExpiryItsCredentialCarries(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return f.Links.OfferInvitation(ctx, tx, u.ID, u.Invitation.Generation, *u.Invitation.DeliveryID)
	}); err != nil {
		t.Fatalf("the handoff returned %v, want the accepted send acknowledged", err)
	}
	sent := f.Mail.Sent()
	if len(sent) != 1 || authtest.TokenIn(sent[0].Body) == "" {
		t.Fatalf("%d mailed links, want the one this handoff mailed", len(sent))
	}
	var expiry time.Time
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT expires_at FROM password_tokens").Row().Scan(&expiry)
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent[0].Body, expiry.UTC().Format(time.RFC3339)) {
		t.Errorf("the mailed link states an expiry besides the %s its credential carries, want one moment"+
			" stated twice", expiry.UTC().Format(time.RFC3339))
	}
}
