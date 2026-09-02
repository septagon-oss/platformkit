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

// The lifecycle. A user is invited before they have a password, active once
// they can sign in, and inactive when somebody has taken that away. Deleting is
// kit/crud's soft delete, which keeps the row and releases the address.
const (
	StatusInvited  = "invited"
	StatusActive   = "active"
	StatusInactive = "inactive"
)

var statuses = []string{StatusInvited, StatusActive, StatusInactive}

// MinPasswordLength is the shortest password this application accepts. Length
// is the only rule: composition rules push people towards Passw0rd! and a
// twelve-character passphrase beats it, which is what every guidance since
// NIST SP 800-63B has said.
const MinPasswordLength = 12

// Roles is the set of role names a user holds, one text[] column.
//
// It is a named type rather than []string so that the array codec is written
// once, and it has a second effect worth knowing: kit/crud's schema covers a
// closed set of field types and a slice is not in it, so `roles` is not a field
// a PATCH can name. Granting a role is Service.SetRoles, which says so in an
// event; it is not something that happens inside a bulk update of a profile.
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

	// Status is a closed set; the enum tag is what a form renders as a select
	// and what Validate refuses a value outside.
	Status string `json:"status" gorm:"type:text;not null;default:'invited'" enum:"invited,active,inactive" ui:"widget:select" doc:"Lifecycle state" default:"invited" required:"false"`

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

// Service is the user lifecycle: the four commands generic CRUD cannot safely
// infer, because each is a rule about the state it came from and each publishes
// an event, plus the two reads the auth module needs.
//
// Every command takes the caller's transaction rather than opening one, so the
// state change and its event commit together. The errors are kit/crud's:
// ErrNotFound, ErrInvalid, ErrConflict.
//
// Each command is idempotent when repeated with the same argument: the callers
// that retry — a browser, a redelivered event — must not each produce an event.
type Service interface {
	// Invite creates a user with no password, in status invited, and publishes
	// user.invited. Inviting an address that is already here is a conflict.
	Invite(ctx context.Context, tx db.Tx[db.Tenant], email, displayName string) (*User, error)

	// SetPassword hashes and stores a password and makes the user active. The
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

	// ByEmail is the login lookup: the user of this tenant with that address,
	// compared without case. It is ErrNotFound for an address nobody has.
	ByEmail(ctx context.Context, tx db.Tx[db.Tenant], email string) (*User, error)

	// Provision creates an active user with a password from a cross-tenant
	// transaction, naming the tenant.
	//
	// It is the bootstrap's door and nothing else's. Every other way a user
	// comes into being is Invite, inside the tenant's own transaction; this
	// exists because the first administrator of an installation is created in
	// the same transaction as the tenant they administer, and that transaction
	// belongs to no tenant because the tenant is being created in it.
	Provision(ctx context.Context, tx db.Tx[db.System], tenantID uuid.UUID, email, displayName, password string, roles []string) (*User, error)
}
