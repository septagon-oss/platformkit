package internal_test

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// dispatchOf is one credential mail's row, read by the credential it names. The admin
// connection is the one role that sees a table no route serves: this module publishes no
// read over dispatch rows, on purpose, so a case reads them the way an operator does.
func dispatchOf(t *testing.T, admin *sql.DB, digest contracts.Digest) (state, recipient string, settled sql.NullTime) {
	t.Helper()
	err := admin.QueryRowContext(t.Context(),
		"SELECT state, recipient, settled_at FROM auth_mail_dispatch WHERE token_hash = $1", digest).
		Scan(&state, &recipient, &settled)
	if err != nil {
		t.Fatalf("read the dispatch row this run decided: %v", err)
	}
	return state, recipient, settled
}

// countDispatched is the whole table, which is the question every case below asks: one
// decided message is one row, and a mint that never decided to mail is none.
func countDispatched(t *testing.T, admin *sql.DB) int {
	t.Helper()
	var n int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM auth_mail_dispatch").Scan(&n); err != nil {
		t.Fatalf("count the dispatch rows: %v", err)
	}
	return n
}

// mailedDelivery is the composition a case that wants a record wires: the recorder as the
// carrier and the notification module's own ledger beside it, which is what apps/platformkit
// hands the module rather than a fixture-only shape.
func mailedDelivery(box *authtest.Mailbox) internal.Delivery {
	return internal.Delivery{
		Mailer: box, MailDelivery: box, Hosts: authtest.Host("acme.example.com"),
		Secure: true, Mails: mailLedger(),
	}
}

func invitedPerson(t *testing.T, conn *db.Conn, users usercontracts.Service, address string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := users.Invite(ctx, tx, address, "Invited")
		if err == nil {
			id = u.ID
		}
		return err
	})
	if err != nil {
		t.Fatalf("invite %s: %v", address, err)
	}
	return id
}

// TestACommittedCredentialLeavesOneDispatchRowInTheStateItsTransportSaid is the whole
// claim of the table in one run: the record of the mail is written by the transaction that
// minted the credential, so a committed link has exactly one row naming it, and the
// hand-off marked that row with the answer the transport gave.
//
// It fails if the write moves into the deferred effect — the row would then be missing for
// as long as the send is in the air, and a retried effect would leave two rows where the
// primary key is supposed to make one — or if the mark moves out of the hand-off's own
// transaction, which is the two-commits-that-can-disagree this module already rules out
// for sent_at.
func TestACommittedCredentialLeavesOneDispatchRowInTheStateItsTransportSaid(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notificationmodule.Migrations, auth.Migrations)
	box := &authtest.Mailbox{}
	users := realUsers()
	svc := internal.NewService(users, nil, mailedDelivery(box))
	seed(t, conn, acme)
	const address = "dispatch-committed@acme.example.com"
	person := invitedPerson(t, conn, users, address)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)

	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Offer(ctx, tx, person)
	}); err != nil {
		t.Fatal(err)
	}

	sent := box.Sent()
	if len(sent) != 1 {
		t.Fatalf("the offer mailed %d messages, want one", len(sent))
	}
	token := authtest.TokenIn(sent[0].Body)
	if token == "" {
		t.Fatal("the message carried no credential to name the row by")
	}
	state, recipient, settled := dispatchOf(t, admin, contracts.Hash(token))
	if state != "sent" {
		t.Errorf("dispatch state = %q, want sent", state)
	}
	if !settled.Valid {
		t.Error("an answered send left its row unsettled")
	}
	if recipient != address {
		t.Errorf("dispatch recipient = %q, want %q", recipient, address)
	}
	if n := countDispatched(t, admin); n != 1 {
		t.Errorf("dispatch rows = %d, want one per decided message", n)
	}
	if outcome, reason, rows := outcomeOf(t, conn, contracts.MailSetPassword); rows != 1 || outcome != notification.MailSent || reason != "" {
		t.Errorf("mail records = %d outcome=%q reason=%q, want one sent row with nothing to say", rows, outcome, reason)
	}
}

