package internal_test

// A confirmation link is a credential of the same kind as a reset link, and the
// module says so: it is minted in a transaction that commits before the message
// leaves, handed over on that commit, and marked sent only once the transport has
// taken it. That ordering leaves one moment open — the send itself — and the person
// who walks into it is the one at the resend form, because nothing happened in their
// browser and they click it again.
//
// The cooldown cannot see that moment: it counts sent_at, and sent_at is written
// after the transport answers. So the second ask reads "nobody was mailed recently",
// mints a replacement over the row whose message is already with the mail host, and
// the single-pending write keeps only the replacement. The first envelope still
// arrives, and the link inside it is the row the table no longer holds.
//
// A reset ask meets this moment behind the person's mail slot, which an offer asks
// before it mints; a confirmation ask meets nothing, because the confirmation offer
// never asks it. The hand-offs queue, and the mint that lands between the first
// hand-off's supersession read and its mark is not caught by anything.
//
// One link may leave. Which one is the module's choice; the two mails, one of them
// dead, are not.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

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
	usermodule "github.com/septagon-oss/platformkit/modules/user"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// confirmationHandover is the module's own handler for one event of this flow,
// delivered the way kit/events delivers it: the event's tenant, the pool beside it.
func confirmationHandover(t *testing.T, conn *db.Conn, event events.Event) error {
	t.Helper()
	for _, sub := range subs {
		if sub.Name != event.Name {
			continue
		}
		return db.Run(deliveryContext(t.Context(), conn, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return sub.Handler(ctx, tx, event)
		})
	}
	t.Fatalf("%s has no subscriber", event.Name)
	return nil
}

// tokenHeldByTheTable hashes a mailed credential and asks whether the confirmation
// table still holds that row: what a person can spend is what the table holds, not
// what an envelope once carried.
func tokenHeldByTheTable(t *testing.T, admin *sql.DB, body string) bool {
	t.Helper()
	token := authtest.TokenIn(body)
	if token == "" {
		t.Fatal("the mailed message carries no credential")
	}
	var held int
	if err := admin.QueryRow("SELECT count(*) FROM verification_tokens WHERE token_hash = $1",
		contracts.Hash(token)).Scan(&held); err != nil {
		t.Fatal(err)
	}
	return held == 1
}

// TestASecondConfirmationAskedWhileTheFirstLinkIsInTheAirMailsOneLink asks what one
// person's mailbox holds when a confirmation link is with the mail host and the same
// person asks for another one. One message carrying a credential arrives, the row it
// names is the row the table holds, and that link still opens the account.
func TestASecondConfirmationAskedWhileTheFirstLinkIsInTheAirMailsOneLink(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	transport := &heldTransport{entered: make(chan struct{}), release: make(chan struct{})}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		emailSignup(d)
		d.Mailer, d.Mails = transport, mailLedger()
	})
	const address = "in-the-air@example.com"
	if res := call(t, router, http.MethodPost, "/api/v1/public/auth/register",
		approvalBody(t, address, nil)); res.Code != http.StatusAccepted {
		t.Fatalf("registration=%d %s, want 202", res.Code, res.Body.String())
	}
	signUp := events.Event{Name: user.EventRegistrationUnverified, TenantID: acme.ID}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT id, payload FROM platformkit_outbox WHERE name = ?", signUp.Name).
			Row().Scan(&signUp.ID, &signUp.Payload)
	}); err != nil {
		t.Fatalf("read the registration event through its tenant: %v", err)
	}
	asked, err := json.Marshal(contracts.VerificationRequested{Email: address, At: db.Now()})
	if err != nil {
		t.Fatal(err)
	}
	resend := events.Event{ID: uuid.New(), Name: contracts.EventVerificationRequested, TenantID: acme.ID, Payload: asked}

	// The sign-up's link, committed and now parked with the transport.
	first := make(chan error, 1)
	go func() { first <- confirmationHandover(t, conn, signUp) }()
	select {
	case <-transport.entered:
	case <-time.After(60 * time.Second):
		t.Fatal("the mail host was never handed the sign-up link, so nothing below was reached")
	}
	hashBefore := confirmationHash(t, admin)

	// The same person asks again while that message is still in the air. The ask is
	// over once it has decided what to mint: either its delivery comes back, or the
	// row in the table has changed under the link still with the host.
	second := make(chan error, 1)
	go func() { second <- confirmationHandover(t, conn, resend) }()
	decided := time.After(60 * time.Second)
	askedAndDecided := false
	for !askedAndDecided {
		select {
		case err := <-second:
			second <- err
			askedAndDecided = true
		case <-time.After(20 * time.Millisecond):
			if confirmationHash(t, admin) != hashBefore {
				askedAndDecided = true
			}
		case <-decided:
			t.Fatal("the second ask never reached its decision, so nothing below was reached")
		}
	}
	close(transport.release)
	if err := <-second; err != nil {
		t.Errorf("deliver the second confirmation ask: %v", err)
	}
	if err := <-first; err != nil {
		t.Errorf("deliver the sign-up's confirmation link: %v", err)
	}

	var links []string
	for _, letter := range transport.box.Sent() {
		if token := authtest.TokenIn(letter.Body); token != "" {
			links = append(links, token)
		}
	}
	if len(links) != 1 {
		t.Errorf("confirmation links mailed=%d, want one: a link minted over the one already"+
			" with the mail host leaves the person two messages, one of which answers"+
			" \"those credentials are not right\": %v", len(links), links)
	}
	if rows := confirmationRows(t, admin); rows != 1 {
		t.Errorf("confirmation tokens=%d, want the one row the single pending write leaves", rows)
	}
	// Whatever left is a message the person may open first, so every link this
	// module handed the transport has to be the row the table holds and open the
	// account it promises to open.
	for _, letter := range transport.box.Sent() {
		token := authtest.TokenIn(letter.Body)
		if token == "" {
			continue
		}
		if !tokenHeldByTheTable(t, admin, letter.Body) {
			t.Errorf("a mailed confirmation link names no row the table holds: the envelope" +
				" arrived with a credential the single pending write replaced")
			continue
		}
		if res := call(t, router, http.MethodPost, "/api/v1/public/auth/verify-email",
			verificationBody(t, token)); res.Code != http.StatusOK {
			t.Errorf("the link the person was mailed stopped working: %d %s", res.Code, res.Body.String())
		}
	}
}

// confirmationHash is the credential the confirmation row holds, as the table keeps
// it, or the zero value while the table holds none.
func confirmationHash(t *testing.T, admin *sql.DB) string {
	t.Helper()
	var hash string
	err := admin.QueryRow("SELECT token_hash FROM verification_tokens").Scan(&hash)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatalf("read the confirmation row: %v", err)
	}
	return hash
}

// confirmationRows counts the confirmation rows this installation holds.
func confirmationRows(t *testing.T, admin *sql.DB) int {
	t.Helper()
	var n int
	if err := admin.QueryRow("SELECT count(*) FROM verification_tokens").Scan(&n); err != nil {
		t.Fatalf("count confirmation rows: %v", err)
	}
	return n
}
