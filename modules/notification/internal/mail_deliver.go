package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// The delivery worker's own schedule and batch; the lease is a Deps field with this
// behind it, and the ladder, the window and the two caps are contracts'.
const (
	defaultMailBatch    = 20
	defaultMailInterval = 5 * time.Second
	// defaultMailLease bounds how long a claim may hold a row without recording an
	// outcome. It has to outlast one send's timeout, and it is what turns a process
	// that died across the socket into a row that says so within a minute of dying
	// rather than a row that waits for an operator to notice.
	defaultMailLease = 2 * time.Minute

	// mailLock names the advisory transaction lock the outcome takes before it
	// touches the intent row and the delivery ledger row beside it. The critical
	// section spans two tables whose rows no single statement joins, and two
	// outcomes for one notice must not both write a terminal channel row.
	mailLock = "notification-mail-outcome"
)

// mailLadder is how long to wait before attempt 2 and attempt 3, as a table rather
// than a formula so a test can pin "the second send is available one minute after
// the first" instead of an approximation of a curve. It is shorter than webhook's:
// a person's mail is asked for by something that just happened, and a message that
// arrives a day late says nothing about the moment it was about.
var mailLadder = []time.Duration{time.Minute, 10 * time.Minute}

// Deliver is the module's mail clock: the owed rows go out. It is a job and not a
// subscription because the work is triggered by a deadline arriving rather than by
// something having happened (docs/adr/0004), and because the ladder and the
// unknown rule live in rows this job owns, not in the broker's five rungs.
//
// It runs per tenant, so the transaction that claims a row is that tenant's own:
// row level security is what makes "never another tenant's intent" true even of a
// mistyped statement.
//
// Four steps, and which transaction writes which row is what a reviewer should
// check first:
//
//	claim   — one short tenant transaction: due rows become in_flight with FOR UPDATE
//	          SKIP LOCKED and attempt+1, an expired attempt becomes unknown there, and
//	          a row whose window closed becomes failed there. It commits before any
//	          socket opens.
//	re-ask  — one short tenant transaction per row: is the notice still there, is the
//	          address still the one decided for, is the tenant's sender still verified.
//	          A refusal ends the row there, with the sentence, and opens no socket.
//	send    — no transaction at all. No lock is held across a mail server.
//	outcome — a new tenant transaction: this row's state and the notice's terminal
//	          ledger row beside it, one commit, so state and ledger cannot disagree.
//
// What the split buys and what it costs: a crash between the send and the outcome
// loses the record of an attempt that physically happened. Unlike webhook, this
// module does not re-send it on the lease — the expired attempt becomes unknown, and
// contracts.MailIntentMaxUnknown decides whether it is ever handed a socket again.
// SMTP has no idempotency key for a receiver to deduplicate on, so re-sending on a
// guess would mail a person the same message twice with no record of why; saying
// "unknown" and bounding the re-send is the whole of the difference.
func Deliver(svc *Service, tenants jobs.TenantLister, every time.Duration, batch int, lease time.Duration) jobs.Job {
	if every <= 0 {
		every = defaultMailInterval
	}
	if batch <= 0 {
		batch = defaultMailBatch
	}
	if lease <= 0 {
		lease = defaultMailLease
	}
	return jobs.Job{
		Name:  "notification-mail-deliver",
		Every: every,
		Run: func(ctx context.Context, conn *db.Conn) error {
			return jobs.PerTenant(ctx, conn, tenants, func(ctx context.Context, conn *db.Conn, t tenancy.Tenant) error {
				claimed, err := svc.claimMail(ctx, conn, batch, lease)
				if err != nil || len(claimed) == 0 {
					return err
				}
				for _, a := range claimed {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					svc.sendMail(ctx, conn, a)
				}
				return nil
			})
		},
	}
}

// attempt is one row a tick claimed and the message to hand a transport, built by
// the re-ask beside the socket and not by the claim: the tenant's sender and the
// person's address are exactly the two facts a command can change between the claim
// committing and this row's turn, and a queue is where that gap lives.
type attempt struct {
	row contracts.MailIntent
	msg contracts.Message
}

