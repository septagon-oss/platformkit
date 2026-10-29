package internal_test

// One transaction can ask twice for one person's link: the forgotten-password
// worker drains a queue of requests inside a single delivery, and the send waits
// for that delivery's commit, so both offers exist before either message leaves.
// Only one row can hold a link at a time, so one of the two offers is a credential
// nobody is being sent. These two cases read the mail against the table.

import (
	"context"
	"database/sql"
	"net/http"
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
)

// askedFor asks for a link over the public door and returns the payload of one of
// the reset requests the outbox now holds, oldest first.
func askedFor(t *testing.T, conn *db.Conn, router http.Handler, nth int) []byte {
	t.Helper()
	res := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot",
		`{"email":"ada@acme.localhost"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("forgot=%d %s, want 200", res.Code, res.Body.String())
	}
	var payload []byte
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT payload FROM platformkit_outbox WHERE name = ? ORDER BY created_at, id"+
			" LIMIT 1 OFFSET ?", contracts.EventResetRequested, nth).Row().Scan(&payload)
	})
	if err != nil {
		t.Fatalf("read the reset request the ask raised: %v", err)
	}
	return payload
}

// resetSubscription is the module's own handler for one reset request.
func resetSubscription(t *testing.T) events.Subscription {
	t.Helper()
	for _, s := range subs {
		if s.Name == contracts.EventResetRequested {
			return s
		}
	}
	t.Fatal("no reset subscription to deliver to")
	return events.Subscription{}
}

// deliverOne runs one reset request in a transaction of its own and commits it,
// which is what hands its link to the transport.
func deliverOne(t *testing.T, conn *db.Conn, handle events.Subscription, payload []byte) error {
	t.Helper()
	return db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return handle.Handler(ctx, tx, events.Event{
			ID: uuid.New(), Name: handle.Name, TenantID: acme.ID, Payload: payload,
		})
	})
}

// mailedLinkNamesTheRowTheTableHolds reads the one mailed link back against the
// table: the credential the message carries is the one row still spendable.
func mailedLinkNamesTheRowTheTableHolds(t *testing.T, admin *sql.DB, body string) {
	t.Helper()
	token := authtest.TokenIn(body)
	if token == "" {
		t.Fatal("the mailed message carries no credential")
	}
	var held int
	if err := admin.QueryRow("SELECT count(*) FROM password_tokens WHERE token_hash = $1",
		contracts.Hash(token)).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if held != 1 {
		t.Errorf("the table holds %d rows for the mailed credential, want the one the person is sent", held)
	}
}

// TestOneSetPasswordAskMailsTheCredentialItsTransactionLeft is the control for the
// case beside it: one ask, delivered and committed, mails one link, and the row
// behind that link is spendable. Without it, a second ask that mailed nothing could
// pass by never reaching the transport at all.
func TestOneSetPasswordAskMailsTheCredentialItsTransactionLeft(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) { d.Mails = mailLedger() })
	person(t, conn, "ada@acme.localhost", contracts.RoleMember)
	payload := askedFor(t, conn, router, 0)

	if err := deliverOne(t, conn, resetSubscription(t), payload); err != nil {
		t.Fatalf("deliver the reset request: %v", err)
	}

	sent := mailbox.Sent()
	if len(sent) != 1 {
		t.Fatalf("links mailed=%d, want the one the committed ask is owed", len(sent))
	}
	mailedLinkNamesTheRowTheTableHolds(t, admin, sent[0].Body)
	if said := len(notices.Sent()); said != 1 {
		t.Errorf("notices raised=%d, want the one that says the link is in the mail", said)
	}
	if outcome, _, rows := outcomeOf(t, conn, contracts.MailSetPassword); rows != 1 || outcome != notification.MailSent {
		t.Errorf("set-password records=%d outcome=%q, want one sent row", rows, outcome)
	}
}

// TestTwoAsksForOneLinkInOneTransactionMailOnlyTheRowTheTableStillHolds is the
// promise the send-on-commit ordering has to keep: a transaction that asks twice for
// one person mails the link the table ends up holding, and nothing else. The offer
// the second ask replaced is a dead credential; mailing it would put a link in an
// envelope that answers "those credentials are not right", and recording it would be
// a delivery nobody owed.
func TestTwoAsksForOneLinkInOneTransactionMailOnlyTheRowTheTableStillHolds(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) { d.Mails = mailLedger() })
	person(t, conn, "ada@acme.localhost", contracts.RoleMember)
	var payloads [][]byte
	for nth := range 2 {
		payloads = append(payloads, askedFor(t, conn, router, nth))
	}
	handle := resetSubscription(t)

	both := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, payload := range payloads {
			if err := handle.Handler(ctx, tx, events.Event{
				ID: uuid.New(), Name: handle.Name, TenantID: acme.ID, Payload: payload,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if both != nil {
		t.Fatalf("deliver both reset requests in one transaction: %v", both)
	}

	sent := mailbox.Sent()
	if len(sent) != 1 {
		t.Fatalf("links mailed=%d, want one: the offer the second ask replaced is nobody's link", len(sent))
	}
	mailedLinkNamesTheRowTheTableHolds(t, admin, sent[0].Body)
	var rows int
	if err := admin.QueryRow("SELECT count(*) FROM password_tokens").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("password tokens=%d, want the one row the single pending write leaves", rows)
	}
	if said := len(notices.Sent()); said != 1 {
		t.Errorf("notices raised=%d, want one for the one link that left", said)
	}
}
