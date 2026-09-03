// Package contracts is everything another module, an app or a test may know
// about signing in: the session, the identity a caller has, the errors a login
// can fail with, and the Service interface. The implementation is in
// ../internal.
//
// # A session is a tenant's row
//
// The sessions table is an ordinary tenant-owned table, and that is the design
// rather than an accident of it. A session is looked up inside the transaction
// of the tenant the request host resolved to, so a session issued on one
// customer's host and presented on another's is a row the policy does not
// return: the caller is anonymous, and no Go code had to compare two tenant ids
// to make it so. The check that used to live in the kernel is a deleted branch.
package contracts

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// The two named roles every tenant starts with. admin holds Wildcard; member
// holds nothing at all and exists so that "a person who is in this tenant and
// may do nothing in particular" has a name to be granted.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
	// Wildcard is the permission that grants every permission. It is one string
	// rather than a boolean column on the role, so "may do everything" is
	// expressed in the same list as everything else and a role screen has one
	// thing to render.
	Wildcard = "*"
)

// SessionLifetime is how long a session lives from its last use. It slides:
// every request that finds the session pushes the expiry out again, at most
// once every SessionTouch, so a person who works every day is not signed out
// and a laptop left in a drawer is.
const (
	SessionLifetime = 30 * 24 * time.Hour
	// SessionMaxLifetime is the cap the slide cannot pass: ninety days from
	// created_at, whatever the last use was.
	//
	// A sliding expiry on its own is not an expiry. A browser somebody uses
	// every day keeps one session for as long as the machine lasts, and a
	// cookie stolen from it works for as long as the thief keeps using it —
	// there is no moment at which anything has to be proved again. Ninety days
	// is that moment: past it the session is refused and the row deleted,
	// whichever of the two limits it crossed.
	SessionMaxLifetime = 90 * 24 * time.Hour
	// SessionTouch is the throttle. Without it every request is a write, and a
	// read-only page load would take a row lock on the session it read.
	SessionTouch = 5 * time.Minute
	// TokenLifetime is how long a set-password or reset link works. Long enough
	// to find the mail, short enough that one left in an inbox is not an account.
	TokenLifetime = 60 * time.Minute
	// ResetInterval is how long a person waits between links, and it is the cap
	// on outstanding notices per recipient.
	//
	// The single-pending rule already means asking twice leaves one live link;
	// this is what stops asking twice being two mails. Without it a public
	// route and a known address are somebody else's inbox filled by a stranger.
	// Five minutes is longer than somebody clicks twice and much shorter than
	// TokenLifetime, so a person who really did lose the first mail asks again
	// and gets one.
	ResetInterval = 5 * time.Minute
)

// The two failures a caller can act on. Everything else from Login is an outage.
var (
	// ErrCredentials is the only answer a failed password login ever gets. The
	// address being unknown and the password being wrong are the same error, on
	// purpose: telling them apart is an account enumeration oracle, and the
	// implementation spends the same time on both.
	ErrCredentials = errors.New("auth: those credentials are not right")

	// ErrTooManyAttempts is the lockout. It is distinguishable from
	// ErrCredentials, which is a deliberate trade: it tells an attacker they
	// have been noticed, and it tells the person whose account is being
	// attacked why their own correct password is not working.
	ErrTooManyAttempts = errors.New("auth: too many failed attempts; wait and try again")
)

