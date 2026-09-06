package authtest

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/limit"
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

	limiter *contracts.Limiter

	// Notify is where the in-application notice goes. It never carries the
	// link, for the reason the real service's does not: a notification is an
	// ordinary row and a token in one is a live credential in a table.
	Notify contracts.Notifier

	// Mailer is where the link itself goes, and the only place it goes. A fake
	// with none writes no token and sends nothing, exactly as the real service
	// does.
	Mailer contracts.Mailer

	mu       sync.Mutex
	sessions map[uuid.UUID]contracts.Session
	roles    map[string]contracts.Permissions
	tokens   map[string]uuid.UUID
	// offered is when each person was last sent a link, which is what bounds
	// how many a stranger can cause. The real one reads the same fact off
	// password_tokens.created_at.
	offered   map[uuid.UUID]time.Time
	published []string
}

// NewFake returns a fake signing people in from users.
func NewFake(users contracts.Users) *Fake {
	return &Fake{
		Users:    users,
		limiter:  contracts.NewLimiter(limit.Memory()),
		sessions: map[uuid.UUID]contracts.Session{},
		roles:    map[string]contracts.Permissions{},
		offered:  map[uuid.UUID]time.Time{},
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

// Precheck mirrors internal.Service.Precheck: the limiter's verdict, from the
// same limiter the real one keeps, over kit/limit's memory store.
func (f *Fake) Precheck(ctx context.Context, email, ip string) contracts.Verdict {
	return f.limiter.Check(ctx, email, ip)
}

// MayAsk mirrors internal.Service.MayAsk.
func (f *Fake) MayAsk(ctx context.Context, ip string) bool { return f.limiter.Requested(ctx, ip) }

// MayRedeem mirrors internal.Service.MayRedeem.
func (f *Fake) MayRedeem(ctx context.Context, ip string) bool { return f.limiter.Redeemed(ctx, ip) }

// Forget mirrors internal.Service.Forget: it publishes and does nothing else,
// which is what makes the public route cost the same for an address somebody
// has and one nobody has.
func (f *Fake) Forget(_ context.Context, _ db.Tx[db.Tenant], _ string) error {
	f.record(contracts.EventResetRequested)
	return nil
}

// Reissue mirrors internal.Service.Reissue: the lookup, in the worker.
func (f *Fake) Reissue(ctx context.Context, tx db.Tx[db.Tenant], email string) error {
	user, err := f.Users.ByEmail(ctx, tx, email)
	switch {
	case isNotFound(err):
		return nil
	case err != nil:
		return err
	case user.Status == usercontracts.StatusInactive:
		return nil
	}
	return f.offer(ctx, tx, user)
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
	return f.offer(ctx, tx, user)
}

// offer keeps one pending token per person, which is the rule the unique index
// enforces in the real one, and puts the token in the mail and nowhere else.
func (f *Fake) offer(ctx context.Context, tx db.Tx[db.Tenant], user *usercontracts.User) error {
	if f.Mailer == nil {
		return nil
	}
	token := uuid.NewString()
	f.mu.Lock()
	if last, ok := f.offered[user.ID]; ok && time.Since(last) < contracts.ResetInterval {
		f.mu.Unlock()
		return nil
	}
	for t, u := range f.tokens {
		if u == user.ID {
			delete(f.tokens, t)
		}
	}
	f.tokens[token] = user.ID
	f.offered[user.ID] = time.Now()
	f.mu.Unlock()
	if f.Notify != nil {
		// The path and no query: the notice is what a person sees in the
		// application and it is not a credential.
		_, err := f.Notify.Notify(ctx, tx, notificationcontracts.Notice{
			Recipient: user.ID, Title: "Set your password", Link: "/auth/reset",
		})
		if err != nil {
			return err
		}
	}
	return f.Mailer.Send(ctx, notificationcontracts.Message{
		To: user.Email, Subject: "Set your password",
		Body: "Follow the link\n\nhttps://acme.example.com/auth/reset?token=" + token,
	})
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
	name, err := contracts.ValidRoleName(name)
	if err != nil {
		return nil, err
	}
	tenant := db.TenantOf(tx)
	want, err := contracts.CheckedPermissions(permissions, declared, tenant)
	if err != nil {
		return nil, err
	}
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
	if f.limiter.Check(ctx, email, from.IP) == contracts.Refuse {
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
		return nil, nil, f.fail(ctx, email, from.IP)
	case !user.CheckPassword(password):
		return nil, nil, f.fail(ctx, email, from.IP)
	}
	f.limiter.Succeeded(ctx, email)
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

func (f *Fake) fail(ctx context.Context, email, ip string) error {
	f.limiter.Failed(ctx, email, ip)
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

// Mailbox is a contracts.Mailer that records instead of sending, which is where
// a conformance case reads the link an implementation mailed.
//
// It is the only place a case can read it, and that is the property under test:
// the token is in the message and in no row anywhere. See
// internal.Service.offer.
type Mailbox struct {
	mu   sync.Mutex
	sent []notificationcontracts.Message
}

// Send records the message.
func (m *Mailbox) Send(_ context.Context, msg notificationcontracts.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

// Sent is every message so far, in order.
func (m *Mailbox) Sent() []notificationcontracts.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.sent)
}

// Host is a contracts.Hosts that answers one name, which is what a mailed link
// is built on.
type Host string

// PublicHost is the host, whatever tenant is asking.
func (h Host) PublicHost(context.Context, db.Tx[db.Tenant]) (string, error) { return string(h), nil }

// TokenIn is the token a set-password link carries, read out of whatever
// carried it. Both implementations spell the link the same way, which is what
// makes it a property of the contract rather than of either one.
func TokenIn(carrier string) string {
	_, token, _ := strings.Cut(carrier, "token=")
	token, _, _ = strings.Cut(token, "\n")
	return strings.TrimSpace(token)
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
