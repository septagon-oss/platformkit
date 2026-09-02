package authtest

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
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

	// Operator are the permissions the operator's own administrator is granted
	// by name, the same list the application hands the real module.
	Operator []string

	limiter *contracts.Limiter

	// Notify is where a set-password or reset link goes. A fake with none
	// writes no token and sends nothing, exactly as the real service does.
	Notify contracts.Notifier

	mu        sync.Mutex
	sessions  map[uuid.UUID]contracts.Session
	roles     map[string]contracts.Permissions
	tokens    map[string]uuid.UUID
	published []string
}

// NewFake returns a fake signing people in from users.
func NewFake(users contracts.Users) *Fake {
	return &Fake{
		Users:    users,
		limiter:  contracts.NewLimiter(),
		sessions: map[uuid.UUID]contracts.Session{},
		roles:    map[string]contracts.Permissions{},
		tokens:   map[string]uuid.UUID{},
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

// RevokeSessions mirrors internal.Service.RevokeSessions.
func (f *Fake) RevokeSessions(_ context.Context, _ db.Tx[db.Tenant], userID, except uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, s := range f.sessions {
		if s.UserID == userID && id != except {
			delete(f.sessions, id)
		}
	}
	return nil
}

// ChangePassword mirrors internal.Service.ChangePassword, current password and
// session revocation and all: both are what the route promises rather than how
// it stores anything.
func (f *Fake) ChangePassword(ctx context.Context, tx db.Tx[db.Tenant], userID, keep uuid.UUID, current, next string) error {
	user, err := f.Users.Get(ctx, tx, userID)
	if err != nil {
		return err
	}
	if !user.CanSignIn() || !user.CheckPassword(current) {
		return contracts.ErrCredentials
	}
	if err := f.Users.SetPassword(ctx, tx, userID, next); err != nil {
		return err
	}
	return f.RevokeSessions(ctx, tx, userID, keep)
}

// Forget mirrors internal.Service.Forget: nil on every path, including the ones
// where nothing was sent.
func (f *Fake) Forget(ctx context.Context, tx db.Tx[db.Tenant], email string) error {
	user, err := f.Users.ByEmail(ctx, tx, email)
	switch {
	case isNotFound(err):
		return nil
	case err != nil:
		return err
	case user.Status == usercontracts.StatusInactive:
		return nil
	}
	return f.offer(ctx, tx, user.ID)
}

// Offer mirrors internal.Service.Offer.
func (f *Fake) Offer(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) error {
	user, err := f.Users.Get(ctx, tx, userID)
	if isNotFound(err) {
		return nil
	}
	if err != nil || user.Status != usercontracts.StatusInvited {
		return err
	}
	return f.offer(ctx, tx, userID)
}

// offer keeps one pending token per person, which is the rule the unique index
// enforces in the real one.
func (f *Fake) offer(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) error {
	if f.Notify == nil {
		return nil
	}
	token := uuid.NewString()
	f.mu.Lock()
	for t, u := range f.tokens {
		if u == userID {
			delete(f.tokens, t)
		}
	}
	f.tokens[token] = userID
	f.mu.Unlock()
	_, err := f.Notify.Notify(ctx, tx, notificationcontracts.Notice{
		Recipient: userID, Title: "Set your password", Link: "/auth/reset?token=" + token, Email: true,
	})
	return err
}

// Tokens is the pending tokens, for a consumer that wants to follow the link a
// fake would have mailed.
func (f *Fake) Tokens() map[string]uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.tokens)
}

// Reset mirrors internal.Service.Reset: one use, every session ended, one event.
func (f *Fake) Reset(ctx context.Context, tx db.Tx[db.Tenant], token, password string) error {
	f.mu.Lock()
	userID, ok := f.tokens[token]
	delete(f.tokens, token)
	f.mu.Unlock()
	if !ok {
		return contracts.ErrCredentials
	}
	if err := f.Users.SetPassword(ctx, tx, userID, password); err != nil {
		return err
	}
	if err := f.RevokeSessions(ctx, tx, userID, uuid.Nil); err != nil {
		return err
	}
	f.record(contracts.EventPasswordReset)
	return nil
}