// TestARolledBackRequestWritesNoDispatchRowAndSendsNothing is the other half of the same
// sentence: the record of the mail has exactly the lifetime of the credential it names,
// because it is written by the same transaction. A rollback therefore leaves no token, no
// dispatch row and no message — not a live link whose record vanished, and not a record of
// a mail nobody can use.
func TestARolledBackRequestWritesNoDispatchRowAndSendsNothing(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notificationmodule.Migrations, auth.Migrations)
	box := &authtest.Mailbox{}
	users := realUsers()
	svc := internal.NewService(users, nil, mailedDelivery(box))
	seed(t, conn, acme)
	person := invitedPerson(t, conn, users, "dispatch-rollback@acme.example.com")
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)

	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := svc.Offer(ctx, tx, person); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("the offer answered a rollback with %v", err)
	}
	if n := countDispatched(t, admin); n != 0 {
		t.Errorf("dispatch rows after a rollback = %d, want none", n)
	}
	var tokens int
	row(t, admin, "SELECT count(*) FROM password_tokens").Scan(&tokens)
	if tokens != 0 || len(box.Sent()) != 0 {
		t.Errorf("a rolled-back offer left %d credentials and mailed %d messages, want neither",
			tokens, len(box.Sent()))
	}
}

// refusingDispatchMailer is the transport that answers. Its sentence quotes the link it
// was handed, which is what the record's redaction is measured against.
type refusingDispatchMailer struct {
	refuse bool
	token  string
}

func (m *refusingDispatchMailer) Send(_ context.Context, msg notification.Message) error {
	m.token = authtest.TokenIn(msg.Body)
	if !m.refuse {
		return nil
	}
	return errors.New("relay: 550 could not deliver " + msg.To + " " + authtest.TokenIn(msg.Body))
}

func (m *refusingDispatchMailer) Available() bool { return true }

// TestARefusedSendWithdrawsTheDispatchRowBesideTheCredentialItRestored checks that the
// record of a message no transport took is written in the same breath as the undoing: the
// row becomes withdrawn, the credential the person held comes back exactly as this attempt
// found it, and the transport's own words reach the ledger with the credential cut out of
// them.
//
// The previous link is read rather than spent: this attempt could only be made once that
// link had passed its expiry, and an expired credential answers ErrCredentials whatever its
// row says, so the claim this case can make is the stronger one — every column of the row
// is the one the offer found.
func TestARefusedSendWithdrawsTheDispatchRowBesideTheCredentialItRestored(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notificationmodule.Migrations, auth.Migrations)
	mailer := &refusingDispatchMailer{}
	users := realUsers()
	svc := internal.NewService(users, nil, internal.Delivery{
		Mailer: mailer, MailDelivery: mailer, Hosts: authtest.Host("acme.example.com"),
		Secure: true, Mails: mailLedger(),
	})
	seed(t, conn, acme)
	const address = "dispatch-refused@acme.example.com"
	person := invitedPerson(t, conn, users, address)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	reissue := func() {
		t.Helper()
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return svc.Reissue(ctx, tx, address)
		}); err != nil {
			t.Fatalf("reissue: %v", err)
		}
	}

	reissue()
	first := mailer.token
	if first == "" {
		t.Fatal("the first offer mailed no credential")
	}
	// Out of both the cooldown and the "somebody may be holding this" answer, so this
	// offer writes ahead of its send as an offer of a credential nobody holds does.
	exec(t, admin, "UPDATE password_tokens SET created_at = created_at - interval '2 hours',"+
		" expires_at = expires_at - interval '2 hours', sent_at = sent_at - interval '2 hours'")
	// The row as this attempt finds it, which is what it has to put back.
	before := heldCredential(t, admin, person)
	if !before.sent.Valid {
		t.Fatal("the first link was never marked delivered, so this offer would replace no held row")
	}
	mailer.refuse = true
	reissue()

	second := mailer.token
	if second == "" || second == first {
		t.Fatal("the refused offer mailed no new credential, so it decided no message")
	}
	state, _, settled := dispatchOf(t, admin, contracts.Hash(second))
	if state != "withdrawn" {
		t.Errorf("refused send left state = %q, want withdrawn", state)
	}
	if !settled.Valid {
		t.Error("a refused send left its row unsettled")
	}
	if state, _, _ := dispatchOf(t, admin, contracts.Hash(first)); state != "sent" {
		t.Errorf("the first send's row reads %q after a later refusal, want its own sent", state)
	}
	outcome, reason, rows := outcomeOf(t, conn, contracts.MailSetPassword)
	if rows != 2 || outcome != notification.MailFailed {
		t.Fatalf("mail records = %d outcome=%q, want a sent row and a failed one", rows, outcome)
	}
	if !strings.Contains(reason, "could not deliver") || strings.Contains(reason, second) {
		t.Errorf("the refusal recorded %q, want the transport's words without its credential", reason)
	}
	if after := heldCredential(t, admin, person); after != before {
		t.Errorf("the refused offer left the credential row as %+v, want it back as the offer found it: %+v", after, before)
	}
	if want := hex.EncodeToString(contracts.Hash(first)); before.hash != want {
		t.Errorf("the row put back names %s, want the link the person was sent: %s", before.hash, want)
	}
}

