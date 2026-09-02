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
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
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
	// SessionTouch is the throttle. Without it every request is a write, and a
	// read-only page load would take a row lock on the session it read.
	SessionTouch = 5 * time.Minute
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

// Session is a signed-in browser. The id is the credential: it is what the
// platformkit_session cookie carries.
type Session struct {
	ID         uuid.UUID `json:"id"`
	TenantID   uuid.UUID `json:"-" gorm:"column:tenant_id"`
	UserID     uuid.UUID `json:"userId"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	UserAgent  string    `json:"-"`
	IP         string    `json:"-"`
}

// TableName pins the table, so the entity and migrations/000008 agree.
func (Session) TableName() string { return "sessions" }

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
// address belongs to, and to read back the person a session belongs to.
//
// It is declared here, narrower than the user module's own Service, because a
// consumer depends on the capability it uses rather than on everything the
// provider offers — which is also what makes a test's stand-in three lines.
type Users interface {
	ByEmail(ctx context.Context, tx db.Tx[db.Tenant], email string) (*usercontracts.User, error)
	Get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*usercontracts.User, error)
}

// Service is signing in, signing out, recognising a session, and resolving what
// a set of roles may do.
//
// Every command takes the caller's transaction rather than opening one, so the
// session row and the event that describes it commit together — with one
// deliberate exception, described on Login.
type Service interface {
	// Login verifies a password and opens a session. It answers ErrCredentials
	// for a wrong password, an unknown address, a user who is not active and a
	// user who has no password, and ErrTooManyAttempts once an address has
	// failed too often.
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
	Authenticate(ctx context.Context, tx db.Tx[db.Tenant], r *http.Request) (httpx.Principal, bool, error)
}
