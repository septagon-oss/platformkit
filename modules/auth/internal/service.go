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
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Delivery is how a link with a secret in it leaves this module: the mailer
// that carries it, the lookup that turns a path into the recipient's own host,
// and whether that host is reached over https.
//
// The three are one decision and travel together. A composition that wires no
// mailer issues no token either — a link nobody is sent is a live credential in
// a table for an hour, for nothing — and every route still answers as though it
// had.
type Delivery struct {
	Mailer contracts.Mailer
	Hosts  contracts.Hosts
	Secure bool
}

// Service is signing in and what a role may do.
type Service struct {
	users   contracts.Users
	notify  contracts.Notifier
	mail    Delivery
	limiter *contracts.Limiter

	// catalogue is every permission the application defines, handed over by the
	// kernel when this module's routes are registered. The hourly sweep reads
	// it from another goroutine an hour later, so it is guarded.
	mu        sync.RWMutex
	catalogue []tenancy.Grant

	// factorKey seals and opens a factor secret. EnableFactors sets it, and a
	// service that was never handed one writes no factor at all: an empty key is
	// not a weaker envelope, it is the absence of one. Guarded by the mutex above
	// for the reason catalogue is — module.go sets it from the wiring path while
	// requests are already reading it.
	factorKey []byte

	// passkeyName is the relying party's fallback display name, shown by a
	// platform when the tenant has no name of its own. Whether the usernameless
	// door is open at all is not held here: it is the tenant's own row, read per
	// request. The name is set once from the wiring path while requests are
	// already reading it, so the mutex above guards it.
	passkeyName string
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
func NewService(users contracts.Users, notify contracts.Notifier, mail Delivery) *Service {
	// The counters every replica shares. httpx.ConnFrom is where they find the
	// pool: this module is constructed before kit/app opens one, and every
	// caller below is inside a request that carries it.
	return &Service{users: users, notify: notify, mail: mail,
		limiter: contracts.NewLimiter(limit.Postgres(httpx.ConnFrom))}
}

// Precheck is the limiter's verdict, read from the shared counters. See
// contracts.Service.
func (s *Service) Precheck(ctx context.Context, email, ip string) contracts.Verdict {
	return s.limiter.Check(ctx, email, ip)
}

// MayAsk counts one forgotten-password request from an address. See
// contracts.Service.
func (s *Service) MayAsk(ctx context.Context, ip string) bool { return s.limiter.Requested(ctx, ip) }

// MayRedeem counts one reset-token redemption from an address. See
// contracts.Service.
func (s *Service) MayRedeem(ctx context.Context, ip string) bool {
	return s.limiter.Redeemed(ctx, ip)
}

var _ contracts.Service = (*Service)(nil)

// Login verifies a password and opens a session.
//
// The three refusals — locked out, no such address, wrong password — cost the
// same and, apart from the lockout, say the same. An address nobody has still
// pays for one argon2id hash (usercontracts.EqualWork), because the difference
// between "no such account" and "wrong password" is otherwise a stopwatch.
func (s *Service) Login(ctx context.Context, tx db.Tx[db.Tenant], email, password string, from contracts.Client) (*contracts.Session, *contracts.Identity, error) {
	// Refuse only. The other verdict a caller can get is Delay, and the pause
	// it earns is taken before the transaction was opened, by whoever asked
	// Precheck — a two-second sleep here holds one of sixteen pool connections,
	// and twenty-four of them at once took a replica to twenty-nine seconds of
	// latency for every other request. See contracts.Service.Precheck.
	if s.limiter.Check(ctx, email, from.IP) == contracts.Refuse {
		// Once per account per address per window. The refusal happens before
		// anything is checked, so a script against a locked account produces
		// one of these a millisecond; the first says the account is under
		// attack and the nine hundredth says it still is.
		if s.limiter.Noted(ctx, email, from.IP) {
			s.recordFailure(ctx, email, from, true)
		}
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
	s.limiter.Succeeded(ctx, email)
	// The password arrived. Whether that is enough is a fact about the account
	// rather than about this request: a person who enrolled a second factor is
	// not signed in by the first half of their own sign-in, and opening a
	// session here and taking it away afterwards would be a window in which a
	// stolen password was a stolen account for as long as the code took to type.
	//
	// Nothing is published from this branch. auth.login_failed is the trail's
	// record of an attempt that did not get in with what it had; this one did,
	// and the answer it got is a step rather than a failure. auth.logged_in is
	// not published either, and that is the point: there is no session, so the
	// trail that says "signed in" would be describing a row that does not exist.
	required, err := s.factorEnrolled(ctx, tx, user.ID)
	if err != nil {
		return nil, nil, err
	}
	if required {
		// The half that arrived is written down, and it is what makes the other
		// half answerable: the challenge leg spends this before it reads a code.
		// Nothing here could be called a sign-in — no session, no identity, no
		// event — and a person who gives up now leaves a row that stops being
		// anything at all five minutes after this password was typed.
		s.markFirstFactorProved(ctx, user.ID)
		return nil, nil, contracts.ErrFactorRequired
	}
	session, identity, err := s.open(ctx, tx, user, from, "password")
	if err != nil {
		return nil, nil, err
	}
	return session, identity, nil
}

// Open creates a session for a user somebody else has already recognised. The
// OIDC callback is its caller.
//
// It asks the account the same question Login asks, and asks it for the same
// reason. Whether what the caller proved is enough is a fact about the account,
// not about which door it came to: an identity provider that confirmed a mailbox
// proved one thing about this person, and the thing it proved is the thing their
// password also proved. A person who enrolled a second factor is not signed in
// by the first half of a sign-in, whichever half the first half was, and a leg
// that opened the session and left the factor for later would be a window in
// which a session stolen at the provider was a stolen account. Nothing is written or
// published on this branch, for the reason Login publishes nothing: there is no
// session, so a trail that said "signed in" would describe a row that does not
// exist. The person finishes at `/challenge/verify`, which spends the code and
// opens the session the provider had already earned.
//
// The question is asked unconditionally because nothing exists that could answer
// it otherwise: no column of 000030_tenant_oidc and no field of
// contracts.OIDCProvider says "this tenant's provider is trusted to have asked",
// and reading a permission nobody wrote would be inventing it. The declaration
// the brief's "where the tenant allows" asks for is named as unbuilt in the
// module's README; until somebody writes it, the account decides at both doors.
func (s *Service) Open(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, from contracts.Client) (*contracts.Session, *contracts.Identity, error) {
	user, err := s.users.Get(ctx, tx, id)
	if err != nil {
		return nil, nil, err
	}
	if user.Status != usercontracts.StatusActive {
		return nil, nil, contracts.ErrCredentials
	}
	required, err := s.factorEnrolled(ctx, tx, user.ID)
	if err != nil {
		return nil, nil, err
	}
	if required {
		// The provider confirmed the address, which is the half a password also
		// proves, so the same window is marked here as at /login: the person is
		// sent on to /challenge/verify with something to spend there, and a leg
		// that refused without leaving one would be a door that cannot be opened
		// from the outside at all.
		s.markFirstFactorProved(ctx, user.ID)
		return nil, nil, contracts.ErrFactorRequired
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
		UserID: user.ID, SessionRef: contracts.SessionRef(session.ID), Method: method, IP: session.IP, At: at,
	})
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
		s.forget(ctx, hash)
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

// detachedWriteBudget bounds auth writes that must survive a refused request.
// A busy or unreachable database must not keep that request open indefinitely.
const detachedWriteBudget = 2 * time.Second

// forget deletes one expired session, in a transaction of its own.
//
// Its own, and that is the correction rather than a flourish. This runs inside
// the request's transaction, and a request whose caller was not recognised
// answers 401 or 403 — a status kit/httpx rolls back — so the DELETE went back
// with it and the row survived every visit it refused. Detached, the row goes
// on the first refusal instead of waiting up to an hour for the sweep, which is
// what the comment here used to claim and did not do.
//
// A failure is logged and changes nothing: the session is expired either way
// and the hourly purge is still behind this. Nothing outside a request has a
// connection to detach onto, and a caller with none simply leaves the row.
func (s *Service) forget(ctx context.Context, hash contracts.Digest) {
	conn, ok := httpx.ConnFrom(ctx)
	if !ok {
		return
	}
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), detachedWriteBudget)
	defer cancel()
	err := db.Run(detached, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Where("id_hash = ?", hash).Delete(&contracts.Session{}).Error
	})
	if err != nil {
		slog.ErrorContext(ctx, "auth: could not end an expired session", "error", err)
	}
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

