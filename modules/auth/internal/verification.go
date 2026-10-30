package internal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func VerificationSubscriptions(svc *Service) []events.Subscription {
	subs := []events.Subscription{{
		Module: "auth", Name: user.EventRegistrationUnverified,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], event events.Event) error {
			var registered user.RegistrationUnverified
			if err := json.Unmarshal(event.Payload, &registered); err != nil {
				return fmt.Errorf("auth: read unverified registration: %w", err)
			}
			// The confirmation link is built here, in the worker, and the sign-up request
			// that asked for it is gone: the port it was answered at rides the event.
			ctx = WithServed(ctx, registered.Served)
			ctx = WithOrigin(ctx, event)
			return svc.offerVerification(ctx, tx, registered.UserID, registered.Email, askedAtSignUp)
		},
	}, {
		Module: "auth", Name: contracts.EventNoLinkRequested,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], event events.Event) error {
			var asked contracts.NoLinkRequested
			if err := json.Unmarshal(event.Payload, &asked); err != nil {
				return fmt.Errorf("auth: read no-link request: %w", err)
			}
			ctx = WithServed(ctx, asked.Served)
			ctx = WithOrigin(ctx, event)
			// Nobody is looked up here, because the sign-up route already decided what
			// this address is: an account was already here, and the one message this
			// call is answered with is the sentence that says no link was sent.
			return svc.noVerificationLink(ctx, tx, asked.Email)
		},
	}, {
		Module: "auth", Name: contracts.EventVerificationRequested,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], event events.Event) error {
			var asked contracts.VerificationRequested
			if err := json.Unmarshal(event.Payload, &asked); err != nil {
				return fmt.Errorf("auth: read verification request: %w", err)
			}
			ctx = WithServed(ctx, asked.Served)
			ctx = WithOrigin(ctx, event)
			found, err := svc.users.ByEmail(ctx, tx, asked.Email)
			if errors.Is(err, crud.ErrNotFound) {
				// Nobody has this address. The route said the same thing it says about
				// an address that does, and this is the record that lets the delivery
				// door answer the two callers identically: the message the no-account
				// branch hands the transport is what its own answer is read from (noLink).
				return svc.noVerificationLink(ctx, tx, asked.Email)
			}
			if err != nil {
				return err
			}
			return svc.offerVerification(ctx, tx, found.ID, asked.Email, askedForAnotherLink)
		},
	}}
	for i := range subs {
		subs[i].Handler = AcknowledgeRefusedDelivery(subs[i].Handler)
	}
	return subs
}

// Both rotation and consumption take this lock before touching a token row.
// The key includes the tenant even though table visibility is already RLS-bound.
func verificationLock(tx db.Tx[db.Tenant], id uuid.UUID) error {
	key := "auth/verification/" + db.TenantOf(tx).ID.String() + "/" + id.String()
	if err := tx.DB().Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
		return fmt.Errorf("auth: lock email verification: %w", err)
	}
	return nil
}

// asker names which of this module's two ways into offerVerification is asking,
// which is what decides what the offer owes the person on the branch where no
// link leaves.
//
// The public delivery door (RegisterEmailRegistrationRoutes' auth-mail-delivery)
// answers a caller from the one record its own call caused, so every branch of an
// ask must hand one message to the transport (no_link.go). Whether this delivery
// is such an ask is not a fact the credential table can answer — a first delivery
// and a redelivery of one event look identical in it — so the caller says.
type asker int

const (
	// askedForAnotherLink is somebody at the resend form. Their ask is the one the
	// door answers, and it is answered from the record this delivery causes.
	askedForAnotherLink asker = iota
	// askedAtSignUp is one account creation, which the outbox may deliver more than
	// once under one id. The sign-up route answered its caller 202 Accepted, and the
	// message that call is answered from is caused by the first delivery of it.
	askedAtSignUp
)

