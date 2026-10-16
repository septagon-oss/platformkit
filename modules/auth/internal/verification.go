package internal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func VerificationSubscriptions(svc *Service) []events.Subscription {
	return []events.Subscription{{
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
			return svc.offerVerification(ctx, tx, registered.UserID, registered.Email)
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
			return svc.offerVerification(ctx, tx, found.ID, asked.Email)
		},
	}}
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

func (s *Service) offerVerification(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, email string) error {
	if s.mail.Hosts == nil {
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
	var recent bool
	err = tx.DB().Raw("SELECT EXISTS (SELECT 1 FROM verification_tokens WHERE user_id = ? AND email = ? AND created_at > clock_timestamp() - ?::interval)",
		id, current.Email, verificationInterval).Row().Scan(&recent)
	if err != nil {
		return err
	}
	if recent {
		// A link this person was sent a moment ago still stands, so no second one
		// leaves — and this call still hands one message to the transport, for the
		// reason the branch above gives. The sentence it carries is the one the flow
		// says on every branch: about this request, no link was sent, which is true
		// even while an earlier one is on its way.
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
	err = tx.DB().Exec("INSERT INTO verification_tokens (token_hash, tenant_id, user_id, email, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)"+
		" ON CONFLICT (tenant_id, user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, email = EXCLUDED.email,"+
		" created_at = EXCLUDED.created_at, expires_at = EXCLUDED.expires_at",
		contracts.Hash(token), db.TenantOf(tx).ID, id, current.Email, at, at.Add(contracts.VerificationLifetime)).Error
	if err != nil {
		return fmt.Errorf("auth: issue email verification: %w", err)
	}
	err = s.mail.Mailer.Send(ctx, notification.Message{
		To: current.Email, Subject: "Verify your email address",
		Body: "Confirm your email address to finish creating your account. Your password will stay the same.\n\n" +
			base + VerifyEmailPath + "?token=" + token +
			"\n\nThe link works once and expires in 24 hours. If you did not request this account, ignore this message.",
	})
	if err != nil {
		// The credential goes back exactly as this attempt found it. Two wrongs are
		// refused: leaving a live token nobody was sent, and destroying the link that
		// person already has because a second one was refused. The resend cap then
		// counts the link that really left, so asking again is answered rather than
		// held for an interval nobody was mailed under.
		//
		// The attempt is acknowledged rather than returned as an error, because the
		// record of it is written in this transaction and has to commit for it to
		// exist. The transport's own words still reach the operator — redacted by
		// contracts.RedactMailReason, into the row — and nothing credential-bearing
		// reaches the outbox either way, which is all the sanitised error this line
		// replaced ever protected.
		if rerr := restoreLink(tx, "verification_tokens", id, was); rerr != nil {
			return rerr
		}
		return s.recordMail(ctx, tx, contracts.MailVerification, current.Email,
			notification.MailFailed, err.Error(), token, string(contracts.Hash(token)))
	}
	return s.recordMail(ctx, tx, contracts.MailVerification, current.Email, notification.MailSent, "")

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
