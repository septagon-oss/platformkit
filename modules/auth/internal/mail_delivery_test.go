package internal_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// mailLedger composes the notification module's service the way the reference
// application does, and hands back the port auth records through. It is the same
// value Notify is in apps/platformkit: MailLedger is part of contracts.Service,
// so a test that wants a record has to wire the service rather than a second one.
func mailLedger() notification.MailLedger {
	svc, _ := notificationmodule.New(notificationmodule.Deps{Mailer: notificationmodule.NewMailbox()})
	return svc
}

// refuseNothingYet scans every table this test's own schema holds for a secret,
// enumerating them from the database rather than naming them in the case. A list
// written into the test cannot notice a leak into a table nobody thought of, which
// is exactly how a new table would be missed; the check below that the new one is
// in the list is what keeps the enumeration honest.
func refuseNothingYet(t *testing.T, admin *sql.DB, secrets ...string) {
	t.Helper()
	rows, err := admin.Query("SELECT table_name FROM information_schema.tables" +
		" WHERE table_schema = current_schema() AND table_type = 'BASE TABLE' ORDER BY table_name")
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	if !slices.Contains(tables, "direct_mail_deliveries") {
		t.Fatalf("the scan names no delivery table, so it scans nothing: %v", tables)
	}
	for _, table := range tables {
		for _, secret := range secrets {
			if secret == "" {
				t.Fatal("a scan for the empty string would report every table")
			}
			var count int
			err := admin.QueryRowContext(t.Context(),
				fmt.Sprintf("SELECT count(*) FROM %s r WHERE strpos(r::text,$1)>0", table), secret).Scan(&count)
			if err != nil {
				t.Fatalf("scan %s: %v", table, err)
			}
			if count != 0 {
				t.Errorf("%s persisted a plaintext credential", table)
			}
		}
	}
}

// trail runs modules/audit's own handler over everything in the outbox, which is
// what the composed application does through SubscribeAll: an event is on the
// trail because it was emitted, with nothing registered anywhere to make that
// true. The auth tests mount no audit routes, so this is the one line of wiring a
// case about the trail needs.
func trail(t *testing.T, conn *db.Conn) {
	t.Helper()
	type pending struct {
		Name    string
		Payload []byte
	}
	subs := audit.New(audit.Deps{}).Subscriptions
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var rows []pending
		if err := tx.DB().Table("platformkit_outbox").Order("created_at, id").Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			for _, s := range subs {
				// SubscribeAll is one subscription with no name: the kernel
				// expands it into one per declared event after every manifest is
				// read, and an unnamed one is the whole tenant.
				if s.Name != "" && s.Name != r.Name {
					continue
				}
				err := s.Handler(ctx, tx, events.Event{
					ID: uuid.New(), Name: r.Name, TenantID: acme.ID, Payload: r.Payload,
				})
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("write the trail: %v", err)
	}
}

// linksMailed is the mail that carries a credential — a verification or
// set-password link, read by authtest.TokenIn rather than by subject, because the
// property every case below counts is that no credential left for an address that
// should not receive one.
//
// It is not the same question as "how many messages left". Since every request for
// a link hands the transport one message whichever branch it takes
// (internal/no_link.go), a request that sends no link still sends the sentence
// saying so, and a case that counted messages as though each were a credential
// would fail on a message that carries nothing.
func linksMailed() []notification.Message {
	var out []notification.Message
	for _, letter := range mailbox.Sent() {
		if authtest.TokenIn(letter.Body) != "" {
			out = append(out, letter)
		}
	}
	return out
}

// outcomeOf is what the module recorded for one kind of mail, as one row.
func outcomeOf(t *testing.T, conn *db.Conn, kind string) (outcome, reason string, rows int) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT outcome, reason FROM direct_mail_deliveries WHERE kind = ?"+
			" ORDER BY seq DESC LIMIT 1", kind).Row().Scan(&outcome, &reason)
	})
	if err != nil {
		return "", "", 0
	}
	var n int
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM direct_mail_deliveries WHERE kind = ?", kind).Row().Scan(&n)
	})
	if err != nil {
		t.Fatalf("count the records of %s: %v", kind, err)
	}
	return outcome, reason, n
}

