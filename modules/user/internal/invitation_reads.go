package internal

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// GetInvitation authorizes the current actor before returning recorded history.
func (s *Service) GetInvitation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.InvitationSnapshot, error) {
	access, err := s.invitationAccess(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	u, err := crud.Get[*contracts.User](tx, id)
	if err != nil {
		return nil, err
	}
	if u.Invitation.Phase == "" {
		if u.Status == contracts.StatusInvited {
			return nil, contracts.ErrInvitationInitializing
		}
		return nil, crud.ErrNotFound
	}
	at := s.invitationNow()
	if err := contracts.CheckInvitationTime(u.Invitation, at); err != nil {
		return nil, err
	}
	return contracts.ReadInvitation(u, access, s.invitationTiming, at), nil
}

// ListInvitations returns a stable page, classifying expiry at the query instant.
func (s *Service) ListInvitations(ctx context.Context, tx db.Tx[db.Tenant], query contracts.InvitationQuery) ([]*contracts.InvitationSnapshot, error) {
	access, err := s.invitationAccess(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	q, err := contracts.CheckInvitationQuery(query)
	if err != nil {
		return nil, err
	}
	at := s.invitationNow()
	var initializing int64
	if err := tx.DB().Model(&contracts.User{}).Where("deleted_at IS NULL AND status = ? AND invitation_phase IS NULL", contracts.StatusInvited).Count(&initializing).Error; err != nil {
		return nil, err
	}
	if initializing > 0 {
		return nil, contracts.ErrInvitationInitializing
	}
	read := tx.DB().Where("deleted_at IS NULL AND invitation_phase IS NOT NULL")
	switch q.State {
	case contracts.InvitationExpired:
		read = read.Where("invitation_phase = 'pending' AND invitation_expires_at <= ?", at)
	case contracts.InvitationPending:
		read = read.Where("invitation_phase = 'pending' AND invitation_expires_at > ?", at)
	case "":
	default:
		read = read.Where("invitation_phase = ?", q.State)
	}
	var users []*contracts.User
	if err := read.Order("created_at DESC, id").Limit(q.Limit).Offset(q.Offset).Find(&users).Error; err != nil {
		return nil, err
	}
	result := make([]*contracts.InvitationSnapshot, 0, len(users))
	for _, u := range users {
		if err := contracts.CheckInvitationTime(u.Invitation, at); err != nil {
			return nil, err
		}
		result = append(result, contracts.ReadInvitation(u, access, s.invitationTiming, at))
	}
	return result, nil
}

// LockForErasure obtains the retained person before any credential owner runs, and
// refuses only a recorded invitation that is still open — pending, or past its
// deadline, which is one phase read two ways. Withdrawal has a revision of its own,
// so a generic erasure must not become a way to skip it. A row carrying no recorded
// history has no revision to withdraw: a person seeded before the adoption file ran,
// or by an owner that writes the row itself, is erased rather than held against a
// credential that was never issued. It writes nothing.
func (s *Service) LockForErasure(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	var u contracts.User
	if err := tx.DB().WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&u).Error; err != nil {
		return nil, crud.Classify(err)
	}
	if u.Invitation.Phase == contracts.InvitationPending {
		return nil, fmt.Errorf("%w: revoke the invitation before erasure", crud.ErrConflict)
	}
	if err := s.invitationFloor(ctx, tx, &u, nil); err != nil {
		return nil, err
	}
	return &u, nil
}