// claimMail takes this tenant's due rows and commits before anything is dialled.
//
// A row whose lease expired is claimed too — that is the crash recovery — but it is
// not re-sent: it becomes unknown in this transaction, with the delay its next
// possible send waits behind. A queued row whose window closed becomes failed here
// and opens no socket either.
func (s *Service) claimMail(ctx context.Context, conn *db.Conn, batch int, lease time.Duration) ([]attempt, error) {
	var out []attempt
	err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		// The clock that judges a row due is the clock that made it due, so the
		// ladder's arithmetic and this drain agree. The rows' own created_at and
		// updated_at stay the database's, as both delivery ledgers' do.
		now := s.clock()
		var rows []contracts.MailIntent
		res := tx.DB().Raw("SELECT "+intentColumns+" FROM "+intentTable+
			" WHERE (state = ? AND available_at <= ?)"+
			" OR (state = ? AND attempted_at < ?)"+
			" OR (state = ? AND available_at <= ? AND uncertain <= ?)"+
			" ORDER BY available_at, seq LIMIT ? FOR UPDATE SKIP LOCKED",
			contracts.MailIntentQueued, now,
			contracts.MailIntentInFlight, now.Add(-lease),
			contracts.MailIntentUnknown, now, contracts.MailIntentMaxUnknown, batch).Find(&rows)
		if res.Error != nil {
			return fmt.Errorf("notification: claim the owed mail: %w", res.Error)
		}
		for _, row := range rows {
			next, err := s.armMail(tx, row, now)
			if err != nil || next == nil {
				if err != nil {
					return err
				}
				continue
			}
			out = append(out, attempt{row: *next})
		}
		return nil
	})
	return out, err
}

// armMail ends or re-arms one claimed row in the claim's own transaction, and hands
// back the row as it now is when this tick should go on to send it.
func (s *Service) armMail(tx db.Tx[db.Tenant], row contracts.MailIntent, now time.Time) (*contracts.MailIntent, error) {
	switch {
	case row.State == contracts.MailIntentInFlight:
		// The attempt that physically happened and never got an outcome. It is not
		// re-sent blind: nothing this process can say about the socket is a fact
		// about the relay, so the row says it does not know, waits out the delay, and
		// is worth at most one more socket.
		if err := s.markUnknown(tx, row, now, "the previous attempt never answered: the worker stopped between the mail server and this row"); err != nil {
			return nil, err
		}
		return nil, nil
	case row.State == contracts.MailIntentQueued && !now.Before(row.CreatedAt.Add(contracts.MailIntentWindow)):
		// A notice mailed two days late is not a notice: the person still has the
		// in-app row, and a queue that keeps mail forever is a queue nobody reads.
		if err := s.endMail(tx, row.ID, contracts.MailIntentFailed, now,
			"the mail was not sent within "+fmt.Sprint(contracts.MailIntentWindow)+": it waited past the moment it was about"); err != nil {
			return nil, err
		}
		return nil, nil
	}
	// The row this transaction already locked is the row being armed: nothing else
	// can have touched it while this claim holds it, so the copy in hand is the one
	// that leaves, with the claim's two marks on it.
	res := tx.DB().Exec("UPDATE "+intentTable+
		" SET state = ?, attempted_at = ?, attempt = attempt + 1, updated_at = ? WHERE id = ?",
		contracts.MailIntentInFlight, now, now, row.ID)
	if res.Error != nil {
		return nil, fmt.Errorf("notification: mark the mail intent %s in flight: %w", row.ID, res.Error)
	}
	next := row
	next.State = contracts.MailIntentInFlight
	next.AttemptedAt = &now
	next.Attempt++
	return &next, nil
}

// sendMail asks the row once more whether it is still wanted, opens the socket with
// no transaction held, and writes the answer in a transaction of its own. It never
// returns an error to the job: one tenant's dead relay must not stop the rows behind
// this one, and every path ends in an outcome — the failure to write one leaves the
// row in_flight for the lease to turn into unknown, which is the row saying what
// happened rather than a mail that vanished.
func (s *Service) sendMail(ctx context.Context, conn *db.Conn, a attempt) {
	a, wanted, err := s.recheckMail(ctx, conn, a)
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "notification: could not prepare an owed mail",
				"intent", a.row.ID, "error", err)
		}
		return
	}
	if !wanted {
		return
	}
	err = nil
	if s.mailer != nil {
		err = s.mailer.Send(ctx, a.msg)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return s.recordMail(ctx, tx, a.row, err)
	}); err != nil && ctx.Err() == nil {
		// Logged where it happened rather than returned: the row stays in_flight and
		// the lease turns it into unknown, so the attempt is not lost, and the other
		// rows of this tenant are still this tick's work.
		slog.ErrorContext(ctx, "notification: could not record the outcome of an owed mail",
			"intent", a.row.ID, "error", err)
	}
}

