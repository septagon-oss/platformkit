package notificationtest

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

func redactMailSecret(t *testing.T, f MailFixture) {
	const token = "Zm9vYmFyYmF6AQT3yZ0pQ0d0dGhlIHF1aWNrIGJyb3duIGZveA"
	request := mailRequest()
	// The mailer that refuses by quoting its own input back is the reason a
	// reason has to be scrubbed on its way in (modules/auth/internal/
	// verification.go says the same thing about its error).
	said := "transport echoed its input: https://acme.example.com/auth/reset?token=" + token
	f.record(t, contracts.MailRecord{
		Kind: "auth.set_password", Recipient: AdaMail, Outcome: contracts.MailFailed,
		Reason: contracts.RedactMailReason(said, token, "sha256-of-"+token), RequestID: request,
	})
	// Over the bound is clipped, not refused: a long sentence that has lost
	// its secret is still worth recording.
	long := mailRequest()
	f.record(t, contracts.MailRecord{
		Kind: "auth.set_password", Recipient: AdaMail, Outcome: contracts.MailFailed,
		Reason: strings.Repeat("refused ", contracts.MaxMailReason), RequestID: long,
	})
	var reasons []string
	for _, row := range f.rows(t) {
		if row.RequestID == request || row.RequestID == long {
			reasons = append(reasons, row.Reason)
		}
	}
	if len(reasons) != 2 {
		t.Fatalf("the two refused mails left %d records, want 2", len(reasons))
	}
	for _, reason := range reasons {
		if strings.Contains(reason, token) {
			t.Errorf("a recorded reason carries the credential: %q", reason)
		}
		if len(reason) > contracts.MaxMailReason {
			t.Errorf("a recorded reason is %d bytes, over the bound of %d", len(reason), contracts.MaxMailReason)
		}
		if !utf8.ValidString(reason) {
			t.Errorf("a recorded reason ends mid-rune: %q", reason)
		}
	}
	if got, known := f.answer(t, request); got != contracts.MailFailed || !known {
		t.Errorf("a refused mail answered %q known=%t, want failed true", got, known)
	}
}
