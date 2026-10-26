package internal_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
)

// A handoff the transport took but the transaction took back leaves no link
// behind, and the generation ends holding exactly the credential the last
// committed handoff actually mailed. A person who follows a link that came back
// from a rolled-back attempt must find nothing, and a person who follows the one
// that committed must find it live.
func TestAHandoffThatRolledBackLeavesNoLinkAndTheRedeliveryLeavesOne(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	generation, delivery := u.Invitation.Generation, *u.Invitation.DeliveryID

	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := f.Links.OfferInvitation(ctx, tx, u.ID, generation, delivery); err != nil {
			return err
		}
		return errRollback
	}); !errors.Is(err, errRollback) {
		t.Fatalf("the handoff that was rolled back returned %v", err)
	}
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return f.Links.OfferInvitation(ctx, tx, u.ID, generation, delivery)
	}); err != nil {
		t.Fatalf("the next handoff of the same generation: %v", err)
	}

	sent := f.Mail.Sent()
	if len(sent) == 0 {
		t.Fatal("the generation was never handed to the transport")
	}
	live := func(token string) int {
		t.Helper()
		var n int
		if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Raw("SELECT count(*) FROM password_tokens WHERE invitation_generation = $1"+
				" AND token_hash = $2 AND expires_at > now()", generation, contracts.Hash(token)).Row().Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, m := range sent[:len(sent)-1] {
		if got := live(linkOf(m.Body)); got != 0 {
			t.Errorf("a handoff that did not commit left %d live credential(s) behind", got)
		}
	}
	if got := live(linkOf(sent[len(sent)-1].Body)); got != 1 {
		t.Errorf("the link of the last handoff holds %d live credential(s), want the one it was mailed as", got)
	}
	var liveOfGeneration int
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM password_tokens WHERE invitation_generation = $1"+
			" AND expires_at > now()", generation).Row().Scan(&liveOfGeneration)
	}); err != nil {
		t.Fatal(err)
	}
	if liveOfGeneration != 1 {
		t.Errorf("one generation carries %d live credentials, want one", liveOfGeneration)
	}
}

// linkOf is the token a mailed set-password link carries.
func linkOf(body string) string {
	_, after, _ := strings.Cut(body, "token=")
	return strings.TrimSpace(strings.SplitN(after, "\n", 2)[0])
}