// Session is a signed-in browser.
//
// The id is the credential and it is not stored: the row is keyed by IDHash,
// and ID is set only on the value Login and Open hand back, for the one caller
// that needs it — the handler writing the cookie. Anybody who can read this
// table holds a list of hashes rather than a set of live sessions, which is the
// difference between a leaked backup being an incident and being a breach.
type Session struct {
	// ID is the random value the cookie carries. gorm:"-", so it is in no
	// INSERT and no SELECT: there is no column for it.
	ID uuid.UUID `json:"id" gorm:"-"`
	// IDHash is sha256(ID), and the primary key.
	IDHash     Digest    `json:"-" gorm:"column:id_hash;primaryKey"`
	TenantID   uuid.UUID `json:"-" gorm:"column:tenant_id"`
	UserID     uuid.UUID `json:"userId"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	// UserAgent and IP are recorded once, when the session is opened, and never
	// touched again. They are not a credential and nothing is checked against
	// them: they are what a person needs to recognise a session in a list and
	// say "that was not me". Rewriting them on every use would erase exactly
	// that — the address a session was opened from is the interesting one, and
	// the address it was last used from is whatever proxy answered last.
	UserAgent string `json:"-"`
	IP        string `json:"-"`
}

// TableName pins the table, so the entity and migrations/000013 agree.
func (Session) TableName() string { return "sessions" }

// Hash is how a credential this module issues becomes a row key: sha256, no
// work factor, no salt.
//
// That is right here and would be wrong for a password. The input is not
// something a person chose — a session id is 128 bits of crypto/rand and a
// token is 256 — so there is no dictionary to run against it and nothing a slow
// hash would buy; what is wanted is a one-way function fast enough to run on
// every request. No salt, because the lookup is by the hash itself: a salted
// hash cannot be an index probe, and per-row salting protects against a
// dictionary that does not exist here.
func Hash(credential string) Digest {
	sum := sha256.Sum256([]byte(credential))
	return sum[:]
}

// Digest is a hash as a database value: a bytea column, and one bind parameter.
//
// It is a named type with a Valuer rather than a plain []byte because GORM
// expands a slice argument into a comma-separated list — which is right for
// `IN (?)` and wrong for every hash here, where it turns one parameter into
// thirty-two. Saying so in the type means no query has to remember.
type Digest []byte

func (d Digest) Value() (driver.Value, error) { return []byte(d), nil }

func (d *Digest) Scan(src any) error {
	b, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("auth: a digest is bytea, not %T", src)
	}
	*d = slices.Clone(b)
	return nil
}

// Role is a name and what it grants, in one tenant. It is the row Permissions
// reads and the roles routes write.
type Role struct {
	TenantID  uuid.UUID   `json:"-" gorm:"column:tenant_id;primaryKey"`
	Name      string      `json:"name" gorm:"primaryKey" doc:"The role's name, a lower-case identifier" example:"editor"`
	Grants    Permissions `json:"permissions" gorm:"column:permissions;type:text[]" doc:"The permissions this role grants"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

// TableName pins the table, so the struct and migrations/000008 agree.
func (Role) TableName() string { return "roles" }

// Permissions is one role's permission list, one text[] column. It is a named
// type rather than []string so that the array codec is written once, the way
// user/contracts.Roles is; both delegate to lib/pq, which is already linked in.
type Permissions []string

func (p Permissions) Value() (driver.Value, error) { return pq.StringArray(p).Value() }
func (p *Permissions) Scan(src any) error          { return (*pq.StringArray)(p).Scan(src) }

// Identity is who the caller is, as a response body says it. It is what login
// returns and what GET /api/v1/auth/me answers with, so a browser that has just
// signed in needs no second request to render a navigation bar.
type Identity struct {
	UserID      uuid.UUID `json:"userId" format:"uuid"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName,omitempty"`
	Roles       []string  `json:"roles"`
	// Permissions is what those roles grant, resolved once. A screen decides
	// what to draw from this; the server decides what to allow from the same
	// rows, so the two cannot drift.
	Permissions []string `json:"permissions"`
}

// Client is what a session records about where it was opened. It is not a
// credential and nothing is checked against it: it is what a person needs in
// order to recognise a session in a list and say "that was not me".
type Client struct {
	UserAgent string
	IP        string
}

// Users is what this module needs of the user module: to find the person an
// address belongs to, to read back the person a session belongs to, and to
// store a password once somebody has proved they may choose one.
//
// It is declared here, narrower than the user module's own Service, because a
// consumer depends on the capability it uses rather than on everything the
// provider offers — which is also what makes a test's stand-in three lines.
// SetPassword is the user module's, hash and event and all: this module decides
// who may change a password and the user module owns what a password is.
type Users interface {
	ByEmail(ctx context.Context, tx db.Tx[db.Tenant], email string) (*usercontracts.User, error)
	Get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*usercontracts.User, error)
	SetPassword(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, password string) error
}

// Notifier is what this module needs of the notification module to tell
// somebody something inside the application: a row with a title, a body and a
// path, which a bell shows and a page renders.
//
// It never carries the secret. The notice raised beside a reset link points at
// ResetPath and nothing more, because a notification row is an ordinary row —
// listed by a route, copied into no audit trail but readable by anybody who can
// read the table — and a token in it is a live credential sitting in one.
type Notifier interface {
	Notify(ctx context.Context, tx db.Tx[db.Tenant], n notificationcontracts.Notice) (*notificationcontracts.Notification, error)
}

// Mailer and Hosts are the notification module's own two interfaces, named here
// so that this module's Deps reads as one list of capabilities rather than as a
// mixture of two packages'.
//
// This module sends one kind of mail itself rather than asking the notification
// module to, and that is the whole of why it holds a Mailer. Everything else
// this application mails goes out of the notification worker, which reads the
// row back and renders it — so whatever is in the message is in a row. A
// set-password link must not be in a row, in an outbox payload or in the audit
// trail, and the only way to have it in none of them is for the process that
// mints the token to hand it straight to the mail server. See Service.Reissue.
type (
	Mailer = notificationcontracts.Mailer
	Hosts  = notificationcontracts.HostLookup
)

