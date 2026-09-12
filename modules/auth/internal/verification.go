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
			return svc.offerVerification(ctx, tx, registered.UserID, registered.Email)
		},
	}, {
		Module: "auth", Name: contracts.EventVerificationRequested,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], event events.Event) error {
			var asked contracts.VerificationRequested
			if err := json.Unmarshal(event.Payload, &asked); err != nil {
				return fmt.Errorf("auth: read verification request: %w", err)
			}
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
	var recent bool
	err = tx.DB().Raw("SELECT EXISTS (SELECT 1 FROM verification_tokens WHERE user_id = ? AND email = ? AND created_at > clock_timestamp() - ?::interval)",
		id, current.Email, verificationInterval).Row().Scan(&recent)
	if err != nil || recent {
		return err
	}
	base, err := s.baseURL(ctx, tx)
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
			base + contracts.VerifyEmailPath + "?token=" + token +
			"\n\nThe link works once and expires in 24 hours. If you did not request this account, ignore this message.",
	})
	if err != nil {
		// A transport may quote its input in an error. The outbox retains handler
		// errors, so never carry the mailer's potentially credential-bearing text.
		return fmt.Errorf("auth: verification email delivery failed")
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
