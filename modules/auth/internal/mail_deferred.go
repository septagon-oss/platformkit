package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// linkMail is one credential link this module sends itself, described early enough
// that everything it needs is read inside the transaction that minted the token. It is
// the one description of the send: the dispatch row that records the decision to mail
// and the message that carries the link are both written from it, in that transaction.
//
// The link leaves on the commit and not with the transaction: a mailbox holding a
// credential for a row nobody can read yet is a link that answers "those credentials
// are not right", and a rollback leaves a mail nobody can use. So the message, the
// host it is addressed at and the row it describes are settled before the hand-off,
// and what waits for the commit is the send and its outcome.
type linkMail struct {
	table   string // the credential table this link's row lives in
	kind    string // contracts.MailSetPassword or MailVerification
	person  uuid.UUID
	address string           // the recipient, as the ledger records it
	token   string           // the secret: held to name the row this attempt wrote, and to redact it
	digest  contracts.Digest // sha256 of the token, which is what the row holds
	msg     notificationcontracts.Message
	notice  notificationcontracts.Notice // in-app notice raised beside the link; empty raises none
	was     pendingLink                  // the row as this attempt found it
}

// mailAfterCommit hands the link to the transport once the transaction that made it
// spendable has committed, records what the transport answered, and answers whoever
// asked. It sends on no rollback, and a rollback needs nothing of it: the row this
// describes never was.
//
// What the transport's answer means depends on who asked, and that difference belongs
// to this module. Inside an event delivery the refusal is returned: the claim is
// released beside it (events.Consume), the delivery comes back on the transport's own
// ladder, and the retry mints a fresh link. Outside a delivery nobody retries, so the
// refusal is recorded and swallowed as it was before the send moved.
//
// The send, its marks and this person's mail slot are one transaction that reaches one
// commit (see handOver): no second offer of one person's link overtakes the first, and
// one hand-off costs one connection of the application pool rather than two.
func (s *Service) mailAfterCommit(ctx context.Context, m linkMail) error {
	refused, err := s.handOver(ctx, m)
	if err != nil {
		return err
	}
	if refused == nil {
		return nil
	}
	return s.answerRefused(ctx, m, refused)
}

// handOver carries one link to the transport inside this person's mail slot and writes
// the marks of the transport's answer in the transaction that holds the slot, so the
// message, its record and the release of the slot are one commit.
//
// The slot is an advisory lock and belongs to a transaction, so that transaction has to
// span the mail server. The marks used to be a second transaction of their own, opened
// while the first was parked on the server: two hand-offs at the two connections
// kit/app admits would each hold one connection and wait on the other for the one they
// needed to record a send they had already made — the link gone, the table saying it
// never left, the retry minting a second over the one in the mailbox.
//
// The transport's refusal comes back beside a nil error rather than as it: an error
// here would take the record of the refusal back with it (see answerRefused).
func (s *Service) handOver(ctx context.Context, m linkMail) (refused error, err error) {
	if _, pooled := db.ConnFrom(ctx); !pooled {
		// Nothing to hold a slot on, so nothing to be told about either way: the row this
		// hand-off describes stood or fell with its own transaction, and its outcome has
		// no connection to be recorded on. See detached.
		return s.handOverUnbracketed(ctx, m)
	}
	err = s.detachedWithin(ctx, handoverBudget, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := holdMailSlot(tx, m); err != nil {
			return err
		}
		superseded, err := linkSuperseded(tx, m)
		if err != nil || superseded {
			return err
		}
		sendCtx, cancel := sendWithin(ctx)
		defer cancel()
		if refused = s.mail.Mailer.Send(sendCtx, m.msg); refused != nil {
			return s.undelivered(ctx, m, refused)
		}
		return s.delivered(ctx, m)
	})
	if err != nil {
		return nil, err
	}
	return refused, nil
}

