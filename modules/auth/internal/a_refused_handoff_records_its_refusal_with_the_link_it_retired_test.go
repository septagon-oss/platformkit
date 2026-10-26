package internal_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// What a handoff the transport refused leaves behind is one transaction's, in both
// directions: the handoff that commits keeps one row saying the send failed, the
// failure event beside it and no credential the refusal minted, and the handoff the
// worker took back keeps none of the three. A reading that says delivery is
// uncertain has to be able to ask which of these happened, and the transport's own
// words reach the record only as the module's reason for refusing.
func TestARefusedHandoffRecordsItsRefusalWithTheLinkItRetired(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	generation, delivery := u.Invitation.Generation, *u.Invitation.DeliveryID
	providerWords := "smtp 550 5.7.1 relay denied for this mailbox"
	f.Mail.Failure = errors.New(providerWords)

	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := f.Links.OfferInvitation(ctx, tx, u.ID, generation, delivery); err != nil {
			return err
		}
		return errRollback
	}); !errors.Is(err, errRollback) {
		t.Fatalf("the refused handoff that was rolled back returned %v", err)
	}
	if got := handoffEffects(t, f); got.records+got.events+got.tokens != 0 {
		t.Fatalf("the refused handoff that was rolled back left %+v, want nothing", got)
	}

	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return f.Links.OfferInvitation(ctx, tx, u.ID, generation, delivery)
	}); err != nil {
		t.Fatalf("the refused handoff returned %v, want the refusal recorded rather than the command failed", err)
	}
	got := handoffEffects(t, f)
	if got.records != 1 || got.outcome != notification.MailFailed || got.reason == "" {
		t.Fatalf("the committed refusal left %+v, want one failed row that says why", got)
	}
	if strings.Contains(got.reason, providerWords) || len(got.reason) > notification.MaxMailReason {
		t.Errorf("the refusal says %q, which is the transport's own words or longer than the record may hold", got.reason)
	}
	if got.tokens != 0 {
		t.Errorf("%d credential rows beside a refused handoff, want none: the link it minted was retired with the send", got.tokens)
	}
	if got.events != 1 {
		t.Errorf("%d mail-failure events committed beside the refusal, want the one the record commits with", got.events)
	}
}

// handoffEffects is what one tenant can see of its handoff attempts, asked as one
// read so a case can say "nothing was left" and "what the last one left" together.
type handoffEffect struct {
	records, events, tokens int64
	outcome, reason         string
}

func handoffEffects(t *testing.T, f invitationSQLFixture) handoffEffect {
	t.Helper()
	var out handoffEffect
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Raw("SELECT count(*), coalesce(max(outcome), ''), coalesce(max(reason), '')"+
			" FROM direct_mail_deliveries WHERE kind = $1", contracts.MailSetPassword).
			Row().Scan(&out.records, &out.outcome, &out.reason); err != nil {
			return err
		}
		if err := tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE name = $1",
			contracts.EventMailFailed).Row().Scan(&out.events); err != nil {
			return err
		}
		return tx.DB().Raw("SELECT count(*) FROM password_tokens").Row().Scan(&out.tokens)
	}); err != nil {
		t.Fatal(err)
	}
	return out
}
