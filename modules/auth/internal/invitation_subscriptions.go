package internal

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

type invitationSourceKey struct{}

// InvitationSubscriptions carries the event's generation and original decision
// time through the existing invitation and public recovery consumers.
func InvitationSubscriptions(s *Service) []events.Subscription {
	initial := events.Subscription{Module: "auth", Name: user.EventInvited, Handler: s.handleInvitation, Terminal: s.failedInvitation}
	resent := initial
	resent.Name = user.EventInvitationResent
	return []events.Subscription{initial, resent, {
		Module: "auth", Name: contracts.EventResetRequested,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			var asked contracts.ResetRequested
			if err := json.Unmarshal(ev.Payload, &asked); err != nil {
				return errors.New("auth: invalid password recovery event")
			}
			ctx = context.WithValue(WithOrigin(WithServed(ctx, asked.Served), ev), invitationSourceKey{}, contracts.InvitationSource{ID: ev.ID, At: asked.At, Kind: contracts.InvitationRecoveryRequest})
			return s.Reissue(ctx, tx, asked.Email)
		},
	}}
}

func (s *Service) handleInvitation(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
	var intent user.Invited
	if err := json.Unmarshal(ev.Payload, &intent); err != nil {
		return errors.New("auth: invalid invitation event")
	}
	ctx = WithOrigin(WithServed(ctx, intent.Served), ev)
	ctx = context.WithValue(ctx, invitationSourceKey{}, contracts.InvitationSource{ID: ev.ID, At: intent.At, Kind: contracts.InvitationLegacyRequest})
	if intent.Generation == 0 {
		return s.offerLegacyInvitation(ctx, tx, intent.UserID)
	}
	if intent.DeliveryID == nil {
		return user.ErrInvitationUnavailable
	}
	return s.OfferInvitation(ctx, tx, intent.UserID, intent.Generation, *intent.DeliveryID)
}

func (s *Service) requestInvitationRecovery(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error {
	if s.invitationUsers == nil {
		return user.ErrInvitationUnavailable
	}
	source, ok := ctx.Value(invitationSourceKey{}).(contracts.InvitationSource)
	if !ok {
		source = contracts.InvitationSource{ID: uuid.New(), At: s.invitationNow(), Kind: contracts.InvitationRecoveryRequest}
	}
	return s.invitationUsers.RequestInvitationLink(ctx, tx, id, source)
}

func (s *Service) offerLegacyInvitation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error {
	if s.invitationUsers == nil {
		return s.Offer(ctx, tx, id)
	}
	u, err := s.invitationUsers.LockInvitation(ctx, tx, id)
	if errors.Is(err, crud.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if u.Invitation.Phase == "" {
		return s.offerUser(ctx, tx, id)
	}
	if u.Invitation.Generation != 0 || u.Status != user.StatusInvited {
		return nil
	}
	if err := passwordLock(tx, id); err != nil {
		return err
	}
	var live int64
	if err := tx.DB().Table("password_tokens").Where("user_id = ? AND expires_at > ?", id, s.invitationNow()).Count(&live).Error; err != nil {
		return err
	}
	if live > 0 {
		return nil
	}
	source, ok := ctx.Value(invitationSourceKey{}).(contracts.InvitationSource)
	if !ok {
		source = contracts.InvitationSource{ID: uuid.New(), At: s.invitationNow(), Kind: contracts.InvitationLegacyRequest}
	}
	return s.invitationUsers.RequestInvitationLink(ctx, tx, id, source)
}

func (s *Service) failedInvitation(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event, _ error) error {
	var intent user.Invited
	if json.Unmarshal(ev.Payload, &intent) != nil || intent.DeliveryID == nil || s.invitationUsers == nil || s.mail.Mails == nil {
		return nil
	}
	u, err := s.invitationUsers.LockInvitation(ctx, tx, intent.UserID)
	if errors.Is(err, crud.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !contracts.InvitationOfferEligible(u, intent.Generation, *intent.DeliveryID, s.invitationNow()) {
		return nil
	}
	return s.invitationMailResult(WithOrigin(ctx, ev), tx, u.Invitation.Recipient, notification.MailSuppressed, "worker_failed")
}
