package internal

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

type passwordCredential struct {
	UserID               uuid.UUID
	ExpiresAt            time.Time
	InvitationGeneration int64
}

func (s *Service) consumePassword(ctx context.Context, tx db.Tx[db.Tenant], token, password string) (uuid.UUID, error) {
	var located passwordCredential
	query := tx.DB().Table("password_tokens").Where("token_hash = ?", contracts.Hash(token))
	if err := query.Take(&located).Error; err != nil {
		return uuid.Nil, passwordCredentialError(err)
	}
	var person *user.User
	var err error
	if s.invitationUsers != nil {
		person, err = s.invitationUsers.LockInvitation(ctx, tx, located.UserID)
		if err != nil {
			return uuid.Nil, passwordCredentialError(err)
		}
	} else {
		person, err = s.users.Get(ctx, tx, located.UserID)
		if err != nil {
			return uuid.Nil, passwordCredentialError(err)
		}
		if person.Invitation.Phase != "" || located.InvitationGeneration > 0 {
			return uuid.Nil, user.ErrInvitationUnavailable
		}
	}
	if err := passwordLock(tx, located.UserID); err != nil {
		return uuid.Nil, err
	}
	var current passwordCredential
	if err := query.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current).Error; err != nil {
		return uuid.Nil, passwordCredentialError(err)
	}
	at := s.invitationNow()
	if at.IsZero() || !at.Before(current.ExpiresAt) || current.UserID != person.ID {
		return uuid.Nil, contracts.ErrCredentials
	}
	if person.Invitation.Phase == user.InvitationPending {
		err = passwordCredentialError(s.invitationUsers.AcceptInvitationPassword(ctx, tx, person.ID, current.InvitationGeneration, password, at))
	} else if current.InvitationGeneration != 0 || person.Invitation.Phase == user.InvitationRevoked {
		return uuid.Nil, contracts.ErrCredentials
	} else {
		err = s.users.SetPassword(ctx, tx, person.ID, password)
	}
	if err != nil {
		return uuid.Nil, err
	}
	result := tx.DB().Exec("DELETE FROM password_tokens WHERE token_hash = ? AND user_id = ?", contracts.Hash(token), person.ID)
	if result.Error != nil {
		return uuid.Nil, result.Error
	}
	if result.RowsAffected != 1 {
		return uuid.Nil, contracts.ErrCredentials
	}
	return person.ID, nil
}

func passwordCredentialError(err error) error {
	if errors.Is(crud.Classify(err), crud.ErrNotFound) || errors.Is(err, crud.ErrConflict) {
		return contracts.ErrCredentials
	}
	return err
}
