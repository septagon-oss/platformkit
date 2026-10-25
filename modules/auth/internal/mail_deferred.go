package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
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
	if err := s.mail.Mailer.Send(ctx, m.msg); err != nil {
		return s.undelivered(ctx, m, err)
	}
	return s.delivered(ctx, m)
}

// delivered leaves the two marks one accepted send is owed — the notice beside the
// link, and the record — in one transaction of its own.
//
// Together, because they describe one fact that is only true once the message left.
// The notice is raised here rather than beside the mint for the same reason: its copy
// tells the person to check their mail, and a sentence promising a mail the transport
// refused is the lie the record exists to make impossible. What the record claims about
// the world is the carrier's answer, not this module's guess: see deliveredOutcome.
func (s *Service) delivered(ctx context.Context, m linkMail) error {
	err := s.detached(ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if s.notify != nil && m.notice.Recipient != uuid.Nil {
			if _, err := s.notify.Notify(ctx, tx, m.notice); err != nil {
				return err
			}
		}
		outcome, reason := s.deliveredOutcome()
		return s.recordMail(ctx, tx, m.kind, m.address, outcome, reason)
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
// five minutes ago because a mail server refused them a second one.
//
// Every statement carries this attempt's hash, so it writes the row this attempt wrote
// and nothing else: a link minted and mailed while the send was in the air is left
// where the person who received it can reach it. Withdrawing the row is also what
// frees the next ask — the resend cap counts minted links, and the row it would have
// counted is the one nobody was sent. A process that died between the commit and this
// write leaves one live row, which caps for one interval and expires on its own clock.
func restoreUnmailed(_ context.Context, tx db.Tx[db.Tenant], m linkMail) error {
	tenant, where := db.TenantOf(tx).ID, " WHERE tenant_id = ? AND user_id = ? AND token_hash = ?"
	var res *gorm.DB
	if !m.was.there {
		res = tx.DB().Exec("DELETE FROM "+m.table+where, tenant, m.person, m.digest)
	} else {
		res = tx.DB().Exec("UPDATE "+m.table+" SET token_hash = ?, created_at = ?, expires_at = ?"+where,
			m.was.hash, m.was.created, m.was.expires, tenant, m.person, m.digest)
	}
	if res.Error != nil {
		return fmt.Errorf("auth: withdraw the %s link nobody was sent: %w", m.kind, res.Error)
	}
	return nil
}
