package internal_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

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
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// crashingMailer is a transport that stops between the socket and the answer: it remembers
// what it was handed, then takes the process down with it, which is what a lost process
// does to a credential that existed only in its memory.
type crashingMailer struct {
	box   authtest.Mailbox
	crash bool
	token string
}

func (m *crashingMailer) Send(ctx context.Context, msg notification.Message) error {
	m.token = authtest.TokenIn(msg.Body)
	if m.crash {
		m.crash = false
		panic("the mail transport stopped before accepting the message")
	}
	return m.box.Send(ctx, msg)
}

func (m *crashingMailer) Available() bool { return true }

// TestACrashBeforeTheSendLeavesTheLinkUnsentAndLetsThePersonAskAgainNow is what this
// table exists for. The credential committed, the plaintext reached the memory of the one
// process that could hand it to a mail server, and that process stopped: the row stays
// unsent, which is not a claim that the message never left but a statement that nobody can
// say either way, and after the grace it becomes unknown, which no code may ever turn into
// sent.
//
// Nothing re-sends it, because nothing can. What the row buys instead is that the person
// asking again is not held off: both resend cooldowns count the mark a transport wrote, and
// a lost send wrote none — so the very next ask mints and mails a replacement, and the link
// in the mailbox that never arrived stops working the moment its replacement is written.
func TestACrashBeforeTheSendLeavesTheLinkUnsentAndLetsThePersonAskAgainNow(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	mailer := &crashingMailer{crash: true}
	users := realUsers()
	svc := internal.NewService(users, nil, internal.Delivery{
		Mailer: mailer, MailDelivery: mailer, Hosts: authtest.Host("acme.example.com"), Secure: true,
	})
	seed(t, conn, acme)
	const address = "dispatch-crashed@acme.example.com"
	person := invitedPerson(t, conn, users, address)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	reissue := func() error {
		return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return svc.Reissue(ctx, tx, address)
		})
	}

	// The transport stops inside the send, after the mint committed and before the marks
	// ran, and nothing in the runtime recovers it: the database is left holding exactly
	// what a lost process leaves.
	lost := func() (recovered bool) {
		defer func() { recovered = recover() != nil }()
		return reissue() == nil
	}
	if !lost() {
		t.Fatal("the interrupted send came back instead of stopping the process")
	}
	token := mailer.token
	if token == "" {
		t.Fatal("the interrupted transport never saw a credential, so it proved nothing")
	}
	state, _, settled := dispatchOf(t, admin, contracts.Hash(token))
	if state != "unsent" {
		t.Errorf("state after the crash = %q, want unsent", state)
	}
	if settled.Valid {
		t.Errorf("a send nobody answered settled at %v, want nothing recorded", settled.Time)
	}
	for _, letter := range mailer.box.Sent() {
		if authtest.TokenIn(letter.Body) == token {
			t.Error("the interrupted transport filed the message it never accepted")
		}
	}
	var sentAt sql.NullTime
	row(t, admin, "SELECT sent_at FROM password_tokens WHERE user_id = $1", person).Scan(&sentAt)
	if sentAt.Valid {
		t.Error("the lost send marked its credential delivered")
	}

	// The grace passes on the database's clock, moved here rather than slept through.
	exec(t, admin, "UPDATE auth_mail_dispatch SET created_at = created_at - interval '10 minutes'")
	if _, err := purgeOnce(t, conn, svc); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	state, _, settled = dispatchOf(t, admin, contracts.Hash(token))
	if state != "unknown" || !settled.Valid {
		t.Errorf("after the grace the row reads %q settled=%v, want unknown and settled", state, settled)
	}

	// The same person, asking again right now.
	if err := reissue(); err != nil {
		t.Fatalf("the person whose link was lost could not ask again: %v", err)
	}
	replacement := mailer.token
	if replacement == "" || replacement == token {
		t.Fatal("the recovery mailed no new credential")
	}
	if got := len(mailer.box.Sent()); got != 1 {
		t.Errorf("recovery mailed %d messages, want exactly one", got)
	}
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Reset(ctx, tx, token, authtest.Password, nobody)
		return err
	})
	if !errors.Is(err, contracts.ErrCredentials) {
		t.Errorf("the lost link answers %v, want %v now that a replacement stands", err, contracts.ErrCredentials)
	}
	// The replacement is a rotation of a link this person could be holding, and a
	// rotation mails before it writes: its evidence can only be written by the transaction
	// that already has the transport's answer, which is where sent_at, the notice and the
	// ledger row are already written. So one row is still the right count.
	if n := countDispatched(t, admin); n != 1 {
		t.Errorf("dispatch rows = %d, want the one lost send: a rotation is the documented exception", n)
	}
}