// TestMailedLinkIsRecordedAndCarriesNoCredential is the brief's fourth test for a
// sign-up: the person was mailed, the record says sent, and the token that mail
// carried is in no row of any table — including the new one, which is the table a
// delivery record would leak into if a delivery record leaked.
func TestMailedLinkIsRecordedAndCarriesNoCredential(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	mailer := &verificationFailingMailer{}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.Mailer, d.Mails = mailer, mailLedger()
	})
	if res := call(t, router, "POST", "/api/v1/public/auth/register", approvalBody(t, "recorded@example.com", nil)); res.Code != http.StatusAccepted {
		t.Fatalf("signup=%d", res.Code)
	}
	worker(t, conn)
	if mailer.token == "" {
		t.Fatal("the mail carried no token, so the scan below proves nothing")
	}
	outcome, reason, rows := outcomeOf(t, conn, contracts.MailVerification)
	if rows != 1 || outcome != notification.MailSent || reason != "" {
		t.Fatalf("verification records=%d outcome=%q reason=%q, want one sent row with nothing to say", rows, outcome, reason)
	}
	var tokens int
	if err := admin.QueryRow("SELECT count(*) FROM verification_tokens").Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if tokens != 1 {
		t.Fatalf("verification tokens=%d, want the one credential that was mailed", tokens)
	}
	// The record says who it went to and which kind it was, and it holds no
	// address-shaped or link-shaped anything else.
	var recipient, traceparent *string
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT recipient, traceparent FROM direct_mail_deliveries").Row().Scan(&recipient, &traceparent)
	})
	if err != nil {
		t.Fatal(err)
	}
	if recipient == nil || *recipient != "recorded@example.com" {
		t.Errorf("record recipient=%v, want the address that asked", recipient)
	}
	// The subscription in worker() carries no request, and the row says so as
	// NULL rather than as an empty string: "no call asked" must be askable.
	if traceparent != nil {
		t.Errorf("record traceparent=%q, want NULL for a mail with no call behind it", *traceparent)
	}
	refuseNothingYet(t, admin, mailer.token, authtest.Password)
}

// TestRefusedMailIsRecordedWithoutItsCredential replaces the case that pinned the
// outbox's retry ladder for a direct send. The ladder is gone, and what took its
// place is here: one row that says the mail did not go and why, the trail saying
// the same, no credential left behind, and the person able to ask again at once
// rather than waiting on a cap for a mail that never left.
//
// Its two promises survive from the case it replaces — the accounts and tokens
// counts, and the whole-schema scan — and the scan is now made against a row that
// says the send failed rather than against the absence of one.
func TestRefusedMailIsRecordedWithoutItsCredential(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	mailer := &verificationFailingMailer{fail: true}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.Mailer, d.Mails = mailer, mailLedger()
	})
	if res := call(t, router, "POST", "/api/v1/public/auth/register", approvalBody(t, "refused@example.com", nil)); res.Code != http.StatusAccepted {
		t.Fatalf("signup=%d", res.Code)
	}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var payload []byte
		if err := tx.DB().Raw("SELECT payload FROM platformkit_outbox WHERE name=?", user.EventRegistrationUnverified).Row().Scan(&payload); err != nil {
			return err
		}
		for _, sub := range subs {
			if sub.Name == user.EventRegistrationUnverified {
				return sub.Handler(ctx, tx, events.Event{Name: sub.Name, TenantID: acme.ID, Payload: payload})
			}
		}
		return nil
	})
	// The send is acknowledged, which is what lets the record of it commit. The
	// transport's text went into the record redacted, not into the handler's error.
	if err != nil {
		t.Fatalf("a refused send must acknowledge so its record commits: %v", err)
	}
	if mailer.token == "" {
		t.Fatal("the mailer never saw a token, so the scan below proves nothing")
	}
	outcome, reason, rows := outcomeOf(t, conn, contracts.MailVerification)
	if rows != 1 || outcome != notification.MailFailed || reason == "" {
		t.Fatalf("verification records=%d outcome=%q reason=%q, want one failed row that says why", rows, outcome, reason)
	}
	if strings.Contains(reason, mailer.token) || len(reason) > notification.MaxMailReason {
		t.Errorf("the failed row's reason carries the secret or is unbounded: %q", reason)
	}
	var accounts, tokens int
	if err := admin.QueryRow("SELECT count(*) FROM users WHERE status='unverified'").Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow("SELECT count(*) FROM verification_tokens").Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if accounts != 1 || tokens != 0 {
		t.Fatalf("failed delivery accounts=%d tokens=%d, want the person kept and no credential issued", accounts, tokens)
	}

	// The failure and the event commit together, and the trail is what an operator
	// reads: auth.mail_failed reaches audit_events through modules/audit's
	// SubscribeAll, which needs nothing registered to be true.
	var published int
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE name = ?", contracts.EventMailFailed).Row().Scan(&published)
	})
	if err != nil {
		t.Fatal(err)
	}
	if published != 1 {
		t.Fatalf("auth.mail_failed outbox rows=%d, want the one failure", published)
	}
	trail(t, conn)
	var trailed int
	if err := admin.QueryRow("SELECT count(*) FROM audit_events WHERE name = $1", contracts.EventMailFailed).Scan(&trailed); err != nil {
		t.Fatal(err)
	}
	if trailed != 1 {
		t.Fatalf("auth.mail_failed audit rows=%d, want the failure on the trail", trailed)
	}

	// And the person can ask again at once: the cap counts links that left, and
	// this one did not, so the next attempt mails rather than waiting.
	mailer.fail = false
	worker(t, conn)
	if got := len(mailer.box.Sent()); got != 1 {
		t.Fatalf("mails after the transport came back=%d, want the next attempt to go out", got)
	}
	if got, _, rows := outcomeOf(t, conn, contracts.MailVerification); rows != 2 || got != notification.MailSent {
		t.Fatalf("newest verification record=%q in %d rows, want the failed attempt followed by a sent one", got, rows)
	}
	refuseNothingYet(t, admin, mailer.token, authtest.Password)
}

