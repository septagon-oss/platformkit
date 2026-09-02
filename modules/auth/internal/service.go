// Package internal is every implementation of the auth module. Nothing outside
// modules/auth can import it, which is the compiler enforcing idea 3.
package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Service is signing in and what a role may do.
type Service struct {
	users   contracts.Users
	limiter *contracts.Limiter
}

// NewService returns the auth service. module.go constructs it.
func NewService(users contracts.Users) *Service {
	return &Service{users: users, limiter: contracts.NewLimiter()}
}

var _ contracts.Service = (*Service)(nil)

// Login verifies a password and opens a session.
//
// The three refusals — locked out, no such address, wrong password — cost the
// same and, apart from the lockout, say the same. An address nobody has still
// pays for one argon2id hash (usercontracts.EqualWork), because the difference
// between "no such account" and "wrong password" is otherwise a stopwatch.
func (s *Service) Login(ctx context.Context, tx db.Tx[db.Tenant], email, password string, from contracts.Client) (*contracts.Session, *contracts.Identity, error) {
	if s.limiter.Locked(email) {
		s.recordFailure(ctx, email, from, true)
		return nil, nil, contracts.ErrTooManyAttempts
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
func (s *Service) open(ctx context.Context, tx db.Tx[db.Tenant], user *usercontracts.User, from contracts.Client, method string) (*contracts.Session, *contracts.Identity, error) {
	at := db.Now()
	session := &contracts.Session{
		ID: uuid.New(), TenantID: db.TenantOf(tx).ID, UserID: user.ID,
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

// Identify is the lookup every request with a session cookie makes: one row, by
// primary key, in this tenant's transaction, joined to the user so that the
// caller's roles arrive with them and the authorizer needs no second query.
func (s *Service) Identify(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, from contracts.Client) (*contracts.Identity, error) {
	var session contracts.Session
	err := tx.DB().Where("id = ? AND expires_at > ?", id, db.Now()).Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, crud.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auth: read the session: %w", err)
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
	if err := s.slide(tx, &session, from); err != nil {
		return nil, err
	}
	return s.identify(ctx, tx, user)
}

// slide pushes the expiry out, at most once every SessionTouch. Without the
// throttle a read-only page load would take a row lock on the session it read,
// and two tabs of the same person would wait on each other.
func (s *Service) slide(tx db.Tx[db.Tenant], session *contracts.Session, from contracts.Client) error {
	at := db.Now()
	if at.Sub(session.LastSeenAt) < contracts.SessionTouch {
		return nil
	}
	session.LastSeenAt, session.ExpiresAt = at, at.Add(contracts.SessionLifetime)
	session.IP = clip(from.IP, 60)
	err := tx.DB().Model(session).
		Select("last_seen_at", "expires_at", "ip").Updates(session).Error
	if err != nil {
		return fmt.Errorf("auth: slide the session: %w", err)
	}
	return nil
}

// Logout ends a session. Ending one that is already gone is not an error: the
// caller wanted to be signed out and they are.
func (s *Service) Logout(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error {
	var session contracts.Session
	err := tx.DB().Where("id = ?", id).Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("auth: read the session: %w", err)
	}
	if err := tx.DB().Where("id = ?", id).Delete(&contracts.Session{}).Error; err != nil {
		return fmt.Errorf("auth: end the session: %w", err)
	}
	return events.Publish(ctx, tx, contracts.EventLoggedOut, contracts.LoggedOut{
		UserID: session.UserID, SessionID: session.ID, At: db.Now(),
	})
}

// SeedRoles installs the two roles a tenant starts with, in the transaction
// that created it. ON CONFLICT DO NOTHING, because a tenant that already has an
// admin role has one that somebody may have edited, and seeding is not the
// place to put it back.
func (s *Service) SeedRoles(_ context.Context, tx db.Tx[db.System], tenantID uuid.UUID) error {
	for name, permissions := range map[string][]string{
		contracts.RoleAdmin:  {contracts.Wildcard},
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
	locked := s.limiter.Failed(email)
	s.recordFailure(ctx, email, from, locked)
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