// handOverUnbracketed is the hand-off for a caller that handed over no pool at all: the
// message leaves with nothing held across it, its marks go into a transaction of their
// own, and the mail slot — a lock on a transaction — has nothing to live in. What the
// marks do is what they did before the slot existed, and this caller is told in the log
// rather than failed for a send it already waited for. See detached.
func (s *Service) handOverUnbracketed(ctx context.Context, m linkMail) (refused error, err error) {
	if refused = s.mail.Mailer.Send(ctx, m.msg); refused != nil {
		return refused, s.undelivered(ctx, m, refused)
	}
	return nil, s.delivered(ctx, m)
}

// handoverBudget bounds one credential hand-off: the wait for the person's mail slot,
// the send, and the marks of the transport's answer, which are written in the
// bracketing transaction itself. The slot is an advisory lock and belongs to a
// transaction, so that transaction runs the send inside itself; it is the only shape
// that spans a mail server, so it names its own bound rather than borrowing the write
// budget, which would cancel a real send. The send leaves the last write budget of this
// one to the marks of its answer (see sendWithin).
const handoverBudget = 30 * time.Second

// sendWithin bounds the send inside the hand-off's own bracket so that the transaction
// which has to record the answer is still live when the answer arrives: the mail server
// gets everything left to the bracket but one write budget. The deployment's own
// transport timeout is the number that should bite a server that has stopped answering;
// this one is only the ceiling that keeps the record of a send from being what ran out.
func sendWithin(ctx context.Context) (context.Context, context.CancelFunc) {
	until, ok := ctx.Deadline()
	if !ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, max(time.Until(until)-detachedWriteBudget, 0))
}

// mailSlotKey names the mail slot of one person in one credential table. The hand-off
// that takes the slot and the offer that asks whether to take it build the key here, so
// the two cannot key one fact apart.
func mailSlotKey(tenant, person uuid.UUID, table string) string {
	return "auth/mail/" + table + "/" + tenant.String() + "/" + person.String()
}

// holdMailSlot takes this person's mail slot for the transaction now open, waiting for
// whoever holds it. The wait is for one send to be answered, and what bounds it is the
// hand-off's own budget: a slot that cannot be had by then leaves this hand-off
// uncarried, which is the at-least-once its transport already promises.
func holdMailSlot(tx db.Tx[db.Tenant], m linkMail) error {
	key := mailSlotKey(db.TenantOf(tx).ID, m.person, m.table)
	if err := tx.DB().Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
		return fmt.Errorf("auth: wait for the %s mail slot of %s: %w", m.kind, m.person, err)
	}
	return nil
}

// mailSlotOpen reports whether nobody is handing this person a link right now, and takes
// the slot for the asking transaction when it is free. It asks without waiting: the
// message inside a taken slot is still leaving, and a caller that waited on it would be
// a request parked behind a mail server it does not serve.
func mailSlotOpen(tx db.Tx[db.Tenant], table string, person uuid.UUID) (bool, error) {
	var free bool
	err := tx.DB().Raw("SELECT pg_try_advisory_xact_lock(hashtextextended(?, 0))",
		mailSlotKey(db.TenantOf(tx).ID, person, table)).Row().Scan(&free)
	if err != nil {
		return false, fmt.Errorf("auth: ask whether a %s link is in the air: %w", table, err)
	}
	return free, nil
}

// linkSuperseded asks whether the credential table still holds the row this attempt
// wrote. It reads inside the mail slot, on the transaction that holds it, so the answer
// and the send that follows it are one decision: a row a newer offer has replaced
// answers no, and no second link of one person overtakes the first out of this order.
func linkSuperseded(tx db.Tx[db.Tenant], m linkMail) (bool, error) {
	var held bool
	err := tx.DB().Raw("SELECT EXISTS (SELECT 1 FROM "+m.table+
		" WHERE tenant_id = ? AND user_id = ? AND token_hash = ?)",
		db.TenantOf(tx).ID, m.person, m.digest).Row().Scan(&held)
	if err != nil {
		return false, fmt.Errorf("auth: read the %s link this delivery minted: %w", m.kind, err)
	}
	return !held, nil
}

