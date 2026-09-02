// Package internal is every implementation of the auth module. Nothing outside
// modules/auth can import it, which is the compiler enforcing idea 3.
package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Service is signing in and what a role may do.
type Service struct {
	users   contracts.Users
	notify  contracts.Notifier
	limiter *contracts.Limiter
	// operator are the permissions the operator's own administrator holds by
	// name. They are named and not implied because a wildcard does not satisfy
	// an operator grant; the application supplies them, because they belong to
	// the modules that declare them and this one is composed before those are.
	operator []string

	// catalogue is every permission the application defines, handed over by the
	// kernel when this module's routes are registered. The hourly sweep reads
	// it from another goroutine an hour later, so it is guarded.
	mu        sync.RWMutex
	catalogue []tenancy.Grant
}

// Declare records the permissions the composition defines. module.go calls it
// once, inside Routes, with what the kernel read off every manifest.
func (s *Service) Declare(grants []tenancy.Grant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.catalogue = slices.Clone(grants)
}

func (s *Service) declared() []tenancy.Grant {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.catalogue
}

// NewService returns the auth service. module.go constructs it.
func NewService(users contracts.Users, notify contracts.Notifier, operator []string) *Service {
	return &Service{users: users, notify: notify, limiter: contracts.NewLimiter(), operator: operator}
}

var _ contracts.Service = (*Service)(nil)

// Login verifies a password and opens a session.
//
// The three refusals — locked out, no such address, wrong password — cost the
// same and, apart from the lockout, say the same. An address nobody has still
// pays for one argon2id hash (usercontracts.EqualWork), because the difference
// between "no such account" and "wrong password" is otherwise a stopwatch.
func (s *Service) Login(ctx context.Context, tx db.Tx[db.Tenant], email, password string, from contracts.Client) (*contracts.Session, *contracts.Identity, error) {
	switch s.limiter.Check(email, from.IP) {
	case contracts.Refuse:
		s.recordFailure(ctx, email, from, true)
		return nil, nil, contracts.ErrTooManyAttempts
	case contracts.Delay:
		// The account is under attack from many places, or this address is
		// working through a list of accounts. Refusing either would be doing
		// the attacker's work: the first is how somebody locks a person out of
		// their own account, and the second is how an office behind one NAT
		// loses its morning. A pause costs the person two seconds and costs the
		// attack its rate. See contracts.Limiter.
		s.sleep(ctx, contracts.SoftDelay)
	}
	user, err := s.users.ByEmail(ctx, tx, email)
	switch {
	case errors.Is(err, crud.ErrNotFound):
		usercontracts.EqualWork(password)
		return nil, nil, s.fail(ctx, email, from)
	case err != nil:
		return nil, nil, err
	case !user.CanSignIn():
		// An invited user with no password and a deactivated one are both
		// refused, and both pay for the hash: which of the three it was is not
		// something a stranger gets to measure either.
		usercontracts.EqualWork(password)
		return nil, nil, s.fail(ctx, email, from)
	case !user.CheckPassword(password):
		return nil, nil, s.fail(ctx, email, from)
	}
	s.limiter.Succeeded(email)
	session, identity, err := s.open(ctx, tx, user, from, "password")
	if err != nil {
		return nil, nil, err
	}
	return session, identity, nil
}

// Open creates a session for a user somebody else has already recognised. The
// OIDC callback is its caller.
func (s *Service) Open(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, from contracts.Client) (*contracts.Session, *contracts.Identity, error) {
	user, err := s.users.Get(ctx, tx, id)
	if err != nil {
		return nil, nil, err
	}
	if user.Status != usercontracts.StatusActive {
		return nil, nil, contracts.ErrCredentials
	}
	return s.open(ctx, tx, user, from, "oidc")
}

// open writes the session row and its event in the caller's transaction.
//
// The id goes to the caller and the hash goes to the table: what is stored is
// not what the cookie carries, so a copy of this table is a list of hashes
// rather than a set of live sessions. See contracts.Session.
func (s *Service) open(ctx context.Context, tx db.Tx[db.Tenant], user *usercontracts.User, from contracts.Client, method string) (*contracts.Session, *contracts.Identity, error) {
	at := db.Now()
	id := uuid.New()
	session := &contracts.Session{
		ID: id, IDHash: contracts.Hash(id.String()), TenantID: db.TenantOf(tx).ID, UserID: user.ID,
		CreatedAt: at, ExpiresAt: at.Add(contracts.SessionLifetime), LastSeenAt: at,
		UserAgent: clip(from.UserAgent, 400), IP: clip(from.IP, 60),
	}
	if err := tx.DB().Create(session).Error; err != nil {
		return nil, nil, fmt.Errorf("auth: open a session: %w", err)
	}
	identity, err := s.identify(ctx, tx, user)
	if err != nil {
		return nil, nil, err
	}
	return session, identity, events.Publish(ctx, tx, contracts.EventLoggedIn, contracts.LoggedIn{
		UserID: user.ID, SessionID: session.ID, Method: method, IP: session.IP, At: at,
	})
}