// RevokeSessions ends every session this user has but one, and publishes one
// auth.session_revoked per machine that left.
//
// It is the second half of every password change: the point of setting a new
// password is that the old one stops working, and a session opened with the old
// one is the old one still working. except keeps the session the person is
// asking from, so changing a password does not sign you out of the page you
// changed it on; the nil UUID keeps none, which is also what RevokeAllSessions
// asks for. Both are the same statement, in the same transaction as whatever
// asked for them, and both leave the revocations on the trail.
func (s *Service) RevokeSessions(ctx context.Context, tx db.Tx[db.Tenant], userID, except uuid.UUID) error {
	_, err := s.revoke(ctx, tx, userID, except)
	return err
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
	verifications := tx.DB().Exec(
		"DELETE FROM verification_tokens WHERE token_hash IN ("+
			"SELECT token_hash FROM verification_tokens WHERE expires_at <= clock_timestamp() LIMIT ?)", purgeBatch)
	if verifications.Error != nil {
		return 0, fmt.Errorf("auth: purge email verifications: %w", verifications.Error)
	}
	// A spent recovery code has answered its one question. Keeping the row would
	// be keeping a list of which of a person's codes still work — the opposite of
	// what the hash was for.
	codes := tx.DB().Exec(
		"DELETE FROM recovery_codes WHERE id IN ("+
			"SELECT id FROM recovery_codes WHERE used_at IS NOT NULL LIMIT ?)", purgeBatch)
	if codes.Error != nil {
		return 0, fmt.Errorf("auth: purge the spent recovery codes: %w", codes.Error)
	}
	// A key that has been dead for a month is no longer a fact anybody reads: the
	// revocation is on the trail with its name, its scope and its last use, which
	// is what an incident review asks. The month is the difference between
	// answering that question from a row and keeping a credential — a hash, but a
	// row nonetheless — forever because deleting it was a little inconvenient.
	keys := tx.DB().Exec(
		"DELETE FROM api_tokens WHERE id IN ("+
			"SELECT id FROM api_tokens WHERE expires_at <= now() - ?::interval"+
			" OR revoked_at <= now() - ?::interval LIMIT ?)", retired, retired, purgeBatch)
	if keys.Error != nil {
		return 0, fmt.Errorf("auth: purge the retired api tokens: %w", keys.Error)
	}
	// A first-factor proof that has aged out is a row that can no longer be
	// spent: the challenge checks expires_at against the clock, so this is table
	// hygiene, and the five-minute window means a tenant's whole row count is
	// the number of people mid-sign-in.
	proofs := tx.DB().Exec(
		"DELETE FROM first_factor_proofs WHERE user_id IN ("+
			"SELECT user_id FROM first_factor_proofs WHERE expires_at <= now() LIMIT ?)", purgeBatch)
	if proofs.Error != nil {
		return 0, fmt.Errorf("auth: purge the spent first-factor proofs: %w", proofs.Error)
	}
	// A passkey prompt nobody answered is a nonce with a two-minute expiry and a
	// stranger's request behind it: the begin legs are public, they each write one
	// row, and the answer that would have deleted it may never arrive. The window is
	// short enough that a tenant's live count is the number of people mid-ceremony,
	// so this is the ordinary case rather than a sweep of anybody's history.
	ceremonies := tx.DB().Exec(
		"DELETE FROM passkey_challenges WHERE id IN ("+
			"SELECT id FROM passkey_challenges WHERE expires_at <= now() LIMIT ?)", purgeBatch)
	if ceremonies.Error != nil {
		return 0, fmt.Errorf("auth: purge the unanswered passkey prompts: %w", ceremonies.Error)
	}
	return sessions.RowsAffected + tokens.RowsAffected + verifications.RowsAffected +
		codes.RowsAffected + keys.RowsAffected + proofs.RowsAffected + ceremonies.RowsAffected, nil
}

// The purge's two constants. A thousand rows per transaction, for the reason
// modules/audit's retention sweep uses the same number: one DELETE over a busy
// tenant's history is a lock held for as long as it takes. maxAge is
// SessionMaxLifetime as Postgres spells an interval, so the cap is one number
// and the database applies it.
var (
	purgeBatch = 1000
	maxAge     = fmt.Sprintf("%d hours", int(contracts.SessionMaxLifetime/time.Hour))
	// retired is how long a dead API key stays a row: long enough for whoever
	// noticed to ask about it, short enough that the table is not a museum.
	retired = fmt.Sprintf("%d hours", int(contracts.APITokenRetention/time.Hour))
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
	// The event names the session by its ref, never by the id the caller holds.
	return events.Publish(ctx, tx, contracts.EventLoggedOut, contracts.LoggedOut{
		UserID: session.UserID, SessionRef: contracts.SessionRef(id), At: db.Now(),
	})
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
	s.limiter.Failed(ctx, email, from.IP)
	s.recordFailure(ctx, email, from, s.limiter.Check(ctx, email, from.IP) == contracts.Refuse)
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
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), detachedWriteBudget)
	defer cancel()
	err := db.Run(detached, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
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