// delivered leaves the four marks one accepted send is owed — the row's own record
// that the transport took this link, the dispatch record that this module's hand-off
// completed, the notice beside the link, and the ledger record — in the transaction that
// is carrying the hand-off (see marks).
//
// Together, because they describe one fact that is only true once the message left, and
// dropped together for the same reason: once the sweep settled this attempt's row, the record
// of this send is no longer this module's to write (see markDispatch). The notice is raised
// here rather than beside the mint for the same reason: its copy tells the person to check
// their mail, and a sentence promising a mail the transport refused is the lie the record
// exists to make impossible. What the record claims is the carrier's answer, not this
// module's guess: see deliveredOutcome. The dispatch row is the one mark that stays this
// module's own claim: a carrier that filed the message rather than dialling records
// `suppressed` in the ledger and still took the hand-off, and the two say different things.
func (s *Service) delivered(ctx context.Context, m linkMail) error {
	err := s.marks(ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		// The row's own state decides whether this commit is owed at all, so it is settled
		// first and nothing else is written until it has said.
		own, err := markDispatch(tx, m, dispatchSent)
		if err != nil {
			return err
		}
		if !own {
			return errSendAlreadySettled
		}
		if err := markLinkSent(tx, m); err != nil {
			return err
		}
		if s.notify != nil && m.notice.Recipient != uuid.Nil {
			if _, err := s.notify.Notify(ctx, tx, m.notice); err != nil {
				return err
			}
		}
		outcome, reason := s.deliveredOutcome()
		return s.recordMail(ctx, tx, m.kind, m.address, outcome, reason)
	})
	if errors.Is(err, errSendAlreadySettled) {
		// The sweep gave up on this row while the message was with the transport, and what it
		// wrote is terminal, so this module says nothing about the send: no sent_at, no notice,
		// no ledger row. The mail may well have arrived, and the link in it answers until the
		// next offer replaces it, which is the state a crash between the commit and the send
		// leaves — and it has that loss's recovery, the person asking again, whom no cooldown
		// holds off because no cooldown counts a mark nobody wrote. Nothing here failed, so the
		// caller is not answered a failure and no retry mints a second link over the one a
		// transport took; asking again is the only caller owed that.
		slog.WarnContext(ctx, "auth: a link left after its record had been given up on",
			"kind", m.kind, "user", m.person)
		return nil
	}
	if err != nil {
		// The message left and its marks did not commit. The failure goes back to
		// whoever asked: a delivery comes round again and mints a second link, which is
		// the at-least-once its transport always promised, and an ack here would rest
		// on nothing but the luck that the mail is already in the mailbox. A context
		// with no connection is the one case that cannot be repaired by trying again,
		// and the one case nobody but a direct caller of this service ever is: see
		// detached.
		slog.ErrorContext(ctx, "auth: a link left and could not be recorded as mailed",
			"kind", m.kind, "user", m.person, "error", err)
		if errors.Is(err, errNoConnection) {
			return nil
		}
	}
	return err
}

// markLinkSent puts the mark of an accepted send on the row this attempt wrote.
//
// sent_at is the record of which delivery the transport took, and both resend cooldowns
// count it rather than the mint: a link that left is never answered by a second mail
// from a retry that comes round after it, and a row whose mail never reached the
// transport holds nobody's cooldown down. It names this attempt's own hash and rows
// that are still unmarked, so a delivery that comes round again cannot re-date the
// mark of a link somebody else has since been sent.
func markLinkSent(tx db.Tx[db.Tenant], m linkMail) error {
	res := tx.DB().Exec("UPDATE "+m.table+" SET sent_at = clock_timestamp()"+
		" WHERE tenant_id = ? AND user_id = ? AND token_hash = ? AND sent_at IS NULL",
		db.TenantOf(tx).ID, m.person, m.digest)
	if res.Error != nil {
		return fmt.Errorf("auth: record the %s link the transport took: %w", m.kind, res.Error)
	}
	return nil
}