// credentialRow is one person's pending set-password row, in the shape this module reads
// it and puts it back: the hash, its three timestamps, as the database spells them.
type credentialRow struct {
	hash    string
	created string
	expires string
	sent    sql.NullTime
}

func heldCredential(t *testing.T, admin *sql.DB, person uuid.UUID) credentialRow {
	t.Helper()
	var out credentialRow
	row(t, admin, "SELECT encode(token_hash, 'hex'), created_at::text, expires_at::text, sent_at"+
		" FROM password_tokens WHERE user_id = $1", person).
		Scan(&out.hash, &out.created, &out.expires, &out.sent)
	return out
}

// TestAFilingCarrierRecordsASuppressedSendAndASettledDispatchRow keeps the two claims a
// carrier can make in two columns. A deployment with no mail host files the message instead
// of dialling: the honest ledger row is suppressed, because the relay delivered nothing to
// the world, and the honest dispatch row is settled, because this module's hand-off
// completed and no memory is holding a plaintext any more. Neither record is allowed to
// write the other's word, and this case is where that shared decision is visible.
func TestAFilingCarrierRecordsASuppressedSendAndASettledDispatchRow(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notificationmodule.Migrations, auth.Migrations)
	box := notificationmodule.NewMailbox()
	users := realUsers()
	svc := internal.NewService(users, nil, internal.Delivery{
		Mailer: box, MailDelivery: box, Hosts: authtest.Host("acme.example.com"),
		Secure: true, Mails: mailLedger(),
	})
	seed(t, conn, acme)
	person := invitedPerson(t, conn, users, "dispatch-filed@acme.example.com")
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Offer(ctx, tx, person)
	}); err != nil {
		t.Fatal(err)
	}

	filed := box.Sent()
	if len(filed) != 1 {
		t.Fatalf("the filing carrier kept %d messages, want the one it was handed", len(filed))
	}
	state, _, settled := dispatchOf(t, admin, contracts.Hash(authtest.TokenIn(filed[0].Body)))
	if state != "sent" || !settled.Valid {
		t.Errorf("a completed hand-off left state = %q settled = %v, want sent and settled", state, settled)
	}
	if outcome, _, rows := outcomeOf(t, conn, contracts.MailSetPassword); rows != 1 || outcome != notification.MailSuppressed {
		t.Errorf("mail records = %d outcome=%q, want one suppressed row: no relay dialled", rows, outcome)
	}
}
