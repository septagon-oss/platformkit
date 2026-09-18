// Package contracts is everything another module, an app or a test may know
// about users: the entity, the events, the permissions and the Service
// interface. The implementation is in ../internal.
//
// # One row per person per tenant
//
// There is no membership table. One tenant per host means a request is about
// one tenant before it is about anybody, so the same person working in two
// tenants is two rows: two passwords, two sets of roles, two profiles. That is
// what "tenant isolation belongs to the database" implies once it is taken
// seriously — every question about a user becomes an ordinary tenant-scoped
// query, row-level security answers it, and the join table that used to hold
// the answer, along with everything that had to agree with it, is gone.
//
// The cost is real and worth stating: a person who works for two customers
// signs in twice and changes their password twice. The alternative is a global
// identity table that no tenant's policy can protect, which is the thing this
// architecture exists not to have.
package contracts

import (
	"context"
	"database/sql/driver"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// A user is invited before password setup, pending while awaiting approval,
// unverified while awaiting mailbox confirmation, active once they can sign in,
// and inactive when that access is removed. Deleting is
// kit/crud's soft delete, which keeps the row and releases the address.
const (
	StatusInvited    = "invited"
	StatusPending    = "pending"
	StatusUnverified = "unverified"
	StatusActive     = "active"
	StatusInactive   = "inactive"
)

var statuses = []string{StatusInvited, StatusPending, StatusUnverified, StatusActive, StatusInactive}

// MinPasswordLength is the shortest password this application accepts. Length
// is the only rule: composition rules push people towards Passw0rd! and a
// twelve-character passphrase beats it, which is what every guidance since
// NIST SP 800-63B has said.
const MinPasswordLength = 12

// Roles is the set of role names a user holds, one text[] column.
//
// It is a named type rather than []string so that the array codec is written
// once. kit/crud's schema reads it as a list of strings, so it renders and it
// is a field a PATCH could name — which is why the Spec names it Immutable.
// Granting a role is Service.SetRoles, which says so in an event; it is not
// something that happens inside a bulk update of a profile.
type Roles []string

// Value writes the array. Scan reads it. Both delegate to lib/pq, which is
// already linked in — golang-migrate speaks to Postgres through it — so this is
// the array codec the program already carries rather than a second one.
func (r Roles) Value() (driver.Value, error) { return pq.StringArray(r).Value() }
func (r *Roles) Scan(src any) error          { return (*pq.StringArray)(r).Scan(src) }
func (r Roles) Has(name string) bool         { return slices.Contains(r, name) }

// roleName is the grammar of a role: a lower-case identifier, because a role
// name is a key in the auth module's roles table and reaches a policy.
var roleName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// User is one person in one tenant.
//
// The struct is the whole surface: the json tags are the API, the gorm tags are
// the table, and crud.Base contributes the id, the timestamps, the soft delete
// and the tenant column row-level security matches on.
type User struct {
	crud.Base

	// Email identifies the person within the tenant. It is stored as it was
	// given and compared without case, which is what the unique index in
	// migrations/000007 does too.
	Email string `json:"email" gorm:"type:text;not null" validate:"required" format:"email" maxLength:"320" doc:"Address this person signs in with, unique within the tenant" example:"ada@acme.example.com"`
	// DisplayName is what a screen shows. It is optional: an invitation has an
	// address and nothing else.
	DisplayName string `json:"displayName,omitempty" gorm:"type:text;not null;default:''" maxLength:"200" doc:"Name to show" example:"Ada Lovelace"`

	// Handle is what a person is *called* in this tenant: the thing they type,
	// say out loud, and see in a URL. It is not the key — users.id is, and every
	// foreign key, event subject and audit row already points at that uuid, which
	// is exactly why this field can be renamed: nothing has to follow it.
	//
	// Unique per tenant, case-insensitively, like Email: two tenants can each have
	// a `sam`. Empty means unclaimed, and an invited person who has never claimed
	// one is unclaimed rather than nameless.
	//
	// It is refused by name in module.go's Immutable list: a handle arrives by
	// command, publishes user.handle_set, and lands in the trail. A handle you
	// could PATCH alongside a display name is a handle that changed hands while
	// nobody was told — which is the same argument the module already makes about
	// roles and status, one paragraph above.
	//
	// `present:"person"` is the read axis saying what this value *is* rather than
	// how to paint it. The generated list, the description list and the native
	// shell's resource document all read the one word, and each renderer decides
	// what a person looks like in its own medium. A plain cell printing `sam` would
	// look no worse — which is exactly why the declaration is governed, and why a
	// name outside the vocabulary refuses to mount instead of quietly meaning
	// nothing.
	Handle string `json:"handle,omitempty" gorm:"type:text" maxLength:"32" ui:"present:person" doc:"Lower-case name this person answers to in this tenant, empty until claimed" example:"ada"`

	// Status is a closed set; the enum tag is what a form renders as a select
	// and what Validate refuses a value outside.
	Status string `json:"status" gorm:"type:text;not null;default:'invited'" enum:"invited,pending,unverified,active,inactive" ui:"widget:select" doc:"Lifecycle state" default:"invited" required:"false"`

	// Roles are the names of the roles this person holds. What a name grants is
	// the auth module's business, which is why this is a list of strings and
	// not a list of permissions: a role can be re-granted without touching a
	// single user row.
	Roles Roles `json:"roles" gorm:"type:text[];not null;default:'{}'" required:"false" doc:"Roles this person holds in this tenant"`

	// PasswordHash is argon2id in the PHC encoding, or empty for somebody who
	// has never set one — an invited user, or one who only signs in through an
	// identity provider. It is json:"-", so it is in no response, in no request
	// and in no generated screen.
	PasswordHash string `json:"-" gorm:"type:text"`
}

// TableName pins the table, so the entity and migrations/000007 agree.
func (User) TableName() string { return "users" }

// CanSignIn reports whether this user could authenticate with a password.
func (u *User) CanSignIn() bool { return u.Status == StatusActive && u.PasswordHash != "" }

// Validate is the entity's own check, run by kit/crud on every write whichever
// door it came through. It normalises as well as refuses: an address that
// differs only in case or in whitespace is the same mailbox, and two callers
// must not disagree about that.
func (u *User) Validate(context.Context) error {
	u.Email = strings.ToLower(strings.TrimSpace(u.Email))
	u.DisplayName = strings.TrimSpace(u.DisplayName)
	at := strings.IndexByte(u.Email, '@')
	switch {
	case u.Email == "":
		return fmt.Errorf("a user needs an email address")
	case at <= 0 || at == len(u.Email)-1 || strings.ContainsAny(u.Email, " \t\r\n"):
		return fmt.Errorf("%q is not an email address", u.Email)
	case len(u.Email) > 320:
		return fmt.Errorf("that email address is too long")
	}
	if u.Status == "" {
		u.Status = StatusInvited
	}
	// Folded the way the address is, so two callers cannot disagree about whether
	// "Ada" and "ada" are one handle — and so the unique index, which compares
	// lower(handle), is comparing what this struct believes.
	u.Handle = strings.ToLower(strings.TrimSpace(u.Handle))
	if u.Handle != "" {
		if !handleForm.MatchString(u.Handle) {
			return fmt.Errorf("handle %q is not 3 to 32 characters of lower-case letters, digits and interior . _ -", u.Handle)
		}
		if ReservedHandle(u.Handle) {
			return fmt.Errorf("handle %q is reserved: it names the platform or a role, not a person", u.Handle)
		}
	}
	if !slices.Contains(statuses, u.Status) {
		return fmt.Errorf("status %q is not a lifecycle state", u.Status)
	}
	for _, role := range u.Roles {
		if !roleName.MatchString(role) {
			return fmt.Errorf("role %q is not a lower-case identifier", role)
		}
	}
	return nil
}

// handleForm is the shape of a claimable handle, and migrations/000025 states it
// a second time so a psql session cannot get a name the entity would refuse.
var handleForm = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,30}[a-z0-9]$`)

// reservedHandles are refused to anybody. This is a judgement call made once, in
// the open, rather than an emergent property of whoever registered first.
//
// Three kinds of name are here. Names that impersonate the platform or a
// function of it, because `support` sending a password email is a phish with an
// internal handle behind it: admin, root, owner, support, help, security, privacy,
// terms, abuse, webmaster, postmaster, moderator, staff, team, service, noadmin.
// Names that collide with the doors this API answers through, because a handle is
// also a path segment: me, user, users, people, api, admin-api, settings, account,
// accounts, billing, signin, signup, login, logout, oauth, oidc, auth, password,
// invitations, verify, health, live, metrics, admin, assets, static, docs.
// And names that break a thing downstream in a way nobody will debug twice: null,
// true, false, undefined, nan, none.
//
// It is deliberately short. A long list becomes a list people route around, and a
// tenant that wants to keep `ada` free for the person who should have it is a
// tenant-admin problem, not a kernel problem. Renames are allowed, so an early
// claim is not permanent; the trail shows who held it.
var reservedHandles = map[string]bool{}

func init() {
	for _, name := range []string{
		"admin", "administrator", "root", "owner", "moderator", "staff", "team",
		"support", "help", "security", "privacy", "terms", "abuse", "service",
		"webmaster", "postmaster",
		"me", "user", "users", "people", "person", "account", "accounts",
		"settings", "billing", "api", "auth", "oauth", "oidc", "password",
		"signin", "signup", "login", "logout", "verify", "invitations",
		"health", "live", "ready", "metrics", "assets", "static", "docs",
		"null", "true", "false", "undefined", "nan", "none",
		"platformkit", "septagon",
	} {
		reservedHandles[name] = true
	}
}

// ReservedHandle reports whether a name may not be claimed. Exported because the
// form a claim is made through has to say why it refused, and "invalid" is not an
// answer that lets anybody do anything.
func ReservedHandle(handle string) bool {
	return reservedHandles[strings.ToLower(strings.TrimSpace(handle))]
}

// ValidHandle reports whether a name is claimable in principle: right shape, not
// reserved. It does not look at the database, so it answers "can this be typed"
// and never "is this free" — ByHandle answers that, and only inside a transaction
// that can see the tenant's rows.
func ValidHandle(handle string) bool {
	handle = strings.ToLower(strings.TrimSpace(handle))
	return handleForm.MatchString(handle) && !reservedHandles[handle]
}

// Service is the user lifecycle: explicit commands generic CRUD cannot safely
// infer, including password registration and activation, plus tenant-scoped reads.
//
// Every command takes the caller's transaction rather than opening one, so the
// state change and its event commit together. The errors are kit/crud's:
// ErrNotFound, ErrInvalid, ErrConflict.
//
// Commands document their retry behavior. An unchanged idempotent command emits
// no event; verification replay conflicts rather than asserting fresh proof.
type Service interface {
	Registrations
	// Invite creates a user with no password, in status invited, and publishes
	// user.invited. Inviting an address that is already here is a conflict.
	Invite(ctx context.Context, tx db.Tx[db.Tenant], email, displayName string) (*User, error)

	// SetPassword hashes and stores a password for an invited or active user.
	// Pending, unverified and inactive users conflict; setup cannot bypass activation. The
	// same password again is still a write and still an event: a person who
	// changes their password to what it already was has still done it.
	SetPassword(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, password string) error

	// SetRoles replaces the roles this user holds. The same set again changes
	// nothing and publishes nothing.
	SetRoles(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, roles []string) (*User, error)

	// Deactivate stops the user signing in. Their sessions are somebody else's
	// business: the auth module refuses a session whose user is not active, so
	// there is no list of sessions to walk here.
	Deactivate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*User, error)

	// Get is one user of this tenant.
	Get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*User, error)

	// SetHandle claims or changes a handle. It is a command and not a PATCH for
	// the reason roles and status are: the handle is how somebody else finds this
	// person, so "who held `ada` before, and when did it move" has to have an
	// answer. It is renameable — that is the point — and nothing needs to follow
	// it, because every key, event subject and audit row names the uuid.
	//
	// The same handle again changes nothing and publishes nothing. A handle
	// somebody else holds is a conflict naming the rule and not the holder: the
	// answer must not confirm that a given person is in this tenant to somebody
	// who is probing for it. An empty handle conflicts too — an alias you can
	// release leaves a name free while somebody is still being called it.
	SetHandle(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, handle string) (*User, error)

	// ByHandle is the human lookup: the user of this tenant who answers to that
	// name, compared without case. ErrNotFound for a name nobody claimed.
	//
	// It is deliberately not a login door. Signing in by handle is a separate
	// decision with an enumeration surface of its own, and it belongs to the auth
	// module, which owns what a failed attempt costs.
	ByHandle(ctx context.Context, tx db.Tx[db.Tenant], handle string) (*User, error)

	// ByEmail is the login lookup: the user of this tenant with that address,
	// compared without case. It is ErrNotFound for an address nobody has.
	ByEmail(ctx context.Context, tx db.Tx[db.Tenant], email string) (*User, error)

	// Provision creates a user from a cross-tenant transaction, naming the
	// tenant. An empty password makes an invited user; anything else makes an
	// active one. Either way it publishes user.invited.
	//
	// It is the control plane's door and nothing else's. Every other way a user
	// comes into being is Invite, inside the tenant's own transaction; this
	// exists because a tenant's first administrator is created from outside
	// that tenant — by the bootstrap, in the same transaction as the tenant
	// itself, and by the operator inviting one into a tenant they do not
	// otherwise have a transaction in.
	Provision(ctx context.Context, tx db.Tx[db.System], tenantID uuid.UUID, email, displayName, password string, roles []string) (*User, error)
}
