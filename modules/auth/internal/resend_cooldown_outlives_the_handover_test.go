package internal_test

// The mail slot and the resend cooldown are one guard read two ways, and the join
// between them is the moment the slot is released. A hand-off lets go of the slot
// only once the record of the send it made is durable: while the message was in the
// air nothing else minted over it, and the instant it is out the cooldown counts it.
// A release that ran ahead of that record would open a gap in which the next request
// for the same person saw neither guard — no link sent, nobody handing one over —
// and minted a second link of which the single-pending write keeps only the last.
//
// This case closes the gap from the outside, with no concurrency and no held
// transport: one delivered request, then the next ask for the same person. One link
// left, and the ask that followed it is refused by the record of that link.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// TestTheResendCooldownIsInForceTheMomentTheMailSlotIsReleased asks twice, one after
// the other, with the first request delivered and its link mailed in between.
func TestTheResendCooldownIsInForceTheMomentTheMailSlotIsReleased(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) { d.Mails = mailLedger() })
	person(t, conn, "ada@acme.localhost", contracts.RoleMember)

	if err := deliverOne(t, conn, resetSubscription(t), askedFor(t, conn, router, 0)); err != nil {
		t.Fatalf("deliver the first reset request: %v", err)
	}
	sent := mailbox.Sent()
	if len(sent) != 1 {
		t.Fatalf("links mailed=%d, want the one the delivered ask is owed", len(sent))
	}

	var marked int
	if err := admin.QueryRow("SELECT count(*) FROM password_tokens WHERE sent_at IS NOT NULL").Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if marked != 1 {
		t.Fatalf("links recorded as sent=%d, want the one row the hand-off marked", marked)
	}

	// The slot is released with that mark durable: the next ask for the same person
	// meets the cooldown and mints nothing, so no second link leaves.
	if err := deliverOne(t, conn, resetSubscription(t), askedFor(t, conn, router, 1)); err != nil {
		t.Fatalf("deliver the second reset request: %v", err)
	}
	if again := len(mailbox.Sent()); again != 1 {
		t.Errorf("links mailed=%d after a second ask, want 1: the cooldown on the link that already left must refuse this ask", again)
	}
	var rows int
	if err := admin.QueryRow("SELECT count(*) FROM password_tokens").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("password tokens=%d, want the one row the first ask left", rows)
	}
	if outcome, _, records := outcomeOf(t, conn, contracts.MailSetPassword); records != 1 || outcome != notification.MailSent {
		t.Errorf("set-password records=%d outcome=%q, want one sent row", records, outcome)
	}
}