// sleep is the soft delay, interruptible: a shutdown must not wait two seconds
// per request in flight.
func (s *Service) sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// Identify is the lookup every request with a session cookie makes: one row, by
// primary key, in this tenant's transaction, joined to the user so that the
// caller's roles arrive with them and the authorizer needs no second query.
func (s *Service) Identify(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, from contracts.Client) (*contracts.Identity, error) {
	hash := contracts.Hash(id.String())
	var session contracts.Session
	err := tx.DB().Where("id_hash = ?", hash).Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, crud.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auth: read the session: %w", err)
	}
	// Both limits, checked here rather than in the WHERE clause, so that a
	// session that has passed one is deleted on the way past instead of waiting
	// for the hourly purge. A row that is refused and left is a row somebody
	// with a copy of the table can still study.
	at := db.Now()
	if !session.ExpiresAt.After(at) || !session.CreatedAt.Add(contracts.SessionMaxLifetime).After(at) {
		if err := tx.DB().Where("id_hash = ?", hash).Delete(&contracts.Session{}).Error; err != nil {
			return nil, fmt.Errorf("auth: end an expired session: %w", err)
		}
		return nil, crud.ErrNotFound
	}
	user, err := s.users.Get(ctx, tx, session.UserID)
	if err != nil {
		return nil, err
	}
	if user.Status != usercontracts.StatusActive {
		// Deactivating somebody ends their sessions without anybody walking a
		// list of them, which is what makes "log this person out everywhere"
		// one column and not a fan-out.
		return nil, crud.ErrNotFound
	}
	if err := s.slide(tx, &session); err != nil {
		return nil, err
	}
	return s.identify(ctx, tx, user)
}

// slide pushes the expiry out, at most once every SessionTouch. Without the
// throttle a read-only page load would take a row lock on the session it read,
// and two tabs of the same person would wait on each other.
//
// It never passes the absolute cap, so the last request before ninety days
// leaves a session that expires at ninety days rather than one that expires
// thirty days later and is refused anyway. And it writes no address: the
// address a session was opened from is what a person recognises in a list, and
// overwriting it with whatever proxy answered last erases the only useful
// thing about it.
func (s *Service) slide(tx db.Tx[db.Tenant], session *contracts.Session) error {
	at := db.Now()
	if at.Sub(session.LastSeenAt) < contracts.SessionTouch {
		return nil
	}
	session.LastSeenAt = at
	session.ExpiresAt = at.Add(contracts.SessionLifetime)
	if cap := session.CreatedAt.Add(contracts.SessionMaxLifetime); session.ExpiresAt.After(cap) {
		session.ExpiresAt = cap
	}
	err := tx.DB().Model(session).Where("id_hash = ?", session.IDHash).
		Select("last_seen_at", "expires_at").Updates(session).Error
	if err != nil {
		return fmt.Errorf("auth: slide the session: %w", err)
	}
	return nil
}

// RevokeSessions ends every session this user has but one.
//
// It is the second half of every password change: the point of setting a new
// password is that the old one stops working, and a session opened with the old
// one is the old one still working. except keeps the session the person is
// asking from, so changing a password does not sign you out of the page you
// changed it on; the nil UUID keeps none.
func (s *Service) RevokeSessions(_ context.Context, tx db.Tx[db.Tenant], userID, except uuid.UUID) error {
	q := tx.DB().Where("user_id = ?", userID)
	if except != uuid.Nil {
		q = q.Where("id_hash <> ?", contracts.Hash(except.String()))
	}
	if err := q.Delete(&contracts.Session{}).Error; err != nil {
		return fmt.Errorf("auth: revoke the sessions of %s: %w", userID, err)
	}
	return nil
}

// Purge deletes this tenant's expired sessions and spent tokens, a batch per
// call, until fewer than a batch remain.
//
// It runs in the caller's transaction and returns a count, so the hourly job
// can open one transaction per batch: a tenant with a million dead sessions is
// a thousand short transactions rather than one long lock. Both limits are
// applied, because a session that never passed its sliding expiry has still
// passed the absolute one — that is what the cap is for — and the cutoffs are
// computed by the database, so two workers whose clocks have drifted delete the
// same rows.
func (s *Service) Purge(_ context.Context, tx db.Tx[db.Tenant]) (int64, error) {
	sessions := tx.DB().Exec(
		"DELETE FROM sessions WHERE id_hash IN ("+
			"SELECT id_hash FROM sessions WHERE expires_at <= now() OR created_at <= now() - ?::interval LIMIT ?)",
		maxAge, purgeBatch)
	if sessions.Error != nil {
		return 0, fmt.Errorf("auth: purge the sessions: %w", sessions.Error)
	}
	tokens := tx.DB().Exec(
		"DELETE FROM password_tokens WHERE token_hash IN ("+
			"SELECT token_hash FROM password_tokens WHERE expires_at <= now() LIMIT ?)", purgeBatch)
	if tokens.Error != nil {
		return 0, fmt.Errorf("auth: purge the password tokens: %w", tokens.Error)
	}
	return sessions.RowsAffected + tokens.RowsAffected, nil
}