// Roles mirrors internal.Service.Roles.
func (f *Fake) Roles(_ context.Context, _ db.Tx[db.Tenant]) ([]*contracts.Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*contracts.Role, 0, len(f.roles))
	for name, permissions := range f.roles {
		out = append(out, &contracts.Role{Name: name, Grants: slices.Clone(permissions)})
	}
	slices.SortFunc(out, func(a, b *contracts.Role) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// SetRole mirrors internal.Service.SetRole, both refusals included: a
// permission nothing declares and an operator permission outside the operator's
// own tenant are what the route promises to refuse, so the fake refuses them.
func (f *Fake) SetRole(_ context.Context, tx db.Tx[db.Tenant], name string, permissions []string, declared []tenancy.Grant) (*contracts.Role, error) {
	tenant := db.TenantOf(tx)
	want := make(contracts.Permissions, 0, len(permissions))
	for _, p := range permissions {
		if p == contracts.Wildcard {
			want = append(want, p)
			continue
		}
		i := slices.IndexFunc(declared, func(g tenancy.Grant) bool { return g.Permission == p })
		switch {
		case i < 0:
			return nil, fmt.Errorf("%w: no module defines the permission %q", crud.ErrInvalid, p)
		case declared[i].Operator && !tenant.Operator:
			return nil, fmt.Errorf("%w: %q belongs to the operator of this installation", crud.ErrInvalid, p)
		}
		want = append(want, p)
	}
	slices.Sort(want)
	f.mu.Lock()
	was, existed := f.roles[name]
	same := existed && slices.Equal([]string(was), []string(want))
	f.roles[name] = want
	f.mu.Unlock()
	if !same {
		f.record(contracts.EventRoleSet)
	}
	return &contracts.Role{TenantID: tenant.ID, Name: name, Grants: want}, nil
}

// Purge mirrors internal.Service.Purge over the two maps.
func (f *Fake) Purge(_ context.Context, _ db.Tx[db.Tenant]) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var gone int64
	now := time.Now()
	for id, s := range f.sessions {
		if !s.ExpiresAt.After(now) || !s.CreatedAt.Add(contracts.SessionMaxLifetime).After(now) {
			delete(f.sessions, id)
			gone++
		}
	}
	return gone, nil
}

// Login mirrors internal.Service.Login, including the dummy hash on the path
// where no user was found.
func (f *Fake) Login(ctx context.Context, tx db.Tx[db.Tenant], email, password string, from contracts.Client) (*contracts.Session, *contracts.Identity, error) {
	if f.limiter.Check(email, from.IP) == contracts.Refuse {
		f.record(contracts.EventLoginFailed)
		return nil, nil, contracts.ErrTooManyAttempts
	}
	// The soft delay is not slept here. It is a property of the real service's
	// wall clock and a fake that slept two seconds would make every consumer's
	// suite slow to prove something this store cannot be wrong about.
	user, err := f.Users.ByEmail(ctx, tx, email)
	switch {
	case err != nil && !isNotFound(err):
		return nil, nil, err
	case err != nil, !user.CanSignIn():
		usercontracts.EqualWork(password)
		return nil, nil, f.fail(email, from.IP)
	case !user.CheckPassword(password):
		return nil, nil, f.fail(email, from.IP)
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
	now := time.Now()
	if !ok || !session.ExpiresAt.After(now) || !session.CreatedAt.Add(contracts.SessionMaxLifetime).After(now) {
		f.mu.Lock()
		delete(f.sessions, id)
		f.mu.Unlock()
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
		session.LastSeenAt, session.ExpiresAt = at, at.Add(contracts.SessionLifetime)
		if cap := session.CreatedAt.Add(contracts.SessionMaxLifetime); session.ExpiresAt.After(cap) {
			session.ExpiresAt = cap
		}
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

// SeedRoles mirrors internal.Service.SeedRoles, Operator and all: the fake is
// held to the same rule, so a consumer testing against it sees the same refusal
// the real service gives.
func (f *Fake) SeedRoles(_ context.Context, _ db.Tx[db.System], _ uuid.UUID, operator bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	admin := []string{contracts.Wildcard}
	if operator {
		admin = append(admin, f.Operator...)
	}
	for name, permissions := range map[string][]string{
		contracts.RoleAdmin:  admin,
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

func (f *Fake) fail(email, ip string) error {
	f.limiter.Failed(email, ip)
	f.record(contracts.EventLoginFailed)
	return contracts.ErrCredentials
}

func (f *Fake) record(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, name)
}

func isNotFound(err error) bool { return errors.Is(err, crud.ErrNotFound) }

// Notices is a contracts.Notifier that records instead of sending, so a
// conformance case can read the link an implementation would have mailed.
//
// It is here rather than in each harness because both of them need it and the
// cases below read what it holds: the suite's claim is that a reset link is
// issued and works once, and a recorder is the only way to say that without a
// mail server.
type Notices struct {
	mu   sync.Mutex
	sent []notificationcontracts.Notice
}

// Notify records the notice and writes nothing.
func (n *Notices) Notify(_ context.Context, _ db.Tx[db.Tenant], notice notificationcontracts.Notice) (*notificationcontracts.Notification, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, notice)
	return &notificationcontracts.Notification{}, nil
}

// Sent is every notice so far, in order.
func (n *Notices) Sent() []notificationcontracts.Notice {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.sent)
}

// TokenIn is the token a set-password link carries. Both implementations spell
// the link the same way, which is what makes it a property of the contract
// rather than of either one.
func TokenIn(link string) string {
	_, token, _ := strings.Cut(link, "token=")
	return token
}

// SessionsOf is how many sessions this user has, which is what the conformance
// suite asks after a password change. The real service answers the same
// question with a count on the table.
func (f *Fake) SessionsOf(user uuid.UUID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.sessions {
		if s.UserID == user {
			n++
		}
	}
	return n
}
