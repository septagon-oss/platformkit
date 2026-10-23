package notificationtest

import (
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

func queryMailKinds(t *testing.T, f MailFixture) {
	// One call, two mails: a door that speaks about one of them has to be
	// able to ask about it alone, and a read that answered it from whichever
	// row happened to be newest would answer about a mail the caller cannot
	// name (contracts.MailReport). The two rows share a request id on purpose:
	// it is the shape of one call that caused two sends somewhere else.
	request := mailRequest()
	f.record(t, sentRecord("auth.verification", request))
	f.record(t, contracts.MailRecord{
		Kind: "auth.set_password", Recipient: AdaMail, Outcome: contracts.MailFailed,
		Reason: "the relay refused", RequestID: request,
	})
	if got, known := f.answer(t, request); got != contracts.MailFailed || !known {
		t.Errorf("asked among every kind: %q known=%t, want the newest row, failed true", got, known)
	}
	if got, known := f.answer(t, request, "auth.verification"); got != contracts.MailSent || !known {
		t.Errorf("asked among auth.verification: %q known=%t, want sent true across the refused row", got, known)
	}
	if got, known := f.answer(t, request, "auth.set_password"); got != contracts.MailFailed || !known {
		t.Errorf("asked among auth.set_password: %q known=%t, want failed true", got, known)
	}
	// A kind the caller does not name is a row this read cannot see, and it
	// is answered exactly as a call that mailed nothing at all is.
	if got, known := f.answer(t, request, "auth.something_else"); got != "" || known {
		t.Errorf("asked among a kind with no row: %q known=%t, want the empty answer", got, known)
	}
}

func preserveMailOrder(t *testing.T, f MailFixture) {
	requests := []string{mailRequest(), mailRequest(), mailRequest()}
	outcomes := []string{contracts.MailSent, contracts.MailFailed, contracts.MailSuppressed}
	for i, outcome := range outcomes {
		f.record(t, contracts.MailRecord{
			Kind: "auth.verification", Recipient: AdaMail, Outcome: outcome,
			Reason: map[string]string{contracts.MailSent: "", contracts.MailFailed: "the relay refused",
				contracts.MailSuppressed: "no mailer wired"}[outcome],
			RequestID: requests[i],
		})
	}
	var got []string
	for _, row := range f.rows(t) {
		if i := slices.Index(requests, row.RequestID); i >= 0 {
			got = append(got, row.Outcome)
		}
	}
	if !slices.Equal(got, outcomes) {
		t.Errorf("this case's records came back %v in ledger order, want %v", got, outcomes)
	}
	// Three sends are three rows: nothing here updates or deletes, so the
	// second attempt at one address does not overwrite the first.
	before := len(f.rows(t))
	f.record(t, contracts.MailRecord{
		Kind: "auth.verification", Recipient: AdaMail, Outcome: contracts.MailFailed,
		Reason: "the relay refused again", RequestID: requests[0],
	})
	if after := len(f.rows(t)); after != before+1 {
		t.Errorf("a second send for one request moved the count %d -> %d; a record is appended, never updated",
			before, after)
	}
	// And the answer for a request with two attempts is the last of them.
	if got, known := f.answer(t, requests[0]); got != contracts.MailFailed || !known {
		t.Errorf("newest of two=%q known=%t, want failed true", got, known)
	}
}
