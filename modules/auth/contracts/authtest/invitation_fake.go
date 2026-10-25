package authtest

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

type invitationCredential struct {
	Generation int64
	ExpiresAt  time.Time
}

// ConfigureInvitations enables the SQL owner's generation rules. Outcomes land in
// the supplied mail ledger; this fake cannot simulate rollback.
func (f *Fake) ConfigureInvitations(clock user.Clock, users contracts.InvitationUsers, ledger notification.MailLedger) {
	f.invitationClock, f.invitationUsers, f.invitationLedger = clock, users, ledger
	f.invitationTokens = map[string]invitationCredential{}
}
func (f *Fake) invitationNow() time.Time {
	if f.invitationClock == nil {
		return db.Now()
	}
	return f.invitationClock.Now().UTC().Truncate(time.Microsecond)
}

// OfferInvitation applies the same current-generation fence and live-link rule.
func (f *Fake) OfferInvitation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, generation int64, delivery uuid.UUID) error {
	if f.invitationUsers == nil || f.invitationLedger == nil {
		return user.ErrInvitationUnavailable
	}
	u, err := f.invitationUsers.LockInvitation(ctx, tx, id)
	if errors.Is(err, crud.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := user.CheckInvitationTime(u.Invitation, f.invitationNow()); err != nil {
		return err
	}
	if !contracts.InvitationOfferEligible(u, generation, delivery, f.invitationNow()) {
		return nil
	}
	recipient := u.Invitation.Recipient
	if err := user.CheckInvitationTime(u.Invitation, f.invitationNow()); err != nil {
		return err
	}
	if !contracts.InvitationOfferEligible(u, generation, delivery, f.invitationNow()) {
		return f.invitationResult(ctx, tx, recipient, notification.MailSuppressed, "expired_before_send")
	}
	if f.Mailer == nil {
		return f.invitationResult(ctx, tx, recipient, notification.MailSuppressed, "transport_unavailable")
	}
	token := uuid.NewString()
	f.mu.Lock()
	for key, person := range f.tokens {
		if person == id {
			if held := f.invitationTokens[key]; held.Generation == generation && f.invitationNow().Before(held.ExpiresAt) {
				f.mu.Unlock()
				return nil
			}
			delete(f.tokens, key)
			delete(f.invitationTokens, key)
		}
	}
	f.tokens[token] = id
	f.invitationTokens[token] = invitationCredential{generation, *u.Invitation.ExpiresAt}
	f.mu.Unlock()
	if err := f.Mailer.Send(ctx, notification.Message{To: recipient, Subject: "Set your password", Body: "https://acme.example.com" + resetPage + "?token=" + token}); err != nil {
		f.mu.Lock()
		delete(f.tokens, token)
		delete(f.invitationTokens, token)
		f.mu.Unlock()
		return f.invitationResult(ctx, tx, recipient, notification.MailFailed, "transport_error")
	}
	if f.Notify != nil {
		if _, err := f.Notify.Notify(ctx, tx, notification.Notice{Recipient: u.ID, Title: "Set your password", Body: "A password setup link was accepted by the email transport.", Link: resetPage}); err != nil {
			return err
		}
	}
	return f.invitationResult(ctx, tx, recipient, notification.MailSent, "")
}

// invitationResult appends one outcome for one set-password message, as the SQL
// owner does in the caller's transaction.
func (f *Fake) invitationResult(ctx context.Context, tx db.Tx[db.Tenant], recipient, outcome, reason string) error {
	err := f.invitationLedger.RecordMail(ctx, tx, notification.MailRecord{Recipient: recipient, Kind: contracts.MailSetPassword, Outcome: outcome, Reason: reason})
	if err == nil && outcome == notification.MailFailed {
		f.record(contracts.EventMailFailed)
	}
	return err
}
func (f *Fake) resetPassword(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, token, password string) error {
	f.mu.Lock()
	credential, managed := f.invitationTokens[token]
	f.mu.Unlock()
	u, err := f.Users.Get(ctx, tx, id)
	if err != nil {
		return err
	}
	if managed || (u.Invitation.Phase != "" && u.Invitation.Phase != user.InvitationAccepted) {
		if f.invitationUsers == nil {
			return user.ErrInvitationUnavailable
		}
		if !managed || !f.invitationNow().Before(credential.ExpiresAt) {
			return contracts.ErrCredentials
		}
		err = f.invitationUsers.AcceptInvitationPassword(ctx, tx, id, credential.Generation, password, f.invitationNow())
		if errors.Is(err, crud.ErrConflict) || errors.Is(err, crud.ErrNotFound) {
			return contracts.ErrCredentials
		}
		return err
	}
	return f.Users.SetPassword(ctx, tx, id, password)
}
