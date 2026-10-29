package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// The tables a refused account-email request could have written, and the events it
// could have published. Named rather than counted generically: "wrote nothing" has
// to mean the tables and the names this flow touches, and a count over every table
// in the schema would answer for a flow nobody asked about.
var refusedTables = []string{"users", "password_tokens", "verification_tokens", "direct_mail_deliveries"}

var refusedEvents = []string{
	contracts.EventRegistrationRequested, contracts.EventVerificationRequested,
	contracts.EventResetRequested, contracts.EventNoLinkRequested,
}

// countsByTable reads those tables in one transaction, as the tenant under test.
func countsByTable(t *testing.T, conn *db.Conn) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, table := range refusedTables {
			var n int64
			if err := tx.DB().Raw("SELECT count(*) FROM " + table).Row().Scan(&n); err != nil {
				return err
			}
			out[table] = n
		}
		return nil
	})
	if err != nil {
		t.Fatalf("count the tables a refused request could touch: %v", err)
	}
	return out
}

// eventsPublished is how many account-email events exist, from whichever door or
// worker: an outbox row commits with the domain write it describes, so a refusal
// that left one behind would be counted here.
func eventsPublished(t *testing.T, conn *db.Conn) int64 {
	t.Helper()
	var n int64
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE name IN ?", refusedEvents).Row().Scan(&n)
	})
	if err != nil {
		t.Fatalf("count the account-email events: %v", err)
	}
	return n
}

// undeliverable says what a deployment with no mail host says: the composition
// still holds a sender, and that sender records what it is handed and answers that
// it never sends it. Nothing about the carrier changes; the answer is its own.
func undeliverable(deps *auth.Deps) {
	if box, ok := deps.Mailer.(*authtest.Mailbox); ok {
		box.Undeliverable = true
	}
	deps.MailDelivery = authtest.Undelivers
}

// TestAccountEmailDoorsRefuseWhereNoMailIsDelivered is the deployment that names no
// mail host, standing at every door that would have to promise it an email:
// sign-up, resend verification, forgotten password. Each answers with the refusal
// this module already had, and writes nothing — no account, no token, no delivery
// record, and no event for a worker to discharge.
//
// A sender exists in every case here, which is the point: the refusal comes from
// the carrier's answer about itself, not from the absence of one.
func TestAccountEmailDoorsRefuseWhereNoMailIsDelivered(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, undeliverable)
	before := countsByTable(t, conn)
	if n := eventsPublished(t, conn); n != 0 {
		t.Fatalf("a fresh installation holds %d account-email events", n)
	}
	for _, door := range []struct{ path, body string }{
		{"/api/v1/public/auth/register", approvalBody(t, "nobody@example.com", nil)},
		{"/api/v1/public/auth/resend-verification", `{"email":"nobody@example.com"}`},
		{"/api/v1/public/auth/password/forgot", `{"email":"nobody@example.com"}`},
	} {
		res := call(t, router, "POST", door.path, door.body, sameSite)
		if res.Code != http.StatusServiceUnavailable {
			t.Errorf("%s answered %d, want the unavailable refusal", door.path, res.Code)
		}
		if !strings.Contains(res.Body.String(), authtest.NoMailDetail) {
			t.Errorf("%s said %q, want the deployment's own unavailable sentence", door.path, res.Body.String())
		}
	}
	after := countsByTable(t, conn)
	for table, was := range before {
		if after[table] != was {
			t.Errorf("%s holds %d rows where it held %d before a refused request", table, after[table], was)
		}
	}
	if n := eventsPublished(t, conn); n != 0 {
		t.Errorf("a refused account-email request published %d events, want none", n)
	}
	if n := len(mailbox.Sent()); n != 0 {
		t.Errorf("a refused door handed %d messages to the carrier, want none", n)
	}
}

// TestAccountEmailDoorsAskForTheAnswerTheSenderGives is the negative half of the
// predicate, one field at a time. A sender that exists but says it files rather
// than dials is unavailable; a sender that says it dials and was never wired, or a
// composition with no host to build a link on, still is. Every reading answers the
// same door with the same sentence, so no arrangement of the three can leave a door
// that accepts a promise nobody can keep.
func TestAccountEmailDoorsAskForTheAnswerTheSenderGives(t *testing.T) {
	for _, leg := range []struct {
		name   string
		change func(*auth.Deps)
	}{
		{"the sender files the message and says so", func(d *auth.Deps) { d.MailDelivery = authtest.Undelivers }},
		{"the sender dials and nobody wired it", func(d *auth.Deps) { d.Mailer, d.MailDelivery = nil, authtest.Delivers }},
		{"the sender says nothing about itself", func(d *auth.Deps) { d.MailDelivery = nil }},
		{"there is no host to build the link on", func(d *auth.Deps) { d.Hosts, d.MailDelivery = nil, authtest.Delivers }},
	} {
		t.Run(leg.name, func(t *testing.T) {
			_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
			router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, leg.change)
			res := call(t, router, "POST", "/api/v1/public/auth/password/forgot", `{"email":"nobody@example.com"}`, sameSite)
			if res.Code != http.StatusServiceUnavailable {
				t.Fatalf("the forgot door answered %d where no mail is delivered, want 503", res.Code)
			}
			if !strings.Contains(res.Body.String(), authtest.NoMailDetail) {
				t.Errorf("the forgot door said %q", res.Body.String())
			}
			if n := eventsPublished(t, conn); n != 0 {
				t.Errorf("the refused forgot door published %d events, want none", n)
			}
		})
	}
}

