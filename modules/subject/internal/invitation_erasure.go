package internal

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func (s *service) lockErasurePerson(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (personaldata.Subject, error) {
	if s.invitationRetention == nil {
		if s.people != nil {
			u, err := s.people.Retained(ctx, tx, id)
			if err != nil {
				return personaldata.Subject{}, err
			}
			if u.Invitation.Phase == user.InvitationPending {
				return personaldata.Subject{}, user.ErrInvitationUnavailable
			}
		}
		return s.person(ctx, tx, id)
	}
	u, err := s.invitationRetention.LockForErasure(ctx, tx, id)
	if err != nil {
		return personaldata.Subject{}, err
	}
	return personaldata.Subject{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Handle: u.Handle}, nil
}