// Service is signing in, signing out, recognising a session, and resolving what
// a set of roles may do.
//
// Every command takes the caller's transaction rather than opening one, so the
// session row and the event that describes it commit together — with one
// deliberate exception, described on Login.
type Service interface {
	// Precheck is what the limiter says about an attempt on this address from
	// this one, before anything has been opened. It reads memory and touches no
	// database.
	//
	// It is a method of its own because one of the three answers is "wait two
	// seconds", and two seconds is a long time to be holding a database
	// connection. The caller asks, sleeps if it is told to, and opens its
	// transaction afterwards; Login applies the refusal itself, so a caller
	// that never asks is refused rather than let in — what it loses is the
	// delay, which is a slowdown and not a gate.
	Precheck(email, ip string) Verdict

	// MayAsk counts one forgotten-password request from an address and reports
	// whether it is within ResetRequests for the window. It is the public
	// route's own limit, and it counts the address asking rather than the
	// address asked about.
	MayAsk(ip string) bool

	// Login verifies a password and opens a session. It answers ErrCredentials
	// for a wrong password, an unknown address, a user who is not active and a
	// user who has no password, and ErrTooManyAttempts once an address has
	// failed too often.
	//
	// It does not sleep. The soft delay a distributed attack earns belongs to
	// Precheck and to whoever calls it, for the reason given there.
	//
	// A failure publishes auth.login_failed from a transaction of its own,
	// because the request's transaction is about to be rolled back: a 401 is a
	// response of 400 or worse, and kit/httpx does not commit those. That is the
	// one place in the application where an event outlives the transaction that
	// caused it, and it is right here rather than nowhere because "this account
	// was attacked and nothing recorded it" is the failure that matters.
	Login(ctx context.Context, tx db.Tx[db.Tenant], email, password string, from Client) (*Session, *Identity, error)

	// Logout ends a session. Ending one that is already gone is not an error:
	// the caller wanted to be signed out and they are.
	Logout(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error

	// Identify is the session lookup every request makes. It answers
	// crud.ErrNotFound for a session that does not exist here, has expired, or
	// belongs to a user who is no longer active, and it slides the expiry —
	// at most once every SessionTouch.
	Identify(ctx context.Context, tx db.Tx[db.Tenant], session uuid.UUID, from Client) (*Identity, error)

	// Open creates a session for a user who has already been recognised some
	// other way. The OIDC callback is its caller: the identity provider did the
	// verifying, and what is left is the same session this module issues for a
	// password.
	Open(ctx context.Context, tx db.Tx[db.Tenant], user uuid.UUID, from Client) (*Session, *Identity, error)

	// SeedRoles installs admin and member in a tenant that has just been
	// created. It takes a system transaction because it is called from the one
	// that creates the tenant, as a hook the tenant module was handed.
	//
	// operator says the tenant is the installation's own, and it is the one
	// thing that differs: the operator's admin is granted the control plane's
	// permission by name as well as the wildcard, because a wildcard does not
	// satisfy an operator grant. Every other tenant's admin gets the wildcard
	// and nothing else, which is "everything in this tenant".
	SeedRoles(ctx context.Context, tx db.Tx[db.System], tenantID uuid.UUID, operator bool) error

	// Permissions is the union of what these roles grant in this tenant. A role
	// nobody defined grants nothing rather than failing: a user carrying a role
	// that was deleted is a user with less authority, not a broken request.
	Permissions(ctx context.Context, tx db.Tx[db.Tenant], roles []string) ([]string, error)

	// RevokeSessions ends every session this user has, except the one named.
	// The nil UUID keeps none, which is "sign me out everywhere".
	//
	// It takes the caller's transaction because it never happens on its own:
	// every caller is changing the credential those sessions were opened with,
	// and a revocation that committed apart from the change it belongs to is a
	// window in which the old password is gone and the sessions it opened are
	// not.
	RevokeSessions(ctx context.Context, tx db.Tx[db.Tenant], userID, except uuid.UUID) error

	// ChangePassword sets a new password for somebody who is signed in, having
	// checked the one they have. It ends their other sessions and keeps the one
	// they are asking from, so the person who did it stays where they are and a
	// thief who had the old password does not.
	//
	// A wrong current password is ErrCredentials. Requiring it is what stops a
	// stolen cookie from becoming a stolen account.
	ChangePassword(ctx context.Context, tx db.Tx[db.Tenant], userID, keep uuid.UUID, current, next string) error

	// Forget publishes auth.reset_requested and does nothing else.
	//
	// Not the lookup, not the token, not the mail: all three are Reissue's, in
	// the worker. The route this sits behind is public and has to cost the same
	// whether or not anybody has the address — an endpoint that said "no such
	// address" would be an account enumeration oracle, and one that merely took
	// half as long to say nothing is the same oracle with a stopwatch. So the
	// request path is one INSERT into the outbox and is identical either way.
	Forget(ctx context.Context, tx db.Tx[db.Tenant], email string) error

	// Reissue is the other half, and it runs in the worker: look the address
	// up, and if somebody active has it, mint a token and mail them the link.
	//
	// It answers nil for an address nobody has, for a deactivated user, for a
	// composition that wired no mailer, and for a person who was sent one
	// recently — every one of those is "no mail" and none of them is a failure
	// the outbox should retry.
	Reissue(ctx context.Context, tx db.Tx[db.Tenant], email string) error

	// Offer issues a set-password token for somebody who has just been invited
	// and mails the link. It is what the user.invited subscription does, and it
	// is the same token Reissue issues: an invitation and a reset differ in the
	// message, not in the mechanism.
	Offer(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) error

	// Reset consumes a token, sets the password and ends every session that
	// user has — including the one asking, because whoever is resetting a
	// password is not relying on a session and whoever else held one may be the
	// reason it is being reset. It publishes auth.password_reset.
	//
	// A token that is unknown, spent or expired is ErrCredentials, and the
	// three are one answer for the reason Login's three are.
	Reset(ctx context.Context, tx db.Tx[db.Tenant], token, password string) error

	// Roles is every role in this tenant, by name.
	Roles(ctx context.Context, tx db.Tx[db.Tenant]) ([]*Role, error)

	// SetRole writes what a role grants, creating it if it is new.
	//
	// declared is every permission the application defines, which the caller
	// gets from the kernel: a role naming one nothing defines is a grant that
	// can never be exercised and reads, to whoever wrote it, as one that can.
	// It is a parameter rather than something this module knows, because the
	// list belongs to every other module's manifest and a module that knew the
	// catalogue would know its neighbours.
	//
	// An operator permission outside the operator's own tenant is refused: the
	// kernel would refuse the request anyway, so a role that named one would be
	// a grant that looks like authority and is not.
	SetRole(ctx context.Context, tx db.Tx[db.Tenant], name string, permissions []string, declared []tenancy.Grant) (*Role, error)

	// Purge deletes this tenant's expired sessions and spent tokens, in batches,
	// and reports how many rows went. The hourly job calls it once per tenant.
	Purge(ctx context.Context, tx db.Tx[db.Tenant]) (int64, error)
}

// Grants reports whether a set of permissions satisfies a grant. It is a
// function rather than a method so that the rule is written once and is the
// same rule in the authorizer, in a test and in a screen.
//
// The rule has two halves. An ordinary permission is granted by the wildcard or
// by naming it. An operator permission is granted only by naming it, and that
// exception is the point: Wildcard means "everything in this tenant", and the
// control plane is not in this tenant — it is every tenant. A customer's
// administrator holds the wildcard by construction (SeedRoles), so letting it
// answer for an operator permission would hand every customer the installation.
func Grants(held []string, g tenancy.Grant) bool {
	if g.Operator {
		return slices.Contains(held, g.Permission)
	}
	return slices.Contains(held, Wildcard) || slices.Contains(held, g.Permission)
}

// Auth is the whole capability main is wired with: the lifecycle in Service,
// plus the two hooks the kernel takes.
//
// The two are separate methods with separate names on purpose. Allowed is the
// kernel's question — may this caller exercise this permission, here, now — and
// it reads the principal and the transaction off the context because that is
// where kit/httpx put them. Permissions is the query underneath it, which takes
// its arguments, so a test can ask what a role grants without building a
// request.
type Auth interface {
	Service

	// Allowed is httpx.Authorizer.
	Allowed(ctx context.Context, tenant tenancy.Tenant, grant tenancy.Grant) (bool, error)

	// Authenticate is httpx.Options.Authenticate: the query that turns the
	// session cookie into the caller, inside the tenant's own transaction.
	//
	// The signature names net/http and kit/tenancy and nothing else, which is
	// the point: this package is what every consumer of this module compiles
	// against, so a mention of kit/httpx here would link huma and chi into the
	// build graph of anything that only wanted to name a Session.
	Authenticate(ctx context.Context, tx db.Tx[db.Tenant], r *http.Request) (tenancy.Principal, bool, error)
}