// sentStamp is what sent_at holds in a credential write: the clock for a credential
// whose message the transport already took, and NULL for one that is still only an
// offer and becomes a delivery in markLinkSent.
func sentStamp(delivered bool) string {
	if delivered {
		return "clock_timestamp()"
	}
	return "NULL"
}

// undelivered takes back the row this attempt minted, marks the message this module
// decided to send as withdrawn, and records the refusal in the transaction that is
// carrying the hand-off, and reports only a refusal to write that record. Who is told of
// the link no transport took is decided once it has committed, in answerRefused.
func (s *Service) undelivered(ctx context.Context, m linkMail, failure error) error {
	err := s.marks(ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := restoreUnmailed(ctx, tx, m); err != nil {
			return err
		}
		// What the dispatch row says is not owed to a refusal: a row the sweep settled keeps
		// that state and markDispatch will not move it. The rest of these marks stand either
		// way, because withdrawing a link nobody was sent and recording the refusal are
		// protections rather than claims that a mail left.
		if _, err := markDispatch(tx, m, dispatchWithdrawn); err != nil {
			return err
		}
		return s.recordMail(ctx, tx, m.kind, m.address, notificationcontracts.MailFailed, failure.Error(),
			m.token, string(m.digest))
	})
	if err != nil && !errors.Is(err, errNoConnection) {
		// The record is what an operator reads, so a refusal to write it is the
		// failure that matters: nothing but this line now says the link in that table
		// is one nobody was sent.
		slog.ErrorContext(ctx, "auth: a refused link could not be recorded",
			"kind", m.kind, "user", m.person, "error", err)
		return err
	}
	return nil
}

// answerRefused is what the caller of a link no transport took hears, once the record of
// the refusal has committed.
func (s *Service) answerRefused(ctx context.Context, m linkMail, failure error) error {
	if _, fromDelivery := events.DeliveryOf(ctx); !fromDelivery {
		return nil
	}
	// A transport retries a delivery, and retries nothing else: a request that
	// waited on a mail server would be a request whose answer depends on a machine
	// nobody here controls.
	//
	// What it is told is the redacted sentence, not the transport's own. A failure
	// that ends the ladder becomes a dead letter, and a dead letter is a row: the
	// cause is written down and read back for a week, and the transport's error
	// repeats what it was handed, which is a link carrying a live credential. The
	// module that minted the secret is the one that knows it, so the module is the
	// one that scrubs it — the same redaction the mail ledger's reason carries, from
	// the same two values. The failure itself still comes back as a failure: this
	// delivery is owed an effect nobody carried out, and an acknowledgement here
	// would be the loss this mark exists to refuse.
	return fmt.Errorf("auth: the mail transport refused a link after its token committed: %s",
		notificationcontracts.RedactMailReason(failure.Error(), m.token, string(m.digest)))
}

// writeGrace is how long a credential write waits for a row another transaction holds
// before this module concludes that the transaction holding it is the one asking. The
// write is its own, so waiting longer would be a delivery parked behind itself.
const writeGrace = "SET LOCAL lock_timeout = '500ms'"

// isBlockedByOwnWrite is Postgres refusing to wait for a row lock (55P03) inside that
// grace. The only writer that can hold a person's credential row against the transaction
// asking for it is that transaction itself, still holding a mint it has not committed.
func isBlockedByOwnWrite(err error) bool {
	pg, isPostgres := errors.AsType[*pgconn.PgError](err)
	return isPostgres && pg.Code == "55P03"
}

