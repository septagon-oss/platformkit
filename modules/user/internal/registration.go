package internal

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"gorm.io/gorm/clause"
)

func (s *Service) RegisterPending(ctx context.Context, tx db.Tx[db.Tenant], in contracts.PendingRegistration) (*contracts.User, error) {
	u := &contracts.User{Email: in.Email, DisplayName: in.DisplayName, Status: contracts.StatusPending, Roles: normalise(in.Roles)}
	if err := u.Validate(ctx); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	hash, err := contracts.HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	u.PasswordHash = hash
	u.ID, u.TenantID = uuid.New(), db.TenantOf(tx).ID
	// Match the partial expression index exactly. A duplicate is a no-op,
	// not a SQL error that would poison the caller's acknowledgment transaction.
	// Hashing happens for both new and existing addresses before this write;
	// there is no account lookup and no credential update on conflict.
	created := tx.DB().Clauses(clause.OnConflict{
		Columns:     []clause.Column{{Name: "tenant_id"}, {Name: "lower(email)", Raw: true}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Eq{Column: "deleted_at", Value: nil}}},
		DoNothing:   true,
	}).Create(u)
	if created.Error != nil {
		return nil, crud.Classify(created.Error)
	}
	if created.RowsAffected == 0 {
		return nil, contracts.ErrRegistrationExists
	}
	// This is not an invitation: no password link may activate this account.
	return u, events.Publish(ctx, tx, contracts.EventRegistrationPending, contracts.RegistrationPending{UserID: u.ID, At: db.Now()})
}

func (s *Service) PendingRegistrations(_ context.Context, tx db.Tx[db.Tenant], limit, offset int) (contracts.RegistrationPage, error) {
	if limit < 0 || limit > crud.MaxLimit || offset < 0 {
		return contracts.RegistrationPage{}, fmt.Errorf("%w: invalid registration page bounds", crud.ErrInvalid)
	}
	items, total, err := crud.List[*contracts.User](tx, crud.Query{Limit: limit, Offset: offset, Sort: "createdAt", Filter: map[string]any{"status": contracts.StatusPending}})
	return contracts.RegistrationPage{Items: items, Total: total}, err
}

func (s *Service) ApproveRegistration(ctx context.Context, tx db.Tx[db.Tenant], id, actor uuid.UUID) (*contracts.User, error) {
	if actor == uuid.Nil {
		return nil, fmt.Errorf("%w: approval requires an acting principal", crud.ErrInvalid)
	}
	u, err := lockedUser(tx, id)
	if err != nil {
		return nil, err
	}
	if u.Status == contracts.StatusActive {
		return u, nil
	}
	if u.Status != contracts.StatusPending || u.PasswordHash == "" {
		return nil, fmt.Errorf("%w: only a pending registration with a password can be approved", crud.ErrConflict)
	}
	u.Status = contracts.StatusActive
	if err := crud.Update(ctx, tx, u, "status", "updated_at"); err != nil {
		return nil, err
	}
	return u, events.Publish(ctx, tx, contracts.EventRegistrationApproved, contracts.RegistrationApproved{UserID: u.ID, Actor: actor, At: db.Now()})
}

// Status decisions and their writes share a row lock. In particular a password
// change cannot read active, wait for deactivation, then write active back.
func lockedUser(tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	var u contracts.User
	if err := tx.DB().Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND deleted_at IS NULL", id).Take(&u).Error; err != nil {
		return nil, crud.Classify(err)
	}
	return &u, nil
}
