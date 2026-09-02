package usertest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Fake is contracts.Service over a map: the same rules, no database, no
// transaction. The auth module's tests take one instead of a Postgres.
//
// The passwords are hashed with the same function the real service uses, so a
// consumer testing a login against the fake is testing against real argon2id —
// which is slow on purpose, and is what makes the fake honest here rather than
// fast.
type Fake struct {
	mu        sync.Mutex
	users     map[uuid.UUID]contracts.User
	published []string
}

// NewFake returns an empty store.
func NewFake() *Fake { return &Fake{users: map[uuid.UUID]contracts.User{}} }

var _ contracts.Service = (*Fake)(nil)

// Published is the names of the events the fake would have emitted, in order.
func (f *Fake) Published() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.published)
}

// Invite mirrors internal.Service.Invite.
func (f *Fake) Invite(_ context.Context, _ db.Tx[db.Tenant], email, displayName string) (*contracts.User, error) {
	u := &contracts.User{Email: email, DisplayName: displayName, Status: contracts.StatusInvited}
	if err := u.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.users {
		if strings.EqualFold(existing.Email, u.Email) {
			return nil, fmt.Errorf("%w: users_tenant_email", crud.ErrConflict)
		}
	}
	u.ID, u.CreatedAt, u.UpdatedAt = uuid.New(), stamp(), stamp()
	f.users[u.ID] = *u
	f.published = append(f.published, contracts.EventInvited)
	return f.get(u.ID)
}

// SetPassword mirrors internal.Service.SetPassword.
func (f *Fake) SetPassword(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID, password string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := f.get(id)
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
	u.PasswordHash, u.Status, u.UpdatedAt = hash, contracts.StatusActive, stamp()
	f.users[id] = *u
	f.published = append(f.published, contracts.EventPasswordSet)
	return nil
}

// SetRoles mirrors internal.Service.SetRoles.
func (f *Fake) SetRoles(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID, roles []string) (*contracts.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := f.get(id)
	if err != nil {
		return nil, err
	}
	want := Normalise(roles)
	if slices.Equal([]string(u.Roles), want) {
		return u, nil
	}
	u.Roles, u.UpdatedAt = want, stamp()
	if err := u.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	f.users[id] = *u
	f.published = append(f.published, contracts.EventRolesSet)
	return f.get(id)
}

// Deactivate mirrors internal.Service.Deactivate.
func (f *Fake) Deactivate(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := f.get(id)
	if err != nil {
		return nil, err
	}
	if u.Status == contracts.StatusInactive {
		return u, nil
	}
	u.Status, u.UpdatedAt = contracts.StatusInactive, stamp()
	f.users[id] = *u
	f.published = append(f.published, contracts.EventDeactivated)
	return f.get(id)
}

// Get mirrors internal.Service.Get.
func (f *Fake) Get(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.get(id)
}

// ByEmail mirrors internal.Service.ByEmail.
func (f *Fake) ByEmail(_ context.Context, _ db.Tx[db.Tenant], email string) (*contracts.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := strings.ToLower(strings.TrimSpace(email))
	for id, u := range f.users {
		if u.Email == want {
			return f.get(id)
		}
	}
	return nil, crud.ErrNotFound
}

// Provision mirrors internal.Service.Provision.
func (f *Fake) Provision(_ context.Context, _ db.Tx[db.System], tenantID uuid.UUID,
	email, displayName, password string, roles []string,
) (*contracts.User, error) {
	hash, err := contracts.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	u := &contracts.User{
		Email: email, DisplayName: displayName, Status: contracts.StatusActive,
		Roles: Normalise(roles), PasswordHash: hash,
	}
	u.ID, u.TenantID, u.CreatedAt, u.UpdatedAt = uuid.New(), tenantID, stamp(), stamp()
	if err := u.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[u.ID] = *u
	f.published = append(f.published, contracts.EventInvited)
	return f.get(u.ID)
}

// get is a copy of the stored user, so a caller that mutates what it was handed
// does not reach into the store — which is what a database would do. The caller
// holds the lock.
func (f *Fake) get(id uuid.UUID) (*contracts.User, error) {
	stored, ok := f.users[id]
	if !ok {
		return nil, crud.ErrNotFound
	}
	stored.Roles = slices.Clone(stored.Roles)
	return &stored, nil
}

// Normalise is the stored form of a role set: trimmed, lower-cased,
// deduplicated and sorted, so "the same roles in another order" is the same
// value. It is exported because the fake and the real service have to agree
// about it, and the conformance suite is what says so.
func Normalise(roles []string) contracts.Roles {
	out := make(contracts.Roles, 0, len(roles))
	for _, r := range roles {
		if r = strings.ToLower(strings.TrimSpace(r)); r != "" && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	slices.Sort(out)
	return out
}

func stamp() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }
