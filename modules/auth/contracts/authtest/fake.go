package authtest

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Fake is contracts.Service over two maps: the same rules, no database, no
// transaction. A consumer that wants to test what it does for a signed-in
// caller takes one of these instead of a Postgres.
//
// It keeps the real limiter and hashes with the real argon2id, because both are
// part of what Login promises rather than of how it stores things. What it
// cannot be is what row-level security is: there is one tenant here, so "a
// session from another tenant is invisible" is a claim only the real service
// can be held to, and internal/service_test.go holds it.
type Fake struct {
	// Users is where the fake finds people. It is exported so that a consumer
	// invites somebody through the same interface the real module takes.
	Users contracts.Users

	limiter *contracts.Limiter

	mu        sync.Mutex
	sessions  map[uuid.UUID]contracts.Session
	roles     map[string][]string
	published []string
}

// NewFake returns a fake signing people in from users.
func NewFake(users contracts.Users) *Fake {
	return &Fake{
		Users:    users,
		limiter:  contracts.NewLimiter(),
		sessions: map[uuid.UUID]contracts.Session{},
		roles:    map[string][]string{},
	}
}

var _ contracts.Service = (*Fake)(nil)

// Published is the names of the events the fake would have emitted, in order.
func (f *Fake) Published() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.published)
}

// Grant is the fake's stand-in for a roles table somebody edited.
func (f *Fake) Grant(name string, permissions ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.roles[name] = slices.Clone(permissions)
}

// Login mirrors internal.Service.Login, including the dummy hash on the path
// where no user was found.
func (f *Fake) Login(ctx context.Context, tx db.Tx[db.Tenant], email, password string, from contracts.Client) (*contracts.Session, *contracts.Identity, error) {
	if f.limiter.Locked(email) {
		f.record(contracts.EventLoginFailed)
		return nil, nil, contracts.ErrTooManyAttempts
	}
	user, err := f.Users.ByEmail(ctx, tx, email)
	switch {
	case err != nil && !isNotFound(err):
		return nil, nil, err
	case err != nil, !user.CanSignIn():
		usercontracts.EqualWork(password)
		return nil, nil, f.fail(email)
	case !user.CheckPassword(password):
		return nil, nil, f.fail(email)
	}
	f.limiter.Succeeded(email)
	return f.open(ctx, tx, user, from)
}

// Open mirrors internal.Service.Open.
func (f *Fake) Open(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, from contracts.Client) (*contracts.Session, *contracts.Identity, error) {
	user, err := f.Users.Get(ctx, tx, id)
	if err != nil {
		return nil, nil, err
	}
	if user.Status != usercontracts.StatusActive {
		return nil, nil, contracts.ErrCredentials
	}
	return f.open(ctx, tx, user, from)
}

func (f *Fake) open(ctx context.Context, tx db.Tx[db.Tenant], user *usercontracts.User, from contracts.Client) (*contracts.Session, *contracts.Identity, error) {
	at := db.Now()
	session := contracts.Session{
		ID: uuid.New(), UserID: user.ID, CreatedAt: at,
		ExpiresAt: at.Add(contracts.SessionLifetime), LastSeenAt: at,
		UserAgent: from.UserAgent, IP: from.IP,
	}
	f.mu.Lock()
	f.sessions[session.ID] = session
	f.mu.Unlock()
	identity, err := f.identify(ctx, tx, user)
	if err != nil {
		return nil, nil, err
	}
	f.record(contracts.EventLoggedIn)
	return &session, identity, nil
}

// Identify mirrors internal.Service.Identify, sliding expiry and all.
func (f *Fake) Identify(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, from contracts.Client) (*contracts.Identity, error) {
	f.mu.Lock()
	session, ok := f.sessions[id]
	f.mu.Unlock()
	if !ok || !session.ExpiresAt.After(time.Now()) {
		return nil, crud.ErrNotFound
	}
	user, err := f.Users.Get(ctx, tx, session.UserID)
	if err != nil {
		return nil, err
	}
	if user.Status != usercontracts.StatusActive {
		return nil, crud.ErrNotFound
	}
	if at := db.Now(); at.Sub(session.LastSeenAt) >= contracts.SessionTouch {
		session.LastSeenAt, session.ExpiresAt, session.IP = at, at.Add(contracts.SessionLifetime), from.IP
		f.mu.Lock()
		f.sessions[id] = session
		f.mu.Unlock()
	}
	return f.identify(ctx, tx, user)
}

// Logout mirrors internal.Service.Logout.
func (f *Fake) Logout(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID) error {
	f.mu.Lock()
	_, ok := f.sessions[id]
	delete(f.sessions, id)
	f.mu.Unlock()
	if ok {
		f.record(contracts.EventLoggedOut)
	}
	return nil
}

// SeedRoles mirrors internal.Service.SeedRoles.
func (f *Fake) SeedRoles(context.Context, db.Tx[db.System], uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for name, permissions := range map[string][]string{
		contracts.RoleAdmin:  {contracts.Wildcard},
		contracts.RoleMember: {},
	} {
		if _, taken := f.roles[name]; !taken {
			f.roles[name] = permissions
		}
	}
	return nil
}

// Permissions mirrors internal.Service.Permissions.
func (f *Fake) Permissions(_ context.Context, _ db.Tx[db.Tenant], roles []string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, role := range roles {
		for _, p := range f.roles[role] {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out, nil
}

func (f *Fake) identify(ctx context.Context, tx db.Tx[db.Tenant], user *usercontracts.User) (*contracts.Identity, error) {
	permissions, err := f.Permissions(ctx, tx, user.Roles)
	if err != nil {
		return nil, err
	}
	return &contracts.Identity{
		UserID: user.ID, Email: user.Email, DisplayName: user.DisplayName,
		Roles: user.Roles, Permissions: permissions,
	}, nil
}

func (f *Fake) fail(email string) error {
	f.limiter.Failed(email)
	f.record(contracts.EventLoginFailed)
	return contracts.ErrCredentials
}

func (f *Fake) record(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, name)
}

func isNotFound(err error) bool { return errors.Is(err, crud.ErrNotFound) }
