// Package internal is every implementation of the user module. Nothing outside
// modules/user can import it, which is the compiler enforcing idea 3: a
// consumer takes contracts.Service, and taking anything else does not build.
package internal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Service is the user lifecycle. It has no fields: everything a command needs
// arrives with the transaction it is given, which is what lets one instance
// serve a request, a job and an event handler at once.
type Service struct{}

// NewService returns the lifecycle commands. module.go constructs it.
func NewService() *Service { return &Service{} }

var _ contracts.Service = (*Service)(nil)

// Invite creates a user with no password. They cannot sign in until somebody
// sets one, which in E3.2 is a link in an email a subscriber to this event
// sends; until then it is SetPassword, called by an administrator.
func (s *Service) Invite(ctx context.Context, tx db.Tx[db.Tenant], email, displayName string) (*contracts.User, error) {
	u := &contracts.User{Email: email, DisplayName: displayName, Status: contracts.StatusInvited}
	if err := crud.Create(ctx, tx, u); err != nil {
		return nil, err
	}
	return u, events.Publish(tx, contracts.EventInvited, contracts.Invited{
		UserID: u.ID, Email: u.Email, Status: u.Status, At: db.Now(),
	})
}

// SetPassword hashes and stores a password, and makes an invited user active.
//
// It is not idempotent and must not be: setting a password to the value it
// already had is still a password change, and a person who did it deliberately
// has to see it in their own audit trail.
func (s *Service) SetPassword(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, password string) error {
	u, err := crud.Get[*contracts.User](tx, id)
	if err != nil {
		return err
	}
	if u.Status == contracts.StatusInactive {
		return fmt.Errorf("%w: a deactivated user cannot be given a password", crud.ErrConflict)
	}
	hash, err := contracts.HashPassword(password)
	if err != nil {
		return fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	u.PasswordHash, u.Status = hash, contracts.StatusActive
	// The columns this changed, and no others: a whole-row write would put
	// every field back to what this transaction read, losing a concurrent
	// change to a field it never touched.
	if err := crud.Update(ctx, tx, u, "password_hash", "status", "updated_at"); err != nil {
		return err
	}
	return events.Publish(tx, contracts.EventPasswordSet, contracts.PasswordSet{UserID: u.ID, At: db.Now()})
}

// SetRoles replaces the roles this user holds. The same set again — in any
// order — changes nothing and publishes nothing: a retried click must not
// appear twice in an audit of who was made an administrator.
func (s *Service) SetRoles(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, roles []string) (*contracts.User, error) {
	u, err := crud.Get[*contracts.User](tx, id)
	if err != nil {
		return nil, err
	}
	want := normalise(roles)
	was := slices.Clone([]string(u.Roles))
	if slices.Equal(was, want) {
		return u, nil
	}
	u.Roles = want
	if err := crud.Update(ctx, tx, u, "roles", "updated_at"); err != nil {
		return nil, err
	}
	return u, events.Publish(tx, contracts.EventRolesSet, contracts.RolesSet{
		UserID: u.ID, Was: was, Now: want, At: db.Now(),
	})
}

// Deactivate stops the user signing in. Deactivating them again changes nothing.
func (s *Service) Deactivate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	u, err := crud.Get[*contracts.User](tx, id)
	if err != nil {
		return nil, err
	}
	if u.Status == contracts.StatusInactive {
		return u, nil
	}
	u.Status = contracts.StatusInactive
	if err := crud.Update(ctx, tx, u, "status", "updated_at"); err != nil {
		return nil, err
	}
	return u, events.Publish(tx, contracts.EventDeactivated, contracts.Deactivated{UserID: u.ID, At: db.Now()})
}

// Get is one user of this tenant.
func (s *Service) Get(_ context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	return crud.Get[*contracts.User](tx, id)
}

// ByEmail is the login lookup. The comparison is lower(email), which is the
// expression the unique index in migrations/000007 is built on, so this is an
// index scan and not a sequential one.
func (s *Service) ByEmail(_ context.Context, tx db.Tx[db.Tenant], email string) (*contracts.User, error) {
	var u contracts.User
	err := tx.DB().Where("lower(email) = ? AND deleted_at IS NULL",
		strings.ToLower(strings.TrimSpace(email))).Take(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, crud.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user: find by email: %w", err)
	}
	return &u, nil
}

// Provision creates an active user with a password in a named tenant, from a
// transaction that belongs to no tenant. See contracts.Service.Provision: this
// is the bootstrap's door, and the tenant is a parameter because the tenant is
// being created in the same transaction.
func (s *Service) Provision(_ context.Context, tx db.Tx[db.System], tenantID uuid.UUID,
	email, displayName, password string, roles []string,
) (*contracts.User, error) {
	hash, err := contracts.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	at := db.Now()
	u := &contracts.User{
		Email: email, DisplayName: displayName, Status: contracts.StatusActive,
		Roles: normalise(roles), PasswordHash: hash,
	}
	u.ID, u.TenantID, u.CreatedAt, u.UpdatedAt = uuid.New(), tenantID, at, at
	if err := u.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	if err := tx.DB().Create(u).Error; err != nil {
		return nil, fmt.Errorf("user: provision %s: %w", u.Email, err)
	}
	return u, events.PublishFor(tx, tenantID, contracts.EventInvited, contracts.Invited{
		UserID: u.ID, Email: u.Email, Status: u.Status, At: at,
	})
}

// normalise is the stored form of a role set: trimmed, lower-cased, deduplicated
// and sorted. Sorted, so that "the same roles in another order" is the same
// value and SetRoles can tell that nothing changed.
func normalise(roles []string) contracts.Roles {
	out := make(contracts.Roles, 0, len(roles))
	for _, r := range roles {
		if r = strings.ToLower(strings.TrimSpace(r)); r != "" && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	slices.Sort(out)
	return out
}
