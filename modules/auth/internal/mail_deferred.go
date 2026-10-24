package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// linkMail is one credential link this module sends itself, described early enough
// that everything it needs is read inside the transaction that minted the token.
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
// spendable has committed, and records what the transport answered in a transaction
// of its own. It sends on no rollback, and a rollback needs nothing of it: the row
// this describes never was.
//
// What the transport's answer means depends on who asked, and that difference belongs
// to this module. Inside an event delivery the refusal is returned: the claim is
// released beside it (events.Consume), the delivery comes back on the transport's own
// ladder, and the retry mints a fresh link. Outside a delivery nobody retries, so the
// refusal is recorded and swallowed as it was before the send moved.
func (s *Service) mailAfterCommit(ctx context.Context, m linkMail) error {
	gone, err := s.linkSuperseded(ctx, m)
	if err != nil {
		return err
	}
	if gone {
		// A later offer replaced this row between its commit and this send — one
		// transaction can mint both, because the send waits for that commit. The link the
		// table holds is the one its recipient is being sent; mailing this one would put a
		// dead credential in an envelope and record a delivery nobody was owed.
		return nil
	}
	if err := s.mail.Mailer.Send(ctx, m.msg); err != nil {
		return s.undelivered(ctx, m, err)
	}
	return s.delivered(ctx, m)
}

// linkSuperseded asks whether the credential table still holds the row this attempt
// wrote. It reads on the pool, after the commit that made the row spendable, so a row a
// newer offer has replaced answers no. A caller with no pool cannot be told either way:
// the row it wrote stood or fell with its own transaction, so that one sends.
func (s *Service) linkSuperseded(ctx context.Context, m linkMail) (bool, error) {
	var held bool
	err := s.detached(ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT EXISTS (SELECT 1 FROM "+m.table+
			" WHERE tenant_id = ? AND user_id = ? AND token_hash = ?)",
			db.TenantOf(tx).ID, m.person, m.digest).Row().Scan(&held)
	})
	if errors.Is(err, errNoConnection) {
		return false, nil
	}
	return !held, err
}

// delivered leaves the three marks one accepted send is owed — the row's own record
// that the transport took this link, the notice beside the link, and the ledger
// record — in one transaction of its own.
//
// Together, because they describe one fact that is only true once the message left.
// The notice is raised here rather than beside the mint for the same reason: its copy
// tells the person to check their mail, and a sentence promising a mail the transport
// refused is the lie the record exists to make impossible.
func (s *Service) delivered(ctx context.Context, m linkMail) error {
	err := s.detached(ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := markLinkSent(tx, m); err != nil {
			return err
		}
		if s.notify != nil && m.notice.Recipient != uuid.Nil {
			if _, err := s.notify.Notify(ctx, tx, m.notice); err != nil {
				return err
			}
		}
		return s.recordMail(ctx, tx, m.kind, m.address, notificationcontracts.MailSent, "")
	})
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

// undelivered takes back the row this attempt minted and records the refusal in one
// transaction of its own, and then decides who is told.
func (s *Service) undelivered(ctx context.Context, m linkMail, failure error) error {
	err := s.detached(ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := restoreUnmailed(ctx, tx, m); err != nil {
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
	if _, fromDelivery := events.DeliveryOf(ctx); fromDelivery {
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
	return nil
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

// detached runs one write on the application's connection, outside the transaction
// this work was handed. The context keeps everything but the transaction — the
// tenant, the caller, the trace — and the deadline of the attempt that has ended is
// taken off too, because the step it was bounding is already over.
func (s *Service) detached(ctx context.Context, fn func(context.Context, db.Tx[db.Tenant]) error) error {
	conn, ok := db.ConnFrom(ctx)
	if !ok {
		return errNoConnection
	}
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), detachedWriteBudget)
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
