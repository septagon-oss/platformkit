package internal_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
)

// The deadline a mailed invitation link states belongs to the credential it
// carries, not to the moment the send happened. The outbox event that asks for a
// handoff can reach a worker minutes after the invitation was requested, while the
// credential's row keeps the deadline that request set; a build that wrote the
// sentence as "the send, plus a lifetime" would mail a link promising more time
// than the credential keeps, and the person would meet a refusal inside the hour
// the body named. Reading the row through the token the link itself carries is
// what makes this that credential and not whichever row the table hands back
// first.
func TestLateInvitationHandoffStatesTheDeadlineItsCredentialCarries(t *testing.T) {
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
	var deadline time.Time
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT expires_at FROM password_tokens WHERE token_hash = ?",
			contracts.Hash(authtest.TokenIn(sent[0].Body))).Row().Scan(&deadline)
	}); err != nil {
		t.Fatalf("the credential the mailed link carries is not in the table: %v", err)
	}
	if !strings.Contains(sent[0].Body, deadline.UTC().Format(time.RFC3339)) {
		t.Errorf("the mailed link states an expiry besides the %s its credential carries, want the request's deadline stated twice",
			deadline.UTC().Format(time.RFC3339))
	}
}
