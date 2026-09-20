package usertest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

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
	mu sync.Mutex
	// administering is this fake's whole role system: the names it treats as
	// granting the permission that can grant every other one back. The real
	// service asks the application through contracts.Administration; a fake
	// with no database is handed the answer instead.
	administering []string
	users         map[uuid.UUID]contracts.User
	published     []string
}

// NewFake returns an empty store whose tenant has no role system at all: no
// role it treats as administering, so the floor never refuses anything.
//
// That is what this constructor answered before the floor existed and what it
// still answers, so a consumer's tests written against v1.1.0 keep compiling and
// keep meaning the same thing — this package is published, and RELEASE.md
// measures a break against v1.1.0 with no accepted-break baseline. The
// signature is therefore frozen, and what changed is where the answer comes
// from: a tenant with a role system asks NewFakeWithAdministration for one.
//
// The cost of freezing it is worth naming rather than leaving to the reader: a
// test that means to exercise the floor and reaches for NewFake gets a store
// that cannot refuse, and nothing complains. RunService is where that case is
// closed — TestFakeConforms runs the same suite the real service runs against a
// fake built by NewFakeWithAdministration, so the two implementations cannot
// drift apart here quietly, and a consumer's own floor case has to ask for it.
func NewFake() *Fake {
	return NewFakeWithAdministration(nil)
}

// NewFakeWithAdministration returns an empty store whose tenant treats the
// given role names as the ones able to change a role again — the fake's whole
// role system, and the answer it needs to keep contracts.Service's promises the
// same way the real service does.
//
// It is a second constructor rather than a variadic first one. Variadic, every
// call written before the floor existed went on compiling and silently got nil,
// which is the state user.Module panics rather than allow, reintroduced in the
// one place a consumer tests against. A distinct name leaves NewFake's callers
// alone and makes a test that wants the floor say so where it builds the store.
func NewFakeWithAdministration(administering []string) *Fake {
	return &Fake{administering: administering, users: map[uuid.UUID]contracts.User{}}
}

// Delete soft-deletes somebody, the way the generated CRUD route does, and
// refuses the same write rest.Spec's AfterDelete hook refuses.
//
// It is not a contracts.Service method, because deleting a user is generic CRUD
// and there is no Delete on the interface; it takes no transaction for the same
// reason the rest of this fake ignores the one it is handed. It is here so the
// conformance suite can hold both implementations to the floor at that door too
// — without it the third door is tested against the real composition and
// against nothing else. The check is before the write rather than after it,
// because there is no transaction here to roll one back.
func (f *Fake) Delete(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := f.get(id)
	if err != nil {
		return err
	}
	if err := f.floor(u, nil); err != nil {
		return err
	}
	at := db.Now()
	u.DeletedAt, u.UpdatedAt = &at, at
	f.users[id] = *u
	f.published = append(f.published, contracts.EventDeleted)
	return nil
}

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
			return nil, &crud.UniqueConflict{Constraint: "users_tenant_email"}
		}
	}
	u.ID, u.CreatedAt, u.UpdatedAt = uuid.New(), db.Now(), db.Now()
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
	if u.Status != contracts.StatusInvited && u.Status != contracts.StatusActive {
		return fmt.Errorf("%w: only invited or active users can be given a password", crud.ErrConflict)
	}
	hash, err := contracts.HashPassword(password)
	if err != nil {
		return fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	u.PasswordHash, u.Status, u.UpdatedAt = hash, contracts.StatusActive, db.Now()
	f.users[id] = *u
	f.published = append(f.published, contracts.EventPasswordSet)
	return nil
}

// SetRoles mirrors internal.Service.SetRoles, the floor included.
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
	after := *u
	after.Roles = want
	if err := f.floor(u, &after); err != nil {
		return nil, err
	}
	u.Roles, u.UpdatedAt = want, db.Now()
	if err := u.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	f.users[id] = *u
	f.published = append(f.published, contracts.EventRolesSet)
	return f.get(id)
}

// Deactivate mirrors internal.Service.Deactivate, the floor included.
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
	after := *u
	after.Status = contracts.StatusInactive
	if err := f.floor(u, &after); err != nil {
		return nil, err
	}
	u.Status, u.UpdatedAt = contracts.StatusInactive, db.Now()
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

func (f *Fake) SetHandle(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID, handle string) (*contracts.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := f.get(id)
	if err != nil {
		return nil, err
	}
	// Mirrors internal.Service.SetHandle, rules and all: the published fake is the
	// contract test other modules write against, so a fake that skipped the rules
	// would let a caller pass here and fail in production.
	want := strings.ToLower(strings.TrimSpace(handle))
	if want == "" {
		return nil, fmt.Errorf("%w: a handle cannot be released once claimed", crud.ErrInvalid)
	}
	if u.Handle == want {
		return u, nil
	}
	if !contracts.ValidHandle(want) {
		return nil, fmt.Errorf("%w: handle %q is not claimable", crud.ErrInvalid, handle)
	}
	for other, held := range f.users {
		if other != id && held.Handle == want {
			return nil, fmt.Errorf("%w: that handle is already claimed in this tenant", crud.ErrConflict)
		}
	}
	u.Handle, u.UpdatedAt = want, db.Now()
	f.users[id] = *u
	f.published = append(f.published, contracts.EventHandleSet)
	return u, nil
}

func (f *Fake) ByHandle(_ context.Context, _ db.Tx[db.Tenant], handle string) (*contracts.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := strings.ToLower(strings.TrimSpace(handle))
	for id, u := range f.users {
		if strings.ToLower(u.Handle) == want && want != "" {
			return f.get(id)
		}
	}
	return nil, crud.ErrNotFound
}

// Provision mirrors internal.Service.Provision.
func (f *Fake) Provision(_ context.Context, _ db.Tx[db.System], tenantID uuid.UUID,
	email, displayName, password string, roles []string,
) (*contracts.User, error) {
	// An empty password makes an invited user, which is how the control plane
	// gives a tenant its first administrator without knowing their password.
	status, hash := contracts.StatusInvited, ""
	if password != "" {
		var err error
		if hash, err = contracts.HashPassword(password); err != nil {
			return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
		}
		status = contracts.StatusActive
	}
	u := &contracts.User{
		Email: email, DisplayName: displayName, Status: status,
		Roles: Normalise(roles), PasswordHash: hash,
	}
	u.ID, u.TenantID, u.CreatedAt, u.UpdatedAt = uuid.New(), tenantID, db.Now(), db.Now()
	if err := u.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[u.ID] = *u
	f.published = append(f.published, contracts.EventInvited)
	return f.get(u.ID)
}

// floor is internal.Service.floor without a database: the same decision, from
// the same function, so the fake and the service cannot disagree about who the
// last administrator is. The caller holds the lock the closure reads under.
func (f *Fake) floor(before, after *contracts.User) error {
	return contracts.CheckedAdministration(before, after, f.administering, func() ([]*contracts.User, error) {
		others := make([]*contracts.User, 0, len(f.users))
		for id, stored := range f.users {
			if id != before.ID && stored.DeletedAt == nil {
				others = append(others, &stored)
			}
		}
		return others, nil
	})
}

// get is a copy of the stored user, so a caller that mutates what it was handed
// does not reach into the store — which is what a database would do. The caller
// holds the lock.
func (f *Fake) get(id uuid.UUID) (*contracts.User, error) {
	stored, ok := f.users[id]
	if !ok || stored.DeletedAt != nil {
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
