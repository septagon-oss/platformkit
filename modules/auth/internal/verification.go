package internal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
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
	// The cooldown counts links that went out, not rows that were written. A
	// credential whose mail never reached the transport is not a recent delivery:
	// it holds nobody's cooldown down, and this offer replaces it.
	var recent bool
	err = tx.DB().Raw("SELECT EXISTS (SELECT 1 FROM verification_tokens WHERE user_id = ? AND email = ?"+
		" AND sent_at IS NOT NULL AND sent_at > clock_timestamp() - ?::interval)",
		id, current.Email, verificationInterval).Row().Scan(&recent)
	if err != nil {
		return err
	}
	if recent {
		// A link this person was sent a moment ago still stands, so no second one
		// leaves — and a person who asked still hands one message to the transport,
		// for the reason the branch above gives: the sentence is the one the flow
		// says on every branch, about this request no link was sent, which is true
		// even while an earlier one is on its way.
		//
		// A delivery of the sign-up event is nobody asking again. It is the outbox's
		// retry of one account creation, and user.registration_unverified is raised
		// once per account: every later ask arrives as an explicit resend. So a
		// delivery of it that finds its own link already sent can only be that
		// delivery arriving twice. The row which set this branch off is the record
		// that the transport took the link for this creation: the mail went, and a
		// second message would tell the person holding it that no link was sent, and
		// record a request the door has already read. The retry owes nothing.
		if asked == askedAtSignUp {
			return nil
		}
		return s.noVerificationLink(ctx, tx, email)
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
	// The credential is committed in a transaction that ends here, before the
	// link that carries it is handed over. In the delivery's own transaction the
	// row would stay invisible until that transaction committed — which is after
	// the send — and a recipient who opens a link that arrives in seconds reads
	// "invalid or has expired" from a row that exists but is not yet visible.
	// The delivery keeps its advisory lock across the whole of this, so no other
	// offer or redemption of this user's is between the commit and the send.
	ahead, err := s.postVerification(ctx, tx, id, current.Email, token, at)
	if err != nil {
		return err
	}
	if err := s.mail.Mailer.Send(ctx, notification.Message{
		To: current.Email, Subject: "Verify your email address",
		Body: "Confirm your email address to finish creating your account. Your password will stay the same.\n\n" +
			base + s.mail.ConfirmEmailPage + "?token=" + token +
			"\n\nThe link works once and expires in 24 hours. If you did not request this account, ignore this message.",
	}); err != nil {
		// The credential goes back exactly as this attempt found it. The row went in
		// ahead of the send, so that restore is a write of its own rather than
		// something the caller's rollback could do. Two wrongs are refused here:
		// leaving a live token nobody was sent, and destroying the link this person
		// already holds because a second one was refused. The resend cap counts the
		// link that really left, so asking again is answered rather than held for an
		// interval nobody was mailed under.
		//
		// The refusal is recorded in this transaction and answered with
		// ErrDeliveryRefused: the worker that has to commit that record acknowledges
		// it, and a caller that asked for the mail directly learns it did not go. The
		// transport's own words reach the operator, redacted into the row, and nothing
		// credential-bearing travels either way.
		if ahead {
			// The row went in on the pool, so the caller's rollback cannot reach
			// it: it goes back in a write of its own.
			s.restoreVerification(ctx, id, was)
		} else if rerr := restoreLink(tx, "verification_tokens", id, was); rerr != nil {
			return rerr
		}
		return s.refuseVerification(ctx, tx, current.Email, err, token)
	}
	// The transport took the message, so the row stops being an offer and becomes
	// a delivery. It changes in this transaction rather than in a write of its own
	// because the two facts must commit together: the event is acknowledged at the
	// same instant the cooldown starts to honour this link. Recorded in a write of
	// its own, the process could die between the send and the record and be refused
	// a retry it owed, or die before the record and mail a second link it did not
	// owe; here it can only die before both, and the retry then sends the one link.
	if err := tx.DB().Exec("UPDATE verification_tokens SET sent_at = clock_timestamp()"+
		" WHERE user_id = ? AND token_hash = ?", id, contracts.Hash(token)).Error; err != nil {
		return fmt.Errorf("auth: record the emailed verification credential")
	}
	return s.recordMail(ctx, tx, contracts.MailVerification, current.Email,
		notification.MailSent, "")
}

