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

// A refused handoff leaves two answers of one transaction: the row in the mail
// ledger and the failure event beside it. The event's contract promises they agree
// — the same redacted sentence, the same address — and that its payload holds no
// token, hash, subject, body, link or URL, because modules/audit copies every
// payload into the trail forever. A count of the event says it exists; this says
// what it says, and that the credential the refusal retired stayed out of it.
func TestARefusedHandoffsFailureEventSaysWhatItsRecordSays(t *testing.T) {
	f := invitationSQL(t)
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	f.Mail.Failure = errors.New("smtp 550 5.7.1 relay denied for this mailbox")

	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return f.Links.OfferInvitation(ctx, tx, u.ID, u.Invitation.Generation, *u.Invitation.DeliveryID)
	}); err != nil {
		t.Fatalf("the refused handoff returned %v, want the refusal recorded rather than the command failed", err)
	}
	sent := f.Mail.Sent()
	if len(sent) == 0 {
		t.Fatal("the handoff never reached the transport, so the credential scan below would prove nothing")
	}
	token := authtest.TokenIn(sent[len(sent)-1].Body)
	if token == "" {
		t.Fatal("the mailed link carries no credential, so the credential scan below would prove nothing")
	}

	var payload, said, kind, address, record, recorded string
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT o.payload::text, coalesce(o.payload->>'reason', ''),"+
			" coalesce(o.payload->>'kind', ''), coalesce(o.payload->>'recipient', ''),"+
			" (SELECT m.reason FROM direct_mail_deliveries m WHERE m.kind = $2 LIMIT 1),"+
			" (SELECT m.recipient FROM direct_mail_deliveries m WHERE m.kind = $2 LIMIT 1)"+
			" FROM platformkit_outbox o WHERE o.name = $1", contracts.EventMailFailed,
			contracts.MailSetPassword).Row().Scan(&payload, &said, &kind, &address, &record, &recorded)
	}); err != nil {
		t.Fatal(err)
	}

	if record != "transport_error" {
		t.Errorf("the record says %q, want the one word the module states a reported failure records", record)
	}
	if kind != contracts.MailSetPassword {
		t.Errorf("the failure event names kind %q, want the set-password mail the handoff mails", kind)
	}
	if said == "" || said != record {
		t.Errorf("the trail says %q where the record says %q, want one redacted sentence in both", said, record)
	}
	if address != recorded {
		t.Errorf("the trail names %q where the record names %q, want the same address in both", address, recorded)
	}
	if strings.Contains(payload, token) || strings.Contains(payload, string(contracts.Hash(token))) {
		t.Errorf("the failure event carries the credential the refused send minted: %s", payload)
	}
}