// The purge's two constants. A thousand rows per transaction, for the reason
// modules/audit's retention sweep uses the same number: one DELETE over a busy
// tenant's history is a lock held for as long as it takes. maxAge is
// SessionMaxLifetime as Postgres spells an interval, so the cap is one number
// and the database applies it.
var (
	purgeBatch = 1000
	maxAge     = fmt.Sprintf("%d hours", int(contracts.SessionMaxLifetime/time.Hour))
)

// Logout ends a session. Ending one that is already gone is not an error: the
// caller wanted to be signed out and they are.
func (s *Service) Logout(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error {
	hash := contracts.Hash(id.String())
	var session contracts.Session
	err := tx.DB().Where("id_hash = ?", hash).Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("auth: read the session: %w", err)
	}
	if err := tx.DB().Where("id_hash = ?", hash).Delete(&contracts.Session{}).Error; err != nil {
		return fmt.Errorf("auth: end the session: %w", err)
	}
	// The id is the caller's own, not something read back: nothing stores it.
	return events.Publish(ctx, tx, contracts.EventLoggedOut, contracts.LoggedOut{
		UserID: session.UserID, SessionID: id, At: db.Now(),
	})
}

// SeedRoles installs the two roles a tenant starts with, in the transaction
// that created it. ON CONFLICT DO NOTHING, because a tenant that already has an
// admin role has one that somebody may have edited, and seeding is not the
// place to put it back.
//
// The operator's own tenant gets one permission more, named rather than
// implied: the wildcard does not satisfy an operator grant, so tenant:manage
// has to appear in the list for anybody to reach the control plane. That row is
// the whole of the installation's own authority, and it exists in exactly one
// tenant — the one the bootstrap created.
func (s *Service) SeedRoles(_ context.Context, tx db.Tx[db.System], tenantID uuid.UUID, operator bool) error {
	admin := []string{contracts.Wildcard}
	if operator {
		admin = append(admin, s.operator...)
	}
	for name, permissions := range map[string][]string{
		contracts.RoleAdmin:  admin,
		contracts.RoleMember: {},
	} {
		err := tx.DB().Exec(
			"INSERT INTO roles (tenant_id, name, permissions) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
			tenantID, name, pq.StringArray(permissions)).Error
		if err != nil {
			return fmt.Errorf("auth: seed the %s role: %w", name, err)
		}
	}
	return nil
}

// Permissions is the union of what these roles grant in this tenant: one query,
// in the request's own transaction, under the tenant's own policy.
//
// Nothing is cached. A permission cache is a window in which a revoked grant
// still works, and the query it would save is a primary-key lookup of at most a
// handful of rows on a table the size of a role list.
func (s *Service) Permissions(_ context.Context, tx db.Tx[db.Tenant], roles []string) ([]string, error) {
	if len(roles) == 0 {
		return nil, nil
	}
	var granted []pq.StringArray
	err := tx.DB().Table("roles").Where("name = ANY(?)", pq.StringArray(roles)).
		Pluck("permissions", &granted).Error
	if err != nil {
		return nil, fmt.Errorf("auth: read the roles: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	for _, list := range granted {
		for _, p := range list {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out, nil
}

// identify assembles what a response says about the caller.
func (s *Service) identify(ctx context.Context, tx db.Tx[db.Tenant], user *usercontracts.User) (*contracts.Identity, error) {
	permissions, err := s.Permissions(ctx, tx, user.Roles)
	if err != nil {
		return nil, err
	}
	return &contracts.Identity{
		UserID: user.ID, Email: user.Email, DisplayName: user.DisplayName,
		Roles: user.Roles, Permissions: permissions,
	}, nil
}

// fail records one failure and returns the one answer a failed login gets.
func (s *Service) fail(ctx context.Context, email string, from contracts.Client) error {
	s.limiter.Failed(email, from.IP)
	s.recordFailure(ctx, email, from, s.limiter.Check(email, from.IP) == contracts.Refuse)
	return contracts.ErrCredentials
}

// recordFailure publishes auth.login_failed in a transaction of its own.
//
// The request's transaction is about to be rolled back — a 401 is a response of
// 400 or worse, and kit/httpx does not commit those — so an event written in it
// would never exist. This is the one place in the application where an event is
// deliberately written outside the transaction of the thing it describes, and
// the reason is that the thing it describes is precisely the case where nothing
// else is written down.
//
// A failure to record it is logged and does not change the answer: the caller's
// password is still wrong.
func (s *Service) recordFailure(ctx context.Context, email string, from contracts.Client, locked bool) {
	conn, ok := httpx.ConnFrom(ctx)
	if !ok {
		return
	}
	err := db.Run(db.Detached(context.WithoutCancel(ctx)), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, contracts.EventLoginFailed, contracts.LoginFailed{
			Email: contracts.EmailKey(email), IP: clip(from.IP, 60), Locked: locked, At: db.Now(),
		})
	})
	if err != nil {
		slog.ErrorContext(ctx, "auth: could not record a failed login",
			"email", contracts.EmailKey(email), "error", err)
	}
}

// clip bounds a string a caller supplied before it becomes a row. A user agent
// is a label, not a payload.
func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
