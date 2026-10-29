package internal_test

// A person's mail slot says one of their credential links is with the mail host right
// now, and an offer that finds it taken mints nothing. It is an advisory lock, so the
// whole protection is the key: tenant, credential table and person, and nothing else.
//
// Three claims follow, and none of them is the case the slot was built for. The slot
// must not reach across a tenant boundary, because a link acme is being sent is no
// reason for globex to be told none was sent. It must not reach a second person of the
// same tenant, whose own ask has nothing in the air. And it must not answer the ask it
// does refuse with silence, because the door behind that ask reads its answer out of
// the record its own call caused through the transport.
//
// A key of the table alone, or of the person alone, and a refusal that records nothing
// each still pass the case that asks for one link per person. These ask who else the
// lock touches.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
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

// TestAConfirmationAskOfAnotherTenantIsMailedWhileOneTenantsLinkIsInTheAir asks
// whether one tenant's link in the air can be the reason a second tenant is mailed
// nothing. The second tenant's ask leaves its own credential, that credential is the
// row its own tenant holds, and the first tenant's link is neither replaced nor spent.
func TestAConfirmationAskOfAnotherTenantIsMailedWhileOneTenantsLinkIsInTheAir(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	transport := &heldTransport{entered: make(chan struct{}), release: make(chan struct{})}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		emailSignup(d)
		d.Mailer, d.Mails = transport, mailLedger()
	})
	const acmeAddress = "acme-link-in-the-air@example.com"
	away := mailOneTenantsLink(t, conn, router, transport, acmeAddress)
	acmeID := personHoldingAddress(t, admin, acme, acmeAddress)
	hashInAir, _ := confirmationHeldBy(t, admin, acme, acmeID)

	// The other tenant's person, asking while acme's message is still with the host.
	seed(t, conn, globex)
	const globexAddress = "globex-link-asked@example.com"
	globexID := unverifiedPerson(t, conn, globex, globexAddress)
	if err := askForConfirmationLink(t, conn, globex, globexAddress); err != nil {
		t.Errorf("deliver globex's confirmation ask: %v", err)
	}
	close(transport.release)
	if err := <-away; err != nil {
		t.Errorf("deliver acme's sign-up link: %v", err)
	}

	if links := credentialsMailedTo(transport.box.Sent(), globexAddress); len(links) != 1 {
		t.Errorf("globex was mailed %d confirmation links while acme's was in the air, want one: "+
			"one tenant's mail slot must not answer for another: %v", len(links), links)
	} else if !tokenHoldsTheRow(t, admin, globex, globexID, links[0]) {
		t.Errorf("the link globex was mailed names no row globex holds")
	}
	if hash, mailed := confirmationHeldBy(t, admin, acme, acmeID); hash != hashInAir || !mailed {
		t.Errorf("acme's own link came away changed: hash moved %t, marked sent %t", hash != hashInAir, mailed)
	}
}

// TestAConfirmationAskOfAnotherPersonIsMailedWhileOnePersonsLinkIsInTheAir asks the
// same question one level down: the slot belongs to a person, so a second person of the
// same tenant is not queued behind the first one's mail server.
func TestAConfirmationAskOfAnotherPersonIsMailedWhileOnePersonsLinkIsInTheAir(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	transport := &heldTransport{entered: make(chan struct{}), release: make(chan struct{})}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		emailSignup(d)
		d.Mailer, d.Mails = transport, mailLedger()
	})
	const held = "first-person-in-the-air@example.com"
	away := mailOneTenantsLink(t, conn, router, transport, held)
	heldID := personHoldingAddress(t, admin, acme, held)
	hashInAir, _ := confirmationHeldBy(t, admin, acme, heldID)

	const asking = "second-person-asking@example.com"
	unverifiedPerson(t, conn, acme, asking)
	if err := askForConfirmationLink(t, conn, acme, asking); err != nil {
		t.Errorf("deliver the second person's confirmation ask: %v", err)
	}
	close(transport.release)
	if err := <-away; err != nil {
		t.Errorf("deliver the first person's sign-up link: %v", err)
	}

	if links := credentialsMailedTo(transport.box.Sent(), asking); len(links) != 1 {
		t.Errorf("the second person was mailed %d confirmation links while the first one's was in the air,"+
			" want one: a mail slot of one person is not this tenant's queue: %v", len(links), links)
	} else if id := personHoldingAddress(t, admin, acme, asking); !tokenHoldsTheRow(t, admin, acme, id, links[0]) {
		t.Errorf("the link the second person was mailed names no row of theirs")
	}
	if hash, mailed := confirmationHeldBy(t, admin, acme, heldID); hash != hashInAir || !mailed {
		t.Errorf("the first person's link came away changed: hash moved %t, marked sent %t", hash != hashInAir, mailed)
	}
}