// TestTheMailDeliveryDoorHasNothingToSayAboutARefusedRequest keeps the refusal from
// becoming a second oracle: the door that answers two words about a call's own mail
// answers a request id from a refused call exactly as it answers an id nobody mailed
// for, because a refusal leaves no row for it to speak from.
func TestTheMailDeliveryDoorHasNothingToSayAboutARefusedRequest(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.Mails = mailLedger()
		undeliverable(d)
	})
	ask := func(requestID string) (int, string) {
		res := call(t, router, "POST", "/api/v1/public/auth/mail-delivery", `{"requestId":"`+requestID+`"}`, sameSite)
		var out struct {
			State string `json:"state"`
		}
		// A refusal is problem+json and holds no state field; every answer that is
		// not a refusal is compared against the state it owes below, so a body that
		// decodes to nothing still cannot pass.
		_ = json.NewDecoder(res.Result().Body).Decode(&out)
		return res.Code, out.State
	}
	const refusedCall = "22222222-2222-4222-8222-222222222222"
	if res := call(t, router, "POST", "/api/v1/public/auth/password/forgot",
		`{"email":"nobody@example.com"}`, sameSite, func(r *http.Request) { r.Header.Set("X-Request-Id", refusedCall) }); res.Code != http.StatusServiceUnavailable {
		t.Fatalf("the forgot door answered %d where no mail is delivered, want 503", res.Code)
	}
	code, state := ask(refusedCall)
	unknownCode, unknownState := ask("33333333-3333-4333-8333-333333333333")
	if code != unknownCode || state != unknownState {
		t.Errorf("a refused request was answered %d %q and an id nobody mailed for %d %q, want one answer", code, state, unknownCode, unknownState)
	}
	if state != notification.MailStatePending {
		t.Errorf("the neutral answer is %q, want %q", state, notification.MailStatePending)
	}
}

// TestAnInvitationLinkIsRecordedAsSuppressedWhereNothingIsDelivered is the ledger
// rule inside a transaction. An operator invitation is the operator's own act and
// stays admitted where no mail is delivered, so its link still goes to the carrier —
// but the row describing the send says suppressed, not sent. A row that said a mail
// left when nothing was attempted is the same lie from the other side of the
// counter. With a sender that dials, nothing about the record changes.
func TestAnInvitationLinkIsRecordedAsSuppressedWhereNothingIsDelivered(t *testing.T) {
	for _, leg := range []struct {
		name     string
		sends    bool
		want     string
		wantText string
	}{
		{"nothing is delivered", false, notification.MailSuppressed, "no mail transport is wired"},
		{"the relay is running", true, notification.MailSent, ""},
	} {
		t.Run(leg.name, func(t *testing.T) {
			_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
			seed(t, conn, acme)
			box := &authtest.Mailbox{Undeliverable: !leg.sends}
			svc := internal.NewService(realUsers(), &authtest.Notices{}, internal.Delivery{
				Mailer: box, MailDelivery: box, Hosts: authtest.Host(host), Secure: true,
				Mails: mailLedger(),
			})
			id := invitePerson(t, conn, "invited@acme.localhost")
			err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return svc.Offer(ctx, tx, id)
			})
			if err != nil {
				t.Fatalf("Offer: %v", err)
			}
			// The installation that wired a recording carrier on purpose keeps its
			// record and its link: the flow stays testable and, in development, the
			// person still reads a working link out of the mailbox.
			if n := len(box.Sent()); n != 1 {
				t.Fatalf("the carrier holds %d messages, want the invitation link it was handed", n)
			}
			outcome, reason, rows := outcomeOf(t, conn, contracts.MailSetPassword)
			if rows != 1 || outcome != leg.want {
				t.Fatalf("set-password records=%d outcome=%q, want one %q row", rows, outcome, leg.want)
			}
			if !strings.Contains(reason, leg.wantText) {
				t.Errorf("the record's reason is %q, want it to say %q", reason, leg.wantText)
			}
		})
	}
}

// TestABrokenRelayIsAFailedSendAndNotAnUnconfiguredDoor keeps an outage from
// reading as an absence. The sender says it dials and the dial fails: the door still
// accepts, the worker still records a failed send, and nobody is told that this
// installation has no mail when what it has is a relay that refused them.
func TestABrokenRelayIsAFailedSendAndNotAnUnconfiguredDoor(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	mailer := &verificationFailingMailer{fail: true}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.Mailer, d.Mails = mailer, mailLedger()
	})
	res := call(t, router, "POST", "/api/v1/public/auth/register", approvalBody(t, "outage@example.com", nil), sameSite)
	if res.Code != http.StatusAccepted {
		t.Fatalf("a relay that refused answered %d, want the acceptance the door always gives", res.Code)
	}
	worker(t, conn)
	outcome, _, rows := outcomeOf(t, conn, contracts.MailVerification)
	if rows != 1 || outcome != notification.MailFailed {
		t.Fatalf("verification records=%d outcome=%q, want one failed row", rows, outcome)
	}
}

// invitePerson makes the person an operator invited and who has not yet signed in,
// which is the only state an offer of a set-password link is made in.
func invitePerson(t *testing.T, conn *db.Conn, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := realUsers().Invite(ctx, tx, email, "")
		if err != nil {
			return err
		}
		id = u.ID
		return nil
	})
	if err != nil {
		t.Fatalf("invite %s: %v", email, err)
	}
	return id
}

// sameSite is the origin a public account door insists on: these routes are read by
// the page that asked, and a cross-site post is refused before anything else is.
func sameSite(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") }
