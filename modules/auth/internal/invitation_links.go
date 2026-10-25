package internal

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// ConfigureInvitations supplies companions before requests or workers start.
func (s *Service) ConfigureInvitations(clock user.Clock, users contracts.InvitationUsers, reads user.InvitationReader) {
	s.invitationClock, s.invitationUsers, s.invitationReader = clock, users, reads
}

func (s *Service) invitationNow() time.Time {
	if s.invitationClock == nil {
		return db.Now()
	}
	return s.invitationClock.Now().UTC().Truncate(time.Microsecond)
}

// OfferInvitation sends the named committed generation once the outbox event
// reaches a worker. The generation fence below is what makes a stale event do
// nothing; the handoff itself is recorded in the mail ledger, as every other
// message this module sends is.
func (s *Service) OfferInvitation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, generation int64, delivery uuid.UUID) error {
	if s.invitationUsers == nil || s.mail.Mails == nil {
		return user.ErrInvitationUnavailable
	}
	u, err := s.invitationUsers.LockInvitation(ctx, tx, id)
	if errors.Is(err, crud.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := user.CheckInvitationTime(u.Invitation, s.invitationNow()); err != nil {
		return err
	}
	if !contracts.InvitationOfferEligible(u, generation, delivery, s.invitationNow()) {
		return nil
	}
	if err := passwordLock(tx, id); err != nil {
		return err
	}
	// A live credential for this generation means its link is already out: the
	// same event arriving again is a retry of work that is done, not a second
	// handoff to send.
	live, err := s.liveInvitationTokens(ctx, tx, id, generation)
	if err != nil || live > 0 {
		return err
	}
	base, err := s.baseURL(ctx, tx)
	if err != nil {
		return err
	}
	recipient := u.Invitation.Recipient
	at := s.invitationNow()
	if err := user.CheckInvitationTime(u.Invitation, at); err != nil {
		return err
	}
	if !contracts.InvitationOfferEligible(u, generation, delivery, at) {
		return s.invitationMailResult(ctx, tx, recipient, notification.MailSuppressed, "expired_before_send")
	}
	if s.mail.Mailer == nil {
		return s.invitationMailResult(ctx, tx, recipient, notification.MailSuppressed, "transport_unavailable")
	}
	return s.sendInvitation(ctx, tx, u, recipient, base, at)
}

// liveInvitationTokens counts the unused credentials this person holds for one
// invitation generation.
func (s *Service) liveInvitationTokens(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, generation int64) (int64, error) {
	var live int64
	err := tx.DB().Table("password_tokens").Where("user_id = ? AND invitation_generation = ? AND expires_at > ?",
		id, generation, s.invitationNow()).Count(&live).Error
	return live, err
}

func (s *Service) sendInvitation(ctx context.Context, tx db.Tx[db.Tenant], u *user.User, recipient, base string, at time.Time) error {
	token := secret()
	err := tx.DB().Exec(`INSERT INTO password_tokens
		(token_hash, tenant_id, user_id, created_at, expires_at, invitation_generation) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (tenant_id, user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash,
		created_at = EXCLUDED.created_at, expires_at = EXCLUDED.expires_at, invitation_generation = EXCLUDED.invitation_generation`,
		contracts.Hash(token), db.TenantOf(tx).ID, u.ID, at, u.Invitation.ExpiresAt, u.Invitation.Generation).Error
	if err != nil {
		return err
	}
	message := notification.Message{To: recipient, Subject: inviteSubject,
		Body: inviteBody + "\n\n" + base + s.mail.SetPasswordPage + "?token=" + token +
			"\n\nThis link works once and expires at " + u.Invitation.ExpiresAt.Format(time.RFC3339) + "."}
	if err := s.mail.Mailer.Send(ctx, message); err != nil {
		if err := tx.DB().Exec("DELETE FROM password_tokens WHERE user_id = ? AND invitation_generation = ?", u.ID, u.Invitation.Generation).Error; err != nil {
			return err
		}
		return s.invitationMailResult(ctx, tx, recipient, notification.MailFailed, "transport_error")
	}
	if s.notify != nil {
		if _, err := s.notify.Notify(ctx, tx, notification.Notice{Recipient: u.ID, Title: inviteSubject,
			Body: "A password setup link was accepted by the email transport.", Link: s.mail.SetPasswordPage}); err != nil {
			return err
		}
	}
	return s.invitationMailResult(ctx, tx, recipient, notification.MailSent, "")
}

// invitationMailResult appends one outcome for one set-password message in the
// caller's transaction, so the row and the domain change commit or vanish
// together, and says so on the event door when the transport refused.
func (s *Service) invitationMailResult(ctx context.Context, tx db.Tx[db.Tenant], recipient, outcome, reason string) error {
	trace, request := originOf(ctx)
	if err := s.mail.Mails.RecordMail(ctx, tx, notification.MailRecord{Recipient: recipient,
		Kind: contracts.MailSetPassword, Outcome: outcome, Reason: reason, Traceparent: trace, RequestID: request}); err != nil {
		return err
	}
	if outcome != notification.MailFailed {
		return nil
	}
	return events.Publish(ctx, tx, contracts.EventMailFailed, contracts.MailFailed{Kind: contracts.MailSetPassword, Recipient: recipient,
		Reason: reason, RequestID: request, Traceparent: trace, At: s.invitationNow()})
}
