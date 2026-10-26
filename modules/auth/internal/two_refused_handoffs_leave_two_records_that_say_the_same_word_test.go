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

// A person who retries an invitation the transport refused is owed one record per
// refusal, each saying the module's one word for why, and no credential any of
// those refusals minted. Reading "what became of the set-password mail" as the
// newest row of its kind is only a sound read while every row a tenant holds for
// that kind says the same thing, so this asks what two refusals leave behind.
func TestTwoRefusedHandoffsLeaveTwoRecordsThatSayTheSameWord(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	f.Mail.Failure = errors.New("smtp 451 4.3.0 give it another try")
	for n := 1; n <= 2; n++ {
		if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return f.Links.OfferInvitation(ctx, tx, u.ID, u.Invitation.Generation, *u.Invitation.DeliveryID)
		}); err != nil {
			t.Fatalf("refused handoff %d returned %v, want the refusal recorded rather than the command failed", n, err)
		}
	}
	var minted []string
	for _, m := range f.Mail.Sent() {
		if tok := authtest.TokenIn(m.Body); tok != "" {
			minted = append(minted, tok)
		}
	}
	if len(minted) != 2 {
		t.Fatalf("%d mailed links, want the two each refused handoff minted, or the credential scan below proves nothing", len(minted))
	}
	var rows, events, agreeing, live int64
	var outcomes, words, payloads string
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Raw("SELECT count(*), coalesce(string_agg(DISTINCT outcome, ''), ''),"+
			" coalesce(string_agg(DISTINCT reason, ''), '') FROM direct_mail_deliveries WHERE kind = $1",
			contracts.MailSetPassword).Row().Scan(&rows, &outcomes, &words); err != nil {
			return err
		}
		if err := tx.DB().Raw("SELECT count(*), count(*) FILTER (WHERE payload->>'reason' = $2 AND payload->>'kind' = $3),"+
			" coalesce(string_agg(payload::text, '|'), '') FROM platformkit_outbox WHERE name = $1",
			contracts.EventMailFailed, "transport_error", contracts.MailSetPassword).
			Row().Scan(&events, &agreeing, &payloads); err != nil {
			return err
		}
		return tx.DB().Raw("SELECT count(*) FROM password_tokens").Row().Scan(&live)
	}); err != nil {
		t.Fatal(err)
	}
	if rows != 2 || outcomes != "failed" {
		t.Errorf("%d records of outcome %q, want two failed rows, one per refusal", rows, outcomes)
	}
	if words != "transport_error" {
		t.Errorf("the records say %q, want the one word the module states a reported failure records, in both", words)
	}
	if events != 2 || agreeing != 2 {
		t.Errorf("%d failure events, %d of them saying the record's own kind and word, want two of each: %s", events, agreeing, payloads)
	}
	for _, tok := range minted {
		if strings.Contains(payloads, tok) || strings.Contains(payloads, string(contracts.Hash(tok))) {
			t.Errorf("a failure event carries a credential a refused send minted: %s", payloads)
		}
	}
	if live != 0 {
		t.Errorf("%d credential rows beside two refused handoffs, want none: each link was retired with its send", live)
	}
}