func (s *Service) offerVerification(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, email string, asked asker) error {
	if !contracts.MailDeliverable(s.mail.Mailer) || s.mail.Hosts == nil {
		return fmt.Errorf("auth: verification delivery is unavailable")
	}
	if err := verificationLock(tx, id); err != nil {
		return err
	}
	// Re-read after waiting: a queued resend must not resurrect another state
	// or send a bearer to the address from a stale account snapshot.
	current, err := s.users.Get(ctx, tx, id)
	if errors.Is(err, crud.ErrNotFound) {
		// The account went away between the request and this send. The person who
		// asked is still owed the one message their call causes on every branch of
		// this flow, and this is the branch with least to say about it.
		return s.noVerificationLink(ctx, tx, email)
	}
	if err != nil {
		return err
	}
	if current.Status != user.StatusUnverified || current.PasswordHash == "" || current.Email != contracts.EmailKey(email) {
		// An account that cannot be sent a confirmation link — already verified, no
		// password to confirm it against, or an address that has since moved — is
		// answered with the same message an address nobody has gets, and it is
		// answered to the address that asked rather than to the one on the account.
		// The door behind this line reads its answer out of this row: whichever
		// branch a call takes, one message of its own is what it is answered from
		// (no_link.go), and a branch that sent nothing would leave that caller to be
		// answered out of somebody else's record.
		return s.noVerificationLink(ctx, tx, email)
	}
	if s.mail.Mailer == nil {
		// Nothing to send it through, which is the deployment's fault and the
		// operator's to see: a suppressed record, and the person is answered
		// exactly as they are when the mail does go, because a record is not a
		// promise about what their mailbox will show. It is written behind the lock
		// and the re-read rather than in front of them, so that the row describes a
		// mail this account would really have been sent — an event about a person who
		// has since verified, deleted or changed address had nothing suppressed, it
		// had nothing to suppress.
		return s.recordMail(ctx, tx, contracts.MailVerification, current.Email,
			notification.MailSuppressed, "no mail transport is wired")
	}
	// The cooldown counts links that went out, not rows that were written, and this
	// offer replaces a credential whose mail never reached the transport.
	recent, err := s.verificationSentRecently(tx, id, current.Email)
	if err != nil {
		return err
	}
	if recent {
		// A link this person was sent a moment ago still stands, so no second one leaves.
		return s.answerNoLink(ctx, tx, asked, email)
	}
	// A link of this person's that is with the transport right now is the link this
	// person is being sent, here exactly as at the reset offer. The send leaves on the
	// commit of the transaction that minted it, so while it is in the air the row it
	// describes stands unmarked, the cooldown above counts no send and answers "nobody
	// was mailed", and a mint over that row would leave the person two messages of which
	// the single-pending write keeps one credential: the first envelope, the one already
	// open in front of them, leads to the row the table threw away. The ask is the mail
	// slot's, and it is asked without waiting: the message inside a taken slot is still
	// leaving, and an offer that waited on it would be a delivery parked behind a mail
	// server, holding this transaction's row lock and its connection with it.
	open, err := mailSlotOpen(tx, "verification_tokens", id)
	if err != nil {
		return err
	}
	if !open {
		// Somebody is carrying this person's link at this moment, so this ask sends none.
		return s.answerNoLink(ctx, tx, asked, email)
	}
	base, err := s.baseURL(ctx, tx)
	if err != nil {
		return err
	}
	// The credential the person already holds, read under the lock, so that a
	// refused send can put it back rather than delete "the" row.
	was, err := pendingLinkOf(tx, "verification_tokens", id)
	if err != nil {
		return err
	}
	token, at := secret(), db.Now()
	out := linkMail{
		table: "verification_tokens", kind: contracts.MailVerification,
		person: id, address: current.Email, token: token, digest: contracts.Hash(token), was: was,
		msg: notification.Message{
			To: current.Email, Subject: "Verify your email address",
			Body: "Confirm your email address to finish creating your account. Your password will stay the same.\n\n" +
				base + s.mail.ConfirmEmailPage + "?token=" + token +
				"\n\nThe link works once and expires in 24 hours. If you did not request this account, ignore this message.",
		},
	}
	if err := s.writeVerification(tx, id, current.Email, token, at); err != nil {
		return err
	}
	if err := writeMailDispatch(tx, out); err != nil {
		return err
	}
	return s.sendConfirmationLink(ctx, out)
}

// answerNoLink is what a confirmation offer that sends no link answers with, whether a
// link this person was just sent still stands or one of theirs is with the transport now.
//
// The public delivery door behind an ask reads its answer out of the record this call
// caused, so a person at the resend form still hands one message to the transport — the
// sentence that says no link was sent, which is true even while an earlier one is away.
//
// A delivery of the sign-up event is nobody asking again: it is the outbox's retry of one
// account creation, and that event is raised once per account, every later ask arriving
// as an explicit resend. The link already leaving, or already in the mailbox, is the
// answer its call caused; a sentence saying none was sent would contradict it and record
// a request the door has read. The retry owes nothing.
func (s *Service) answerNoLink(ctx context.Context, tx db.Tx[db.Tenant], asked asker, email string) error {
	if asked == askedAtSignUp {
		return nil
	}
	return s.noVerificationLink(ctx, tx, email)
}