func purgeOnce(t *testing.T, conn *db.Conn, svc *internal.Service) (int64, error) {
	t.Helper()
	var gone int64
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		gone, err = svc.Purge(ctx, tx)
		return err
	})
	return gone, err
}

// blockingDispatchMailer is a mail server that is still answering when the test needs the
// hand-off to be open: it says it was entered, then waits to be released. It answers nil
// every time, so a case may send again once it has been released — its own control, on the
// same wiring — and only the first send announces itself.
type blockingDispatchMailer struct {
	entered chan struct{}
	release chan struct{}
	token   string
}

func (m *blockingDispatchMailer) Send(ctx context.Context, msg notification.Message) error {
	m.token = authtest.TokenIn(msg.Body)
	select {
	case <-m.entered:
	default:
		close(m.entered)
	}
	select {
	case <-m.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *blockingDispatchMailer) Available() bool { return true }

// TestAMarkThatFoundItsRowAlreadyUnknownSaysNothingAgain races the two writers of one
// dispatch row: the hand-off that is still carrying the message, and the sweep that has
// given up on it. The row lock and the shared insistence on unsent decide it, and the loser
// writes nothing — so the row holds one state and one settled_at rather than the later
// writer's story over the earlier one's.
//
// The direction matters more than the winner: a hand-off that outlived the grace records a
// mail that may well have left as unknown, which is an honest "we do not know" rather than
// a claim about the world it can no longer prove. What comes back with the row's own claim
// is the whole set of marks it carried — `sent_at`, the notice, the ledger row — and the
// control at the end is what makes those three zeros mean anything.
func TestAMarkThatFoundItsRowAlreadyUnknownSaysNothingAgain(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	mailer := &blockingDispatchMailer{entered: make(chan struct{}), release: make(chan struct{})}
	recorder, _ := notificationmodule.New(notificationmodule.Deps{Mailer: notificationmodule.NewMailbox()})
	users := realUsers()
	svc := internal.NewService(users, recorder, internal.Delivery{
		Mailer: mailer, MailDelivery: mailer, Mails: recorder, Hosts: authtest.Host("acme.example.com"), Secure: true,
	})
	seed(t, conn, acme)
	const address = "dispatch-race@acme.example.com"
	person := invitedPerson(t, conn, users, address)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	done := make(chan error, 1)
	go func() {
		done <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return svc.Reissue(ctx, tx, address)
		})
	}()

	select {
	case <-mailer.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the hand-off never reached the transport")
	}
	token := mailer.token
	exec(t, admin, "UPDATE auth_mail_dispatch SET created_at = created_at - interval '10 minutes'")
	if _, err := purgeOnce(t, conn, svc); err != nil {
		t.Fatalf("sweep while a hand-off was open: %v", err)
	}
	close(mailer.release)
	if err := <-done; err != nil {
		t.Fatalf("the hand-off: %v", err)
	}

	var raced []string
	var settled sql.NullTime
	err := admin.QueryRowContext(t.Context(),
		"SELECT array_agg(state), min(settled_at) FROM auth_mail_dispatch WHERE token_hash = $1",
		contracts.Hash(token)).Scan(pq.Array(&raced), &settled)
	if err != nil {
		t.Fatalf("read the raced row: %v", err)
	}
	if len(raced) != 1 || raced[0] != "unknown" {
		t.Errorf("the raced row reads %v, want the sweep's unknown and nothing later", raced)
	}
	if !settled.Valid {
		t.Error("the row the sweep settled lost its settled_at")
	}
	if n := countDispatched(t, admin); n != 1 {
		t.Errorf("dispatch rows = %d, want one row for one decided message", n)
	}
	// The notice and the ledger row are marks of that same message, so they come back with
	// sent_at: a notice telling this person to check mail beside a record that says nothing
	// is known would be the lie the dispatch row exists to make impossible.
	if notices, records, marked := sendRecord(t, conn, person, address); notices != 0 || records != 0 || marked != 0 {
		t.Errorf("the losing hand-off left notices=%d ledger rows=%d sent marks=%d, want none of each",
			notices, records, marked)
	}
	// The control, on the same three counters and the same recorder: an offer answered
	// normally after the row was settled has to move all three, or the zeros above would be
	// a wiring that records nothing rather than a hand-off that kept its marks back.
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Reissue(ctx, tx, address)
	}); err != nil {
		t.Fatalf("the control offer: %v", err)
	}
	if notices, records, marked := sendRecord(t, conn, person, address); notices != 1 || records != 1 || marked != 1 {
		t.Errorf("the answered offer left notices=%d ledger rows=%d sent marks=%d, want one of each",
			notices, records, marked)
	}
}