// TestAConfirmationAskWhoseMailSlotIsTakenStillHandsOneMessageToTheTransport asks what
// the person whose own link is in the air is answered with. They are not mailed a second
// credential, and they are not left unanswered either: the door behind the ask reads its
// answer out of the record this call caused, so one message of its own leaves to the
// address that asked, and it carries no credential.
func TestAConfirmationAskWhoseMailSlotIsTakenStillHandsOneMessageToTheTransport(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	transport := &heldTransport{entered: make(chan struct{}), release: make(chan struct{})}
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		emailSignup(d)
		d.Mailer, d.Mails = transport, mailLedger()
	})
	const address = "asked-while-in-the-air@example.com"
	away := mailOneTenantsLink(t, conn, router, transport, address)
	id := personHoldingAddress(t, admin, acme, address)
	hashInAir, _ := confirmationHeldBy(t, admin, acme, id)

	if err := askForConfirmationLink(t, conn, acme, address); err != nil {
		t.Errorf("deliver the ask whose mail slot was taken: %v", err)
	}
	close(transport.release)
	if err := <-away; err != nil {
		t.Errorf("deliver the sign-up's confirmation link: %v", err)
	}

	letters := lettersTo(transport.box.Sent(), address)
	if len(letters) != 2 {
		t.Errorf("the address that asked twice was mailed %d messages, want the one link and one"+
			" message this ask caused of its own: %d", len(letters), len(letters))
	}
	if links := credentialsMailedTo(transport.box.Sent(), address); len(links) != 1 ||
		len(links) > 0 && !tokenHoldsTheRow(t, admin, acme, id, links[0]) {
		t.Errorf("confirmation links mailed to the person twice=%d, want the one still in the table", len(links))
	}
	if outcome, rows := mailRecordOf(t, conn, acme, contracts.MailVerificationNoLink); rows != 1 || outcome != notification.MailSent {
		t.Errorf("the ask whose slot was taken recorded %d %s rows with outcome %q,"+
			" want one row saying its own message left", rows, contracts.MailVerificationNoLink, outcome)
	}
	if hash, mailed := confirmationHeldBy(t, admin, acme, id); hash != hashInAir || !mailed {
		t.Errorf("the link that was in the air came away changed: hash moved %t, marked sent %t", hash != hashInAir, mailed)
	}
}

// mailOneTenantsLink registers one address through the public door and delivers the
// sign-up event it raised, returning when the transport holds the credential that
// delivery minted. The channel answers with the delivery's error once the transport
// takes the message; the caller unclogs the transport.
func mailOneTenantsLink(t *testing.T, conn *db.Conn, router chi.Router, transport *heldTransport, address string) <-chan error {
	t.Helper()
	res := call(t, router, http.MethodPost, "/api/v1/public/auth/register", approvalBody(t, address, nil))
	if res.Code != http.StatusAccepted {
		t.Fatalf("registration=%d %s, want 202", res.Code, res.Body.String())
	}
	signUp := events.Event{Name: user.EventRegistrationUnverified, TenantID: acme.ID}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT id, payload FROM platformkit_outbox WHERE name = ?", signUp.Name).
			Row().Scan(&signUp.ID, &signUp.Payload)
	})
	if err != nil {
		t.Fatalf("read the registration event through its tenant: %v", err)
	}
	away := make(chan error, 1)
	go func() { away <- deliverConfirmationEvent(conn, acme, signUp) }()
	select {
	case <-transport.entered:
		return away
	case <-time.After(60 * time.Second):
		t.Fatalf("the mail host was never handed the sign-up link of %s", address)
		return nil
	}
}

