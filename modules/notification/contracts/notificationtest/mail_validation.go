package notificationtest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

func recordSentMail(t *testing.T, f MailFixture) {
	request := mailRequest()
	// A mail the transport took has nothing to say about why: the reason
	// column is empty and the row is still the record of a send.
	f.record(t, contracts.MailRecord{
		Kind: "auth.set_password", Recipient: AdaMail,
		Outcome: contracts.MailSent, Reason: "", RequestID: request,
	})
	if got, known := f.answer(t, request); got != contracts.MailSent || !known {
		t.Errorf("recorded send answered %q known=%t, want sent true", got, known)
	}
}

func requireFailureReason(t *testing.T, f MailFixture) {
	f.refuse(t, contracts.MailRecord{
		Kind: "auth.set_password", Recipient: AdaMail,
		Outcome: contracts.MailFailed, RequestID: mailRequest(),
	}, contracts.ErrMailReason)
	// A request that mailed nothing is not refused and not answered: it is
	// the ordinary case, and the caller has to be able to tell it from a
	// record that says failed. The empty id is the same shape of mistake as
	// a record with no reason, so it is refused rather than answered with
	// the newest untraced row in the tenant.
	if got, known := f.answer(t, mailRequest()); got != "" || known {
		t.Errorf("a request that mailed nothing answered %q known=%t, want the empty answer", got, known)
	}
	err := f.Step(f.Ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, known, err := f.Ledger.MailOutcome(ctx, tx, "")
		if known {
			t.Error("the empty request id was answered, which makes it somebody else's call")
		}
		return err
	})
	if !errors.Is(err, contracts.ErrMailRequest) {
		t.Errorf("MailOutcome(\"\") = %v, want the caller's mistake refused", err)
	}
}

func validateMailKind(t *testing.T, f MailFixture) {
	for _, kind := range []string{"", "Set Password", "auth", "auth.Set", "auth.set password"} {
		f.refuse(t, contracts.MailRecord{
			Kind: kind, Recipient: AdaMail, Outcome: contracts.MailSent, RequestID: mailRequest(),
		}, contracts.ErrMailKind)
	}
	// The checker is the table's own grammar, so the two names in use today
	// have to arrive, and the caller's own kind has to be one of them.
	for _, kind := range []string{"auth.set_password", "auth.verification"} {
		request := mailRequest()
		f.record(t, sentRecord(kind, request))
		if got, known := f.answer(t, request); got != contracts.MailSent || !known {
			t.Errorf("kind %q: answered %q known=%t, want sent true", kind, got, known)
		}
	}
}

func validateMailOutcome(t *testing.T, f MailFixture) {
	for _, outcome := range []string{"requested", "pending", ""} {
		// `requested` is the one that matters: it is legal in 000027 and
		// illegal here, because a mail with no notice has no first half to
		// be requested about. A caller that reached for the delivery
		// ledger's vocabulary is refused at the port, not by a CHECK.
		f.refuse(t, contracts.MailRecord{
			Kind: "auth.set_password", Recipient: AdaMail,
			Outcome: outcome, Reason: "the relay refused", RequestID: mailRequest(),
		}, contracts.ErrMailOutcome)
	}
}

func validateMailRecipient(t *testing.T, f MailFixture) {
	f.refuse(t, contracts.MailRecord{
		Kind: "auth.set_password", Outcome: contracts.MailSent, RequestID: mailRequest(),
	}, contracts.ErrMailRecipient)
	f.refuse(t, contracts.MailRecord{
		Kind: "auth.set_password", Recipient: strings.Repeat("a", 321),
		Outcome: contracts.MailSent, RequestID: mailRequest(),
	}, contracts.ErrMailRecipient)
	// An address is spelled by contracts.EmailKey at the call site and is
	// not re-normalised here — that would be the same fact normalised twice,
	// in two ways that could disagree. Only the whitespace around it goes.
	request := mailRequest()
	f.record(t, contracts.MailRecord{
		Kind: "auth.set_password", Recipient: "  Ada@Example.COM  ",
		Outcome: contracts.MailSent, RequestID: request,
	})
	for _, row := range f.rows(t) {
		if row.RequestID == request && row.Recipient != "Ada@Example.COM" {
			t.Errorf("the ledger re-spelled the address it was handed as %q", row.Recipient)
		}
	}
}