// sendRecord reads, in one tenant-scoped transaction, the three records an accepted send
// owes besides its dispatch row: notices raised, mails in the ledger for this address, and
// credential rows carrying sent_at.
func sendRecord(t *testing.T, conn *db.Conn, person uuid.UUID, address string) (notices, records, marked int) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Raw("SELECT count(*) FROM notifications WHERE recipient_id = ?", person).Row().Scan(&notices); err != nil {
			return err
		}
		if err := tx.DB().Raw("SELECT count(*) FROM direct_mail_deliveries WHERE kind = ? AND recipient = ?",
			contracts.MailSetPassword, contracts.EmailKey(address)).Row().Scan(&records); err != nil {
			return err
		}
		return tx.DB().Raw("SELECT count(*) FROM password_tokens WHERE user_id = ? AND sent_at IS NOT NULL",
			person).Row().Scan(&marked)
	})
	if err != nil {
		t.Fatalf("read the records of one send: %v", err)
	}
	return notices, records, marked
}

// TestTheSweepForgetsASettledDispatchRowAndNeverAnUnsettledOne is the retention rule in
// both directions. Ninety days after a transport answered, the row is history and goes; a
// row nobody ever answered is the evidence of a lost send and stays whatever its age,
// because a sweep that deleted it would be the mechanism that hides the loss this table
// exists to make visible.
func TestTheSweepForgetsASettledDispatchRowAndNeverAnUnsettledOne(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	box := &authtest.Mailbox{}
	users := realUsers()
	svc := internal.NewService(users, nil, mailedDelivery(box))
	seed(t, conn, acme)
	const address = "dispatch-retention@acme.example.com"
	person := invitedPerson(t, conn, users, address)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Offer(ctx, tx, person)
	}); err != nil {
		t.Fatal(err)
	}
	sent := contracts.Hash(authtest.TokenIn(box.Sent()[0].Body))
	// A lost send, aged the way ninety-one days ages one. The credential it names is
	// still standing, which is the state a crash leaves, and only its record is aged.
	lost := contracts.Hash("a credential no memory holds any more")
	exec(t, admin, "UPDATE auth_mail_dispatch SET settled_at = settled_at - interval '91 days' WHERE token_hash = $1", sent)
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO auth_mail_dispatch (tenant_id, user_id, token_hash, kind, recipient, state)"+
			" VALUES ($1, $2, $3, $4, $5, 'unsent')", acme.ID, person, lost, contracts.MailSetPassword, address); err != nil {
		t.Fatalf("seed an unsettled row: %v", err)
	}
	exec(t, admin, "UPDATE auth_mail_dispatch SET created_at = created_at - interval '91 days' WHERE token_hash = $1", lost)

	if _, err := purgeOnce(t, conn, svc); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	var kept int
	row(t, admin, "SELECT count(*) FROM auth_mail_dispatch WHERE token_hash = $1", sent).Scan(&kept)
	if kept != 0 {
		t.Error("the sweep kept a settled row past its horizon")
	}
	state, _, settled := dispatchOf(t, admin, lost)
	if state != "unknown" {
		t.Errorf("the unsettled row reads %q, want the sweep's unknown", state)
	}
	if !settled.Valid {
		t.Error("the unsettled row was deleted or left unsettled rather than settled as unknown")
	}
}