// askForConfirmationLink is a person at the resend form: the event that ask raises,
// delivered in the tenant whose account the address belongs to.
func askForConfirmationLink(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, address string) error {
	t.Helper()
	asked, err := json.Marshal(contracts.VerificationRequested{Email: address, At: db.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return deliverConfirmationEvent(conn, tenant, events.Event{
		ID: uuid.New(), Name: contracts.EventVerificationRequested, TenantID: tenant.ID, Payload: asked,
	})
}

// deliverConfirmationEvent runs one auth subscription over one event the way an event
// worker does: the event's tenant, the pool beside it, the delivery's own transaction.
func deliverConfirmationEvent(conn *db.Conn, tenant tenancy.Tenant, event events.Event) error {
	ctx := deliveryContext(context.Background(), conn, tenant)
	for _, sub := range subs {
		if sub.Name != event.Name {
			continue
		}
		return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return sub.Handler(ctx, tx, event)
		})
	}
	return errors.New("no subscriber for " + event.Name)
}

// unverifiedPerson is what the public door's own write leaves, at a named tenant: an
// account with a password, no confirmation, and a member role.
func unverifiedPerson(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, address string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		created, err := realUsers().RegisterUnverified(ctx, tx, user.PasswordRegistration{
			Email: address, DisplayName: "Confirmation asked", Password: authtest.Password, Roles: []string{"member"},
		})
		if err != nil {
			return err
		}
		id = created.ID
		return nil
	})
	if err != nil {
		t.Fatalf("create the unverified account %s at %s: %v", address, tenant.Slug, err)
	}
	return id
}

// personHoldingAddress is the account behind one address in one tenant, read as the
// installation's owner: what a person can reach is a row of a tenant, and a case about
// tenancy that reads through one tenant's window cannot tell a missing row from a hidden one.
func personHoldingAddress(t *testing.T, admin *sql.DB, tenant tenancy.Tenant, address string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := admin.QueryRow("SELECT id FROM users WHERE tenant_id = $1 AND email = $2",
		tenant.ID, address).Scan(&id); err != nil {
		t.Fatalf("the account behind %s at %s: %v", address, tenant.Slug, err)
	}
	return id
}

// confirmationHeldBy reads the credential row one person's confirmation holds in the
// tenant named, and whether a transport has answered for it.
func confirmationHeldBy(t *testing.T, admin *sql.DB, tenant tenancy.Tenant, person uuid.UUID) (hash string, mailed bool) {
	t.Helper()
	err := admin.QueryRow("SELECT token_hash, sent_at IS NOT NULL FROM verification_tokens"+
		" WHERE tenant_id = $1 AND user_id = $2", tenant.ID, person).Scan(&hash, &mailed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read the confirmation row of %s at %s: %v", person, tenant.Slug, err)
	}
	return hash, mailed
}

// tokenHoldsTheRow asks whether the credential a person was mailed is the credential
// that person's tenant still holds: what can be spent is the row, not the envelope.
func tokenHoldsTheRow(t *testing.T, admin *sql.DB, tenant tenancy.Tenant, person uuid.UUID, token string) bool {
	t.Helper()
	var held int
	if err := admin.QueryRow("SELECT count(*) FROM verification_tokens WHERE tenant_id = $1 AND user_id = $2"+
		" AND token_hash = $3", tenant.ID, person, contracts.Hash(token)).Scan(&held); err != nil {
		t.Fatalf("read the credential a person was mailed back: %v", err)
	}
	return held == 1
}

// lettersTo is what left to one address, and credentialsMailedTo is the part of it that
// is a link: a message naming no credential is the sentence saying no link was sent, and
// counting it as a link reads the refusal as a second offer.
func lettersTo(sent []notification.Message, address string) []notification.Message {
	var out []notification.Message
	for _, letter := range sent {
		if letter.To == address {
			out = append(out, letter)
		}
	}
	return out
}

func credentialsMailedTo(sent []notification.Message, address string) []string {
	var links []string
	for _, letter := range lettersTo(sent, address) {
		if token := authtest.TokenIn(letter.Body); token != "" {
			links = append(links, token)
		}
	}
	return links
}

// mailRecordOf is what one tenant's ledger holds for one kind of mail: the newest
// outcome, and how many rows that kind has.
func mailRecordOf(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, kind string) (outcome string, rows int) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Raw("SELECT count(*) FROM direct_mail_deliveries WHERE kind = ?", kind).Row().Scan(&rows); err != nil {
			return err
		}
		if rows == 0 {
			return nil
		}
		return tx.DB().Raw("SELECT outcome FROM direct_mail_deliveries WHERE kind = ?"+
			" ORDER BY seq DESC LIMIT 1", kind).Row().Scan(&outcome)
	})
	if err != nil {
		t.Fatalf("read the record of %s at %s: %v", kind, tenant.Slug, err)
	}
	return outcome, rows
}