// errNoConnection says this work was handed no connection to record its outcome on.
// A request carries one (kit/httpx), an event delivery carries one (kit/events), and
// anything else that reaches these two mails is code calling the service directly,
// which is told in the log and is not failed for it: the send it waited for has
// already happened, and a second attempt could not find a pool the first one lacked.
var errNoConnection = errors.New("auth: no connection to record a mail delivery on")

// marks writes the outcome one send reached, wherever the hand-off stands. Inside its
// bracketing transaction the write joins that transaction, so the record of the send and
// the mail slot it protected come away on one connection and in one commit; outside one
// it is a transaction of its own. Either way the write is a statement and keeps the write
// budget: a row somebody else is holding must not turn this hand-off into a connection
// parked on the pool for the bracket's whole bound. A hand-off whose answer went unwritten
// is a mailbox holding a link the table never marked, which the next offer mints a second
// link over — so this join is what the bracket exists to make.
func (s *Service) marks(ctx context.Context, fn func(context.Context, db.Tx[db.Tenant]) error) error {
	if _, open := db.TxFrom(ctx); !open {
		return s.detached(ctx, fn)
	}
	conn, ok := db.ConnFrom(ctx)
	if !ok {
		return errNoConnection
	}
	written, cancel := context.WithTimeout(ctx, detachedWriteBudget)
	defer cancel()
	return db.Run(written, conn, fn)
}

// detached runs one write on the application's connection, outside the transaction
// this work was handed. The context keeps everything but the transaction — the
// tenant, the caller, the trace — and the deadline of the attempt that has ended is
// taken off too, because the step it was bounding is already over.
func (s *Service) detached(ctx context.Context, fn func(context.Context, db.Tx[db.Tenant]) error) error {
	return s.detachedWithin(ctx, detachedWriteBudget, fn)
}

// detachedWithin is detached with its bound named by the caller: a mark is a statement
// and gets the write budget, and a hand-off that holds a transaction across a mail
// server names the longer bound that fits what it actually waits for.
func (s *Service) detachedWithin(ctx context.Context, budget time.Duration,
	fn func(context.Context, db.Tx[db.Tenant]) error) error {
	conn, ok := db.ConnFrom(ctx)
	if !ok {
		return errNoConnection
	}
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), budget)
	defer cancel()
	return db.Run(detached, conn, fn)
}

// restoreUnmailed puts a person's credential row back the way this attempt found it,
// because the transport refused the message this one minted. Two wrongs are refused:
// leaving a live token nobody was sent, and destroying the link that person was sent
// five minutes ago because a mail server refused them a second one. Which of the two
// it is reads off sent_at alone — the row this attempt found was somebody's link only
// if a transport took its mail. Withdrawing is also what frees the next ask: the
// resend cooldown counts delivered links, and the row it would have counted is the one
// nobody was sent.
//
// Every statement carries this attempt's hash, so it writes the row this attempt wrote
// and nothing else: a link minted and mailed while the send was in the air is left
// where the person who received it can reach it. A process that died between the
// commit and this write leaves one unsent row, which caps nobody, holds no live link,
// and is replaced by the next offer or expires on its own clock.
func restoreUnmailed(_ context.Context, tx db.Tx[db.Tenant], m linkMail) error {
	tenant, where := db.TenantOf(tx).ID, " WHERE tenant_id = ? AND user_id = ? AND token_hash = ?"
	var res *gorm.DB
	if !m.was.there || !m.was.sent.Valid {
		res = tx.DB().Exec("DELETE FROM "+m.table+where, tenant, m.person, m.digest)
	} else {
		res = tx.DB().Exec("UPDATE "+m.table+" SET token_hash = ?, created_at = ?, expires_at = ?, sent_at = ?"+where,
			m.was.hash, m.was.created, m.was.expires, m.was.sent.Time, tenant, m.person, m.digest)
	}
	if res.Error != nil {
		return fmt.Errorf("auth: withdraw the %s link nobody was sent: %w", m.kind, res.Error)
	}
	return nil
}