// sendConfirmationLink hands this person's confirmation message to the transport on the
// commit that makes its row spendable — never after a rollback — and records what the
// transport answered, which is what a refused send costs (see mailAfterCommit). The
// hand-off carries the secret and the digest that names this attempt's row, so nothing
// about the send is read back out of the table after the commit.
func (s *Service) sendConfirmationLink(ctx context.Context, m linkMail) error {
	return events.Defer(ctx, func(ctx context.Context) error {
		return s.mailAfterCommit(ctx, m)
	})
}

// verificationSentRecently reports whether this person was sent a confirmation link
// inside the resend interval. It counts sent_at — the links a transport took — rather
// than the rows that were written, so an offer nobody was ever mailed holds nobody's
// cooldown down.
func (s *Service) verificationSentRecently(tx db.Tx[db.Tenant], id uuid.UUID, email string) (bool, error) {
	var recent bool
	err := tx.DB().Raw("SELECT EXISTS (SELECT 1 FROM verification_tokens WHERE user_id = ? AND email = ?"+
		" AND sent_at IS NOT NULL AND sent_at > clock_timestamp() - ?::interval)",
		id, email, verificationInterval).Row().Scan(&recent)
	return recent, err
}

// writeVerification writes this attempt's one pending confirmation row, unsent until
// the transport takes its message. ON CONFLICT on (tenant_id, user_id) is the
// one-link-per-person rule: asking again replaces the last link rather than adding a
// second, so the cooldown is the only thing left bounding how many mails this causes.
func (s *Service) writeVerification(tx db.Tx[db.Tenant], id uuid.UUID, email, token string, at time.Time) error {
	err := tx.DB().Exec("INSERT INTO verification_tokens (token_hash, tenant_id, user_id, email, created_at, expires_at, sent_at)"+
		" VALUES (?, ?, ?, ?, ?, ?, NULL)"+
		" ON CONFLICT (tenant_id, user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, email = EXCLUDED.email,"+
		" created_at = EXCLUDED.created_at, expires_at = EXCLUDED.expires_at, sent_at = NULL",
		contracts.Hash(token), db.TenantOf(tx).ID, id, email, at, at.Add(contracts.VerificationLifetime)).Error
	if err != nil {
		return fmt.Errorf("auth: issue email verification: %w", err)
	}
	return nil
}

// writeCredential runs one statement about one emailed credential in a transaction
// that commits by itself, which is the only shape that lets the write survive the
// roll back of the delivery that mailed it. Both credentials this module mails go
// through here and through detached, because "on the pool the caller carries, outside
// the transaction now open" is one rule and two spellings of it drift.
func (s *Service) writeCredential(ctx context.Context, verb, kind string, stmt func(db.Tx[db.Tenant]) error) error {
	if err := s.detached(ctx, func(_ context.Context, tx db.Tx[db.Tenant]) error { return stmt(tx) }); err != nil {
		return fmt.Errorf("auth: %s the emailed %s credential: %w", verb, kind, err)
	}
	return nil
}

func (s *Service) verifyEmail(ctx context.Context, tx db.Tx[db.Tenant], users contracts.EmailRegistrar, token string) error {
	var id uuid.UUID
	// This read locates the lock only. Consumption rechecks the credential and
	// wall-clock expiry after waiting for any issuance or earlier redemption.
	err := tx.DB().Raw("SELECT user_id FROM verification_tokens WHERE token_hash = ? AND expires_at > clock_timestamp()",
		contracts.Hash(token)).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return contracts.ErrCredentials
	}
	if err != nil {
		return fmt.Errorf("auth: find email verification: %w", err)
	}
	if err := verificationLock(tx, id); err != nil {
		return err
	}
	var email string
	err = tx.DB().Raw("DELETE FROM verification_tokens WHERE token_hash = ? AND expires_at > clock_timestamp() RETURNING user_id, email",
		contracts.Hash(token)).Row().Scan(&id, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return contracts.ErrCredentials
	}
	if err != nil {
		return fmt.Errorf("auth: consume email verification: %w", err)
	}
	_, err = users.VerifyEmail(ctx, tx, id, email)
	if errors.Is(err, crud.ErrNotFound) || errors.Is(err, crud.ErrConflict) {
		return contracts.ErrCredentials
	}
	return err
}

var verificationInterval = fmt.Sprintf("%d seconds", int(contracts.VerificationResendInterval.Seconds()))
