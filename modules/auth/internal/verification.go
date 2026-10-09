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
			return svc.offerVerification(ctx, tx, registered.UserID, registered.Email)
		},
	}, {
		Module: "auth", Name: contracts.EventVerificationRequested,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], event events.Event) error {
			var asked contracts.VerificationRequested
			if err := json.Unmarshal(event.Payload, &asked); err != nil {
				return fmt.Errorf("auth: read verification request: %w", err)
			}
			ctx = WithServed(ctx, asked.Served)
			found, err := svc.users.ByEmail(ctx, tx, asked.Email)
			if errors.Is(err, crud.ErrNotFound) {
				return nil
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
	if s.mail.Mailer == nil || s.mail.Hosts == nil {
		return fmt.Errorf("auth: verification delivery is unavailable")
	}
	if err := verificationLock(tx, id); err != nil {
		return err
	}
	// Re-read after waiting: a queued resend must not resurrect another state
	// or send a bearer to the address from a stale account snapshot.
	current, err := s.users.Get(ctx, tx, id)
	if errors.Is(err, crud.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.Status != user.StatusUnverified || current.PasswordHash == "" || current.Email != contracts.EmailKey(email) {
		return nil
	}
	// The cooldown counts links that went out, not rows that were written. A
	// credential whose mail never reached the transport is not a recent delivery:
	// it holds nobody's cooldown down, and this offer replaces it.
	var recent bool
	err = tx.DB().Raw("SELECT EXISTS (SELECT 1 FROM verification_tokens WHERE user_id = ? AND email = ?"+
		" AND sent_at IS NOT NULL AND sent_at > clock_timestamp() - ?::interval)",
		id, current.Email, verificationInterval).Row().Scan(&recent)
	if err != nil || recent {
		return err
	}
	base, err := s.baseURL(ctx, tx)
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
	if err := s.postVerification(ctx, id, current.Email, token, at); err != nil {
		return err
	}
	if err := s.mail.Mailer.Send(ctx, notification.Message{
		To: current.Email, Subject: "Verify your email address",
		Body: "Confirm your email address to finish creating your account. Your password will stay the same.\n\n" +
			base + VerifyEmailPath + "?token=" + token +
			"\n\nThe link works once and expires in 24 hours. If you did not request this account, ignore this message.",
	}); err != nil {
		// A transport may quote its input in an error. The outbox retains handler
		// errors, so never carry the mailer's potentially credential-bearing text.
		s.retractVerification(ctx, id, token)
		return fmt.Errorf("auth: verification email delivery failed")
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
	return nil
}

// credentialWriteBudget bounds the two writes a credential offer makes outside
// the delivery's transaction. The delivery holds one pool connection while they
// run, so a pool with nothing free is a wait this module must not turn into a
// hang: the write fails inside the budget, the delivery fails, the ladder retries
// it later, and the pool has drained by then.
const credentialWriteBudget = 2 * time.Second

// postVerification commits one verification credential on its own.
func (s *Service) postVerification(ctx context.Context, id uuid.UUID, email, token string, at time.Time) error {
	return s.writeVerification(ctx, "commit", func(tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("INSERT INTO verification_tokens (token_hash, tenant_id, user_id, email, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)"+
			" ON CONFLICT (tenant_id, user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, email = EXCLUDED.email,"+
			" created_at = EXCLUDED.created_at, expires_at = EXCLUDED.expires_at, sent_at = NULL",
			contracts.Hash(token), db.TenantOf(tx).ID, id, email, at, at.Add(contracts.VerificationLifetime)).Error
	})
}

// retractVerification withdraws a credential whose email the transport refused,
// in a write of its own that commits while the failed delivery rolls back.
//
// Its own, because that rollback is the point: the retry runs the offer from the
// start, and a row the transport never took would sit in the table until its
// twenty-four hours ran out as a credential nobody holds and nothing sends. The
// withdrawal is what keeps "a row exists" meaning "a recipient has a link".
// A withdrawal that itself fails is logged and does not replace the delivery's
// error: the retry then rotates the row it finds and mails a fresh credential, so
// no row outlives its twenty-four hours and no link goes out twice.
func (s *Service) retractVerification(ctx context.Context, id uuid.UUID, token string) {
	err := s.writeVerification(ctx, "withdraw", func(tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("DELETE FROM verification_tokens WHERE user_id = ? AND token_hash = ?",
			id, contracts.Hash(token)).Error
	})
	if err != nil {
		slog.ErrorContext(ctx, "auth: could not withdraw a verification credential its email could not reach",
			"user", id, "error", err)
	}
}

// writeVerification runs one statement about one credential in a transaction
// that commits by itself, which is the only shape that puts a row in front of a
// reader before the message that names it is sent.
//
// db.Detached because the caller's transaction is open on the context and db.Run
// would join it rather than end before the send; context.WithoutCancel because a
// delivery whose deadline has passed must not abandon a row halfway through
// writing it — the statement is bounded by its own budget instead.
func (s *Service) writeVerification(ctx context.Context, what string, stmt func(db.Tx[db.Tenant]) error) error {
	if s.conn == nil {
		return fmt.Errorf("auth: no pool to commit an emailed credential on")
	}
	run, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), credentialWriteBudget)
	defer cancel()
	if err := db.Run(run, s.conn, func(_ context.Context, tx db.Tx[db.Tenant]) error { return stmt(tx) }); err != nil {
		return fmt.Errorf("auth: %s the emailed verification credential: %w", what, err)
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