// recheckMail is the ask beside the socket, and it ends the row in the same short
// transaction that read the answers, so an attempt that never opened a socket still
// carries its terminal state and the reason. It reads without locking: this is the
// sender declining to act on a fact already committed, not a command deciding one.
func (s *Service) recheckMail(ctx context.Context, conn *db.Conn, a attempt) (attempt, bool, error) {
	wanted := true
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		suppress := func(why string) error {
			wanted = false
			return s.endMail(tx, a.row.ID, contracts.MailIntentSuppressed, s.clock(), why)
		}
		if a.row.Notice != uuid.Nil {
			if _, err := crud.Get[*contracts.Notification](tx, a.row.Notice); errors.Is(err, crud.ErrNotFound) {
				return suppress("the notice was deleted before its mail was sent")
			} else if err != nil {
				return err
			}
		}
		to, err := address(ctx, tx, s.recipients, a.row.Person)
		if err != nil {
			return err
		}
		if to != a.row.Recipient {
			// The address the row names is the one that was decided for. A person who
			// moved their address since then is not reached at the old one, and an
			// erased person resolves to nobody: either way the mail stops here rather
			// than at a relay that would have taken it.
			return suppress("the recipient's address is no longer " + a.row.Recipient)
		}
		if s.mailer == nil || contracts.IsNoTransport(s.mailer) {
			return suppress("this installation has no mail server: set mail.host")
		}
		msg := contracts.Message{To: a.row.Recipient, Subject: a.row.Subject, Body: a.row.Body,
			HTML: a.row.HTML, Lang: a.row.Lang, ReplyTo: a.row.ReplyTo}
		if s.senders != nil {
			sender, err := s.senders.For(ctx, tx)
			if err != nil {
				return err
			}
			if sender == nil {
				return suppress("this tenant has no sender: mail is refused until its administrator sets one")
			}
			// Re-read here, not carried in from the moment the mail was decided: between
			// that commit and this one the administrator can save a different domain,
			// which takes the row back to pending, and a queue is exactly where that gap
			// lives. Nothing sends as an identity that stopped being believed.
			if sender.Status != contracts.SenderVerified {
				return suppress("this tenant's sender is " + sender.Status + ", not verified: nothing sends as an identity that stopped being believed")
			}
			msg.Sender, msg.ReplyTo = sender, sender.ReplyTo
		}
		a.msg = msg
		return nil
	})
	return a, wanted, err
}

// recordMail writes the answer. One statement carries the state, guarded on
// `state = 'in_flight'`, so a worker whose send came back after its row had already
// been ended by someone else cannot overwrite the newer answer.
func (s *Service) recordMail(ctx context.Context, tx db.Tx[db.Tenant], row contracts.MailIntent, sendErr error) error {
	now := s.clock()
	if err := tx.DB().Exec("SELECT pg_advisory_xact_lock(hashtext(?)::bigint)", mailLock).Error; err != nil {
		return fmt.Errorf("notification: take the mail outcome lock: %w", err)
	}
	state, said := classifySend(sendErr)
	if state == contracts.MailIntentFailed {
		// A refusal the relay actually gave is retryable while the ladder has a rung
		// and the window is open; a terminal one says so in this row.
		if available, retry := nextMailAttempt(row, now); retry {
			return s.endMail(tx, row.ID, contracts.MailIntentQueued, now, said, available)
		}
	}
	if state == contracts.MailIntentUnknown {
		if err := s.markUnknown(tx, row, now, said); err != nil {
			return err
		}
		// An unresolved send writes no ledger row: a ledger row says at most what
		// this process can prove, and this one cannot prove the mail left.
		return nil
	}
	if err := s.endMail(tx, row.ID, state, now, said); err != nil {
		return err
	}
	if row.Notice == uuid.Nil {
		return nil
	}
	outcome := OutcomeSent
	if state != contracts.MailIntentSent {
		outcome = OutcomeFailed
		if state == contracts.MailIntentSuppressed {
			outcome = OutcomeSuppressed
		}
	}
	return record(tx, row.Notice, ChannelEmail, outcome, said)
}

