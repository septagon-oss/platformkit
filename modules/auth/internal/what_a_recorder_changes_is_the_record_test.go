package internal_test

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// TestARecorderChangesWhatIsRecordedNotWhatIsAnsweredOrSent asks the
// resend-verification door of one installation that can mail twice over, the two
// times differing in one wiring: whether a mail ledger is there to keep the record.
// The answer must be the same acceptance, and the message must leave either way —
// the record is the only thing a recorder is entitled to change.
//
// Both halves break on the same mistake. Read the recorder as availability and the
// installation that wired none refuses a person a carrier would really mail; read
// the recorder as part of the send and the mail stops leaving for the installation
// that has no record. Either way the ledger, which answers what became of a mail,
// has been asked whether one may be promised or sent at all.
func TestARecorderChangesWhatIsRecordedNotWhatIsAnsweredOrSent(t *testing.T) {
	const address = "someone@acme.localhost"
	type answer struct{ status, sent, records int }
	answers := map[string]answer{}
	for _, leg := range []struct {
		name  string
		wires func(*auth.Deps)
	}{
		{"a recorder is wired", func(d *auth.Deps) { d.Mails = mailLedger() }},
		{"no recorder is wired", func(d *auth.Deps) { d.Mails = nil }},
	} {
		_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
		router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, leg.wires)
		person(t, conn, address)
		res := call(t, router, http.MethodPost, "/api/v1/public/auth/resend-verification",
			`{"email":"`+address+`"}`, sameSite)
		worker(t, conn)
		answers[leg.name] = answer{res.Code, len(mailbox.Sent()),
			int(countsByTable(t, conn)["direct_mail_deliveries"])}
	}
	recorder, bare := answers["a recorder is wired"], answers["no recorder is wired"]
	if recorder.status != http.StatusAccepted || bare.status != recorder.status {
		t.Errorf("resend-verification answered %d with a recorder wired and %d without one, want the acceptance both ways",
			recorder.status, bare.status)
	}
	if bare.sent != recorder.sent || recorder.sent == 0 {
		t.Errorf("the carrier was handed %d messages with a recorder wired and %d without, want the same non-zero number both ways",
			recorder.sent, bare.sent)
	}
	if bare.records != 0 {
		t.Errorf("a composition that wired no recorder left %d delivery rows, which Delivery.Mails promises none would",
			bare.records)
	}
	if recorder.records <= bare.records {
		t.Errorf("a send with a recorder wired left %d rows and the same send without one left %d: the record of a mail is what the recorder is for",
			recorder.records, bare.records)
	}
}