// TestTheMailDeliveryDoorAnswersTheRequestNotTheAddress is the public read: the
// id the caller was already answered with, and one of two words about it. Every
// answer has the same shape and the same status, including the ones about an id
// that named nothing, because a door that 404s the misses is a door that lists the
// addresses that were mailed — and a door that answers `sent` about a mailed one is
// a door that lists the addresses that have an account.
func TestTheMailDeliveryDoorAnswersTheRequestNotTheAddress(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	mailer := &verificationFailingMailer{}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.Mailer, d.Mails = mailer, mailLedger()
	})
	const asked = "11111111-1111-4111-8111-111111111111"
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return mailLedger().RecordMail(ctx, tx, notification.MailRecord{
			Kind: contracts.MailVerification, Recipient: "asked@example.com",
			Outcome: notification.MailSent, RequestID: asked,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	ask := func(body string, headers ...func(*http.Request)) (int, string) {
		req := func(r *http.Request) {
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			for _, h := range headers {
				h(r)
			}
		}
		res := call(t, router, "POST", "/api/v1/public/auth/mail-delivery", `{"requestId":"`+body+`"}`, req)
		var out struct {
			State string `json:"state"`
		}
		// A refusal is problem+json, which holds no state field: what a refusal
		// says is in its status, so a body that decodes to nothing is an answer.
		// Every answer that is not a refusal is checked against the state it owes
		// below, which is what still refuses to let a broken body pass.
		_ = json.NewDecoder(res.Result().Body).Decode(&out)
		return res.Code, out.State
	}
	if code, state := ask(asked); code != http.StatusOK || state != notification.MailStatePending {
		// A mail that went is answered exactly as an id that mailed nothing, and
		// that is the point: the routes that caused the mail answer neutrally about
		// the address, so a door that said `sent` here would say who has an account
		// (contracts.MailReport). The row still exists and still says `sent` — what
		// changed is that only a refusal is public.
		t.Errorf("the request that was mailed=%d %q, want 200 pending: a door that answers sent about "+
			"an address undoes the neutral acknowledgment one call later", code, state)
	}
	if code, state := ask("22222222-2222-4222-8222-222222222222"); code != http.StatusOK || state != notification.MailStatePending {
		t.Errorf("an id that mailed nothing=%d %q, want 200 pending", code, state)
	}
	if code, state := ask(""); code != http.StatusOK || state != notification.MailStatePending {
		t.Errorf("no id at all=%d %q, want 200 pending and never somebody else's row", code, state)
	}
	// The one answer this door does give: a transport refused the mail this call
	// asked for, which is the fact the person can act on and the acknowledgment
	// cannot carry. It costs a reason in the row and says none of it out loud.
	const refusedID = "44444444-4444-4444-8444-444444444444"
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return mailLedger().RecordMail(ctx, tx, notification.MailRecord{
			Kind: contracts.MailSetPassword, Recipient: "refused@example.com",
			Outcome: notification.MailFailed, Reason: "the smtp server refused the message",
			RequestID: refusedID,
		})
	})
	if err != nil {
		t.Fatalf("record a refused mail: %v", err)
	}
	if code, state := ask(refusedID); code != http.StatusOK || state != notification.MailFailed {
		t.Errorf("the request whose mail was refused=%d %q, want 200 failed", code, state)
	}
	// A page on another site asking is the one caller this door refuses outright.
	// A caller with no Sec-Fetch-Site at all is not refused: kit/httpx/csrf.go
	// falls back to Origin for a non-browser, and curl is a legitimate caller of a
	// read that names nothing.
	if code, _ := ask(asked, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }); code != http.StatusForbidden {
		t.Errorf("a cross-site ask=%d, want 403", code)
	}

	// An id from another tenant's call is a request that mailed nothing here. Row
	// level security answers it the same way as an id nobody ever minted — 200 and
	// the one word — because a door that distinguished the two would name which
	// ids exist in some other tenant, which is the enumeration this door exists to
	// make impossible. The row is written first, over globex's own transaction,
	// because an ask about an id with no row behind it proves nothing about RLS.
	const elsewhere = "33333333-3333-4333-8333-333333333333"
	err = db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return mailLedger().RecordMail(ctx, tx, notification.MailRecord{
			Kind: contracts.MailVerification, Recipient: "elsewhere@example.com",
			Outcome: notification.MailSent, RequestID: elsewhere,
		})
	})
	if err != nil {
		t.Fatalf("record in globex: %v", err)
	}
	// The answer is the same word an id nobody ever minted gets — which, at this
	// point in the tenant's own record, is `failed`, because the newest mail this
	// tenant sent was refused. It is not `pending`, and that is the cure rather
	// than the leak: a call that left no record is answered from what the
	// transport is doing to this tenant's mail, never from the absence in front of
	// it, because the absence is the shape of "nobody has this address" here and
	// of "not in this tenant" across tenants alike (contracts.MailReport).
	elsewhereAnswer, foreignState := ask(elsewhere)
	mintedAnswer, mintedState := ask("55555555-5555-4555-8555-555555555555")
	if elsewhereAnswer != http.StatusOK {
		t.Errorf("another tenant's request id=%d %q, want a 200 and one word", elsewhereAnswer, foreignState)
	}
	if elsewhereAnswer != mintedAnswer || foreignState != mintedState {
		t.Errorf("another tenant's request id=%d %q, an id nobody minted=%d %q: the two answers differ",
			elsewhereAnswer, foreignState, mintedAnswer, mintedState)
	}

	// And the door is behind the per-address budget that caps spending a link, so
	// somebody working through id after id is refused inside the number the module
	// already agreed to. The asks above spent some of that budget, so the loop
	// asks one at a time until the refusal comes and pins that it comes at all:
	// the promise is the cap, not where this test's own share of it begins.
	var code int
	for i := 0; i < contracts.MailDeliveryAsks; i++ {
		var state string
		if code, state = ask(asked); code == http.StatusTooManyRequests {
			break
		}
		if code != http.StatusOK || state != notification.MailStatePending {
			t.Fatalf("ask %d within the cap=%d %q, want 200 pending", i+1, code, state)
		}
	}
	if code != http.StatusTooManyRequests {
		t.Errorf("%d further asks about a mailed request did not reach the cap of %d",
			contracts.MailDeliveryAsks, contracts.MailDeliveryAsks)
	}

	// And the door spent none of the budget the person needs for the link itself:
	// a shell that polls this read while somebody waits must not be what locks them
	// out of redeeming it. The redemption door still answers its own answer rather
	// than 429, on the same address, immediately after the asks above.
	res := call(t, router, "POST", "/api/v1/public/auth/verify-email", `{"token":"not-a-link"}`, func(r *http.Request) {
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	})
	if res.Code == http.StatusTooManyRequests {
		t.Errorf("the delivery door exhausted the reset-redemption budget: verify-email=%d, want the link's own refusal", res.Code)
	}
}