// credentialWriteBudget bounds the two writes a credential offer makes outside
// the delivery's transaction. The delivery holds one pool connection while they
// run, so a pool with nothing free is a wait this module must not turn into a
// hang: the write fails inside the budget, the delivery fails, the ladder retries
// it later, and the pool has drained by then.
const credentialWriteBudget = 2 * time.Second

// postVerification writes one verification credential where the person reading the
// mail will look for it, and says whether it got a commit of its own.
//
// That commit is the rule wherever the caller carries the application pool: a link
// that arrives in seconds must name a row a reader can already see, and a row held
// inside the delivery's own transaction is invisible until after the send. A caller
// with no pool on its context — a relay that runs this module's subscriptions over a
// transaction of its own — gets the other shape, which is the only one it can have:
// the row goes in beside the send and the two stand or fall together, which is what
// sent_at beside the insert already says.
func (s *Service) postVerification(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID,
	email, token string, at time.Time) (bool, error) {
	write := func(w db.Tx[db.Tenant]) error {
		return w.DB().Exec("INSERT INTO verification_tokens (token_hash, tenant_id, user_id, email, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)"+
			" ON CONFLICT (tenant_id, user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, email = EXCLUDED.email,"+
			" created_at = EXCLUDED.created_at, expires_at = EXCLUDED.expires_at, sent_at = NULL",
			contracts.Hash(token), db.TenantOf(w).ID, id, email, at, at.Add(contracts.VerificationLifetime)).Error
	}
	if _, pooled := httpx.ConnFrom(ctx); !pooled {
		return false, write(tx)
	}
	return true, s.writeCredential(ctx, "commit", "verification", write)
}

// restoreVerification puts a credential back the way this attempt found it, in a
// write of its own that commits while the failed delivery rolls back.
//
// Its own, because that rollback is the point: the row was committed ahead of the
// send, so the caller's rollback reaches neither it nor the row it replaced, and a
// row the transport never took would sit in the table until its twenty-four hours
// ran out as a credential nobody holds. Where a delivered link sits behind it, that
// link comes back; where none does, "a row exists" keeps meaning "a recipient has a
// link". A restore that itself fails is logged and does not replace the delivery's
// error: the retry then rotates the row it finds and mails a fresh credential, so
// no row outlives its twenty-four hours and no link goes out twice.
func (s *Service) restoreVerification(ctx context.Context, id uuid.UUID, was pendingLink) {
	err := s.writeCredential(ctx, "restore", "verification", func(tx db.Tx[db.Tenant]) error {
		return restoreLink(tx, "verification_tokens", id, was)
	})
	if err != nil {
		slog.ErrorContext(ctx, "auth: could not put back a verification credential its email could not reach",
			"user", id, "error", err)
	}
}

// refuseVerification records the refused send in the caller's transaction and
// answers with ErrDeliveryRefused, which the delivery acknowledges and a direct
// caller does not. The transport's words go to the record, redacted of the token;
// they never travel in the error.
func (s *Service) refuseVerification(ctx context.Context, tx db.Tx[db.Tenant],
	email string, cause error, token string) error {
	if err := s.recordMail(ctx, tx, contracts.MailVerification, email,
		notification.MailFailed, cause.Error(), token, string(contracts.Hash(token))); err != nil {
		return err
	}
	return ErrDeliveryRefused
}

// writeCredential runs one statement about one emailed credential in a transaction that
// commits by itself, which is the only shape that puts a row in front of a reader before
// the message that names it is sent. Both credentials this module mails are written
// through here, because "ahead of the message, on the pool the caller carries" is one
// rule and two spellings of it drift.
//
// The pool is the one on the context — the same door every detached write here asks,
// see forget — because the wiring path composes routes before the deployment opens its
// database, so a connection a module captured there would be no connection at all. A
// request carries it on the middleware's context and a delivery on the one kit/app
// hands the subscription; a caller with neither writes no credential.
//
// db.Detached because db.Run would join the caller's open transaction rather than end
// before the send; WithoutCancel so a delivery whose deadline passed still finishes.
func (s *Service) writeCredential(ctx context.Context, verb, kind string, stmt func(db.Tx[db.Tenant]) error) error {
	conn, ok := httpx.ConnFrom(ctx)
	if !ok {
		return fmt.Errorf("auth: %s the emailed %s credential: this caller carries no application pool", verb, kind)
	}
	run, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), credentialWriteBudget)
	defer cancel()
	if err := db.Run(run, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error { return stmt(tx) }); err != nil {
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
