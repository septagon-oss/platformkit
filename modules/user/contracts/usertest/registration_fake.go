package usertest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func (f *Fake) RegisterPending(ctx context.Context, _ db.Tx[db.Tenant], in contracts.PendingRegistration) (*contracts.User, error) {
	u := &contracts.User{Email: in.Email, DisplayName: in.DisplayName, Status: contracts.StatusPending, Roles: Normalise(in.Roles)}
	if err := u.Validate(ctx); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	hash, err := contracts.HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.users {
		if existing.Email == u.Email {
			return nil, contracts.ErrRegistrationExists
		}
	}
	u.ID, u.CreatedAt, u.UpdatedAt, u.PasswordHash = uuid.New(), db.Now(), db.Now(), hash
	f.users[u.ID] = *u
	f.published = append(f.published, contracts.EventRegistrationPending)
	return f.get(u.ID)
}

func (f *Fake) ApproveRegistration(_ context.Context, _ db.Tx[db.Tenant], id, actor uuid.UUID) (*contracts.User, error) {
	if actor == uuid.Nil {
		return nil, fmt.Errorf("%w: approval requires an acting principal", crud.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := f.get(id)
	if err != nil {
		return nil, err
	}
	if u.Status == contracts.StatusActive {
		return u, nil
	}
	if u.Status != contracts.StatusPending || u.PasswordHash == "" {
		return nil, fmt.Errorf("%w: only a pending registration with a password can be approved", crud.ErrConflict)
	}
	u.Status, u.UpdatedAt = contracts.StatusActive, db.Now()
	f.users[id] = *u
	f.published = append(f.published, contracts.EventRegistrationApproved)
	return f.get(id)
}

func (f *Fake) PendingRegistrations(_ context.Context, _ db.Tx[db.Tenant], limit, offset int) (contracts.RegistrationPage, error) {
	if limit < 0 || limit > crud.MaxLimit || offset < 0 {
		return contracts.RegistrationPage{}, fmt.Errorf("%w: invalid registration page bounds", crud.ErrInvalid)
	}
	limit = cmp.Or(limit, crud.DefaultLimit)
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []*contracts.User
	for id, u := range f.users {
		if u.Status == contracts.StatusPending {
			row, _ := f.get(id)
			items = append(items, row)
		}
	}
	slices.SortFunc(items, func(a, b *contracts.User) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), strings.Compare(a.ID.String(), b.ID.String()))
	})
	total := len(items)
	start := min(offset, total)
	return contracts.RegistrationPage{Items: items[start : start+min(limit, total-start)], Total: int64(total)}, nil
}