// classifySend turns the transport's answer into a state, and the burden of proof
// runs one way: only what a relay said is a fact about the relay.
func classifySend(err error) (state, said string) {
	switch {
	case err == nil:
		return contracts.MailIntentSent, ""
	case errors.Is(err, contracts.ErrUncertainSend):
		return contracts.MailIntentUnknown, reason(err)
	case errors.Is(err, contracts.ErrPermanent), errors.Is(err, contracts.ErrNotAccepted):
		return contracts.MailIntentFailed, reason(err)
	default:
		// Nothing is known about an error the carrier did not classify, so the
		// intent says what is true rather than what would be convenient: a send
		// whose answer arrived as an unclassifiable failure may have happened.
		return contracts.MailIntentUnknown, reason(err)
	}
}

// nextMailAttempt answers the two questions the outcome asks: does a refused send
// get another, and when. The count bounds it, and so does the window — a row that
// could only be sent after the moment the mail meant something has already missed
// it, and would sit in the queue looking retryable.
func nextMailAttempt(row contracts.MailIntent, now time.Time) (time.Time, bool) {
	if row.Attempt >= contracts.MailIntentMaxAttempts {
		return time.Time{}, false
	}
	delay := mailLadder[len(mailLadder)-1]
	if row.Attempt-1 >= 0 && row.Attempt-1 < len(mailLadder) {
		delay = mailLadder[row.Attempt-1]
	}
	candidate := now.Add(delay)
	if !candidate.Before(row.CreatedAt.Add(contracts.MailIntentWindow)) {
		return time.Time{}, false
	}
	return candidate, true
}

// markUnknown is the crash answer: the attempt happened, its answer never came, and
// the row records that instead of either of the two stories it could tell.
func (s *Service) markUnknown(tx db.Tx[db.Tenant], row contracts.MailIntent, now time.Time, why string) error {
	res := tx.DB().Exec("UPDATE "+intentTable+
		" SET state = ?, uncertain = uncertain + 1, available_at = ?, reason = ?, updated_at = ?"+
		" WHERE id = ? AND state = ?",
		contracts.MailIntentUnknown, now.Add(contracts.MailIntentUnknownDelay), clipReason(why), now,
		row.ID, contracts.MailIntentInFlight)
	if res.Error != nil {
		return fmt.Errorf("notification: mark the mail intent %s unknown: %w", row.ID, res.Error)
	}
	return nil
}

// endMail moves one intent to a state, with the sentence. It is one statement with
// its guard, so a stale worker cannot write over a newer answer, and the reason is
// never stored without the state it explains. The optional availableAt is the
// re-queue's next rung and nothing else.
func (s *Service) endMail(tx db.Tx[db.Tenant], id uuid.UUID, state string, now time.Time, why string, availableAt ...time.Time) error {
	res := tx.DB().Exec("UPDATE "+intentTable+
		" SET state = ?, reason = ?, updated_at = ?, available_at = COALESCE(?, available_at), attempted_at = ?"+
		" WHERE id = ? AND state IN (?, ?, ?)",
		state, clipReason(why), now, firstOrNil(availableAt), now, id,
		contracts.MailIntentQueued, contracts.MailIntentInFlight, contracts.MailIntentUnknown)
	if res.Error != nil {
		return fmt.Errorf("notification: record the mail intent %s as %s: %w", id, state, res.Error)
	}
	return nil
}

// clipReason is the reason column's own bound over words that did not come from an
// error: the sentence is for an operator, and a 2 kB SMTP greeting is a fact about a
// server rather than about a delivery. Whitespace is folded so a relay's multi-line
// answer reads as one sentence.
func clipReason(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= maxReason {
		return s
	}
	cut := maxReason
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// firstOrNil is the re-queue's rung as a nullable argument: absent means leave the
// row's own available_at alone.
func firstOrNil(in []time.Time) any {
	if len(in) == 0 {
		return nil
	}
	return in[0]
}
