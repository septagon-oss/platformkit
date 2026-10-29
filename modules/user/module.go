// Package user is the module manifest: the people in a tenant.
//
// It is the exemplar's shape with one entity, one Spec and explicit lifecycle
// commands. A user belongs to a tenant by carrying its id, which row-level
// security matches on, so there is nothing for this module to ask the tenant
// module for; its one dependency is Deps.Administration, and that is a question
// about roles rather than about users.
package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/internal"
)

// Deps is what this module cannot make for itself, and it is one thing.
//
// It used to be empty, and the day it needed something was the day the floor
// under a tenant's last administrator was written: whether somebody can still
// administer their tenant depends on what the roles they hold grant, and what a
// role grants is the auth module's table. This module reads no other module's
// rows, so the application answers instead.
type Deps struct {
	// Administration answers which of a tenant's roles grant the permission
	// that can grant every other one back. Wire
	// &contracts.AdministrationFunc{Ask: auth.AdministeringRoles}, or the
	// equivalent for whatever owns roles in this composition; take the
	// adapter's address, which is what keeps this struct comparable.
	//
	// It is required rather than optional because the alternative is a product
	// that composes this module, silently gets no floor, and finds out when a
	// customer locks themselves out — the same argument as file.Deps.Storage
	// and notification.Deps.Mailer, which panic for the same reason.
	Administration contracts.Administration

	// Granting answers whether the caller of a roles write may hand out a role
	// that administers the tenant. Wire &contracts.GrantingFunc{Ask: …} over
	// whatever answers "may this caller manage roles" — in the reference
	// application, the Authorizer the API already holds.
	//
	// Required, in Deps.Administration's own words: a composition that supplies
	// none gets a panic at boot rather than a roles screen that promotes whoever
	// is sitting in front of it.
	Granting contracts.Granting
}

// spec is the entity's presence in the application: five routes, two
// permissions, three events and the schema a generated screen reads.
var spec = rest.Spec[*contracts.User]{
	Module: "user",
	Entity: "user",
	Path:   "/users",
	Read:   contracts.PermissionUserRead,
	Write:  contracts.PermissionUserManage,
	// Present is how a person reads. `primaryField` is the address and not
	// displayName: displayName defaults to empty, so a catalogue reader titling
	// by it would name an invited person "Untitled person", and an email is
	// required, unique per tenant and never blank. The display name and handle
	// follow as the summary; timestamps remain on the record.
	Present: entity.EntryHints{
		Description:      "Invite and manage the people who can access this workspace.",
		EmptyDescription: "Add a person, then assign the role they need.",
		Singular:         "person",
		Plural:           "people",
		Icon:             "people",
		Group:            &entity.ResourceGroup{Key: "people", Label: "People"},
		PrimaryField:     "email",
		SummaryFields:    []string{"displayName", "handle"},
		StatusField:      "status",
		Sortable:         []string{"email", "displayName", "updatedAt"},
	},
	SoftDelete: true,
	// The two fields a command owns. Lifecycle commands move status, and
	// roles are changed by SetRoles, which publishes
	// user.roles_set; a caller who could set either through the generic update
	// would deactivate somebody, or make them an administrator, and tell
	// nobody.
	//
	// roles used to be refused by accident: kit/crud's schema had no list type,
	// so the field did not exist as far as a patch was concerned. It exists
	// now, it renders, and it is refused by name — which is also what tells the
	// caller which door to use.
	Immutable: []string{"status", "roles", "handle"},
}

// Why the generated create screen does not offer roles, since the review asked.
//
// It could: the form is derived from the schema, and roles is a list field like
// any other. What stops it is the rule two lines above — a grant is an event
// somebody can audit, published by POST {id}/roles, and refuseLifecycleOnCreate
// below refuses roles at the create route for exactly that reason. A screen
// that offered the field and then silently made a second request would be a
// screen whose audit trail did not match what the person did, and one that
// offered it and let the create refuse it would be a form that cannot be
// submitted.
//
// The door that does take roles and an address together is the invitation:
// POST /api/v1/user/invitations grants them in the same transaction as the
// invitation, so somebody invited as an administrator was never, for a moment,
// a person with no roles who had already been mailed a link. That is the route
// an administrator should be using, and a screen for it is a page a module
// writes rather than one the generator derives — the generator mounts a Spec's
// five routes, and an invitation is not one of them.

// permissions is what the manifest declares. kit/app checks every route's
// declaration against it at boot, so a route guarded by a permission that is
// not here fails startup instead of denying everyone forever. It lives beside
// the manifest and not in contracts/ because it was the only thing there that
// needed kit/module.
var permissions = []module.Permission{
	{Key: contracts.PermissionUserRead, Label: "read people"},
	{Key: contracts.PermissionUserManage, Label: "manage people"},
	{Key: contracts.PermissionRegistrationApprove, Label: "approve new accounts"},
}

// Module is the manifest, and the service it is built on: the auth module takes
// this value from main, because signing somebody in means finding them first.
//
// Deprecated: use NewChecked, which answers this refusal as an error.
func New(deps Deps) (contracts.Service, module.Module) {
	svc, manifest, err := NewChecked(deps)
	if err != nil {
		panic(err.Error())
	}
	return svc, manifest
}

// NewChecked builds the service and the manifest. Deps.Administration and
// Deps.Granting are required, and an adapter of either whose Ask closure is nil
// is refused with it: both doors answer a question, and an empty adapter would
// answer "nobody administers this tenant" to all of them. It opens nothing, runs
// no query and is safe to call twice.
func NewChecked(deps Deps) (contracts.Service, module.Module, error) {
	if deps.Administration == nil {
		return nil, module.Module{}, errors.New("user.Module: Deps.Administration is required; wire auth.AdministeringRoles so the floor under a tenant's last administrator has something to ask")
	}
	// An interface is not nil just because what it holds is. &contracts.
	// AdministrationFunc{} satisfies the field, reaches this composition looking
	// wired, and would answer "nobody administers this tenant" to every question
	// the floor asks — the missing floor again, one struct field deeper. Refused
	// here for the same reason a nil is: composition happens in main, before
	// there is anywhere to report to. contracts.AdministrationFunc.Administering
	// answers the same way as an error, so a hand-written implementation that
	// cannot answer fails the write rather than passing it.
	if asked, ok := deps.Administration.(*contracts.AdministrationFunc); ok && (asked == nil || asked.Ask == nil) {
		return nil, module.Module{}, errors.New("user.Module: Deps.Administration is an adapter with no Ask; wire auth.AdministeringRoles, do not hand the floor an empty adapter")
	}
	if deps.Granting == nil {
		return nil, module.Module{}, errors.New("user.Module: Deps.Granting is required; wire the answer to \"may this caller manage roles\" so granting a role has a door")
	}
	if asked, ok := deps.Granting.(*contracts.GrantingFunc); ok && (asked == nil || asked.Ask == nil) {
		return nil, module.Module{}, errors.New("user.Module: Deps.Granting requires an initialized adapter")
	}
	svc := internal.NewService(deps.Administration, deps.Granting)
	mounted := spec
	mounted.AfterCreate = refuseLifecycleOnCreate
	// The third door, and the one that is not a lifecycle command: DELETE
	// {id} soft-deletes the row, which hides it from ByEmail and from every
	// other read, so deleting the sole administrator locks the tenant out
	// exactly as emptying their roles does. There is no Delete method for the
	// rule to live in, so it is the Spec's hook — which runs inside the
	// request's transaction, so a refusal rolls the delete back.
	mounted.AfterDelete = svc.RefuseLastAdministrator
	return svc, module.Module{
		Name:       "user",
		Migrations: Migrations.Files,
		Adopts:     Migrations.Adopts,
		RulesFrom:  Migrations.RulesFrom,
		// The person is the row itself: the id stays and the values go, which is
		// what an erasure that keeps the audit trail has to mean (ADR 0020).
		PersonalData: personaldata.Implements(personaldata.Table{
			Name:   "users",
			Person: "id",
			Text:   []string{"email", "display_name", "handle"},
		}),
		Permissions: permissions,
		Declared: []events.Declared{
			// The three kit/rest publishes carry the entity itself, which is
			// what Spec.emit marshals; every other name carries the payload
			// type beside it in contracts.
			events.Declare[*contracts.User](contracts.EventCreated),
			events.Declare[*contracts.User](contracts.EventUpdated),
			events.Declare[*contracts.User](contracts.EventDeleted),
			events.Declare[contracts.Invited](contracts.EventInvited),
			events.Declare[contracts.PasswordSet](contracts.EventPasswordSet),
			events.Declare[contracts.RolesSet](contracts.EventRolesSet),
			events.Declare[contracts.Deactivated](contracts.EventDeactivated),
			events.Declare[contracts.Reactivated](contracts.EventReactivated),
			events.Declare[contracts.HandleSet](contracts.EventHandleSet),
			events.Declare[contracts.AdministrationRefused](contracts.EventAdministrationRefused),
			events.Declare[contracts.RegistrationPending](contracts.EventRegistrationPending),
			events.Declare[contracts.RegistrationApproved](contracts.EventRegistrationApproved),
			events.Declare[contracts.RegistrationUnverified](contracts.EventRegistrationUnverified),
			events.Declare[contracts.EmailVerified](contracts.EventEmailVerified),
		},
		Nav: []module.NavEntry{
			{Label: "Users", Screen: "user/users", Permission: contracts.PermissionUserRead},
		},
		Jobs:          nil,
		Subscriptions: nil,
		Routes: func(s httpx.Surfaces) {
			mounted.Mount(s)
			internal.RegisterRoutes(s, mounted, internal.RouteServices{Invitations: svc, Approvals: svc, Passwords: svc, Roles: svc, Handles: svc, Deactivation: svc})
		},
	}, nil
}

// refuseLifecycleOnCreate keeps roles and password registrations behind their
// owner commands. Public signup cannot manufacture their state through generic CRUD.
//
// The hook runs inside the request's transaction, after the row and its event,
// so returning an error rolls the whole create back and the caller gets a 422.
// The PATCH route is guarded by spec.Immutable instead; a create cannot be,
// because there is no row yet to refuse a change to.
//
// The second half of its job is the brief's first rule: a person created through
// the collection is invited. The create route used to write the row, publish
// user.user.created and stop — and nothing subscribes to that name, so an
// administrator who used it made somebody who exists, cannot sign in and was
// never told, which is the defect the walkthrough of record counted as an empty
// directory. Inviting is what the collection means by "create a person": the row
// lands in `invited`, user.invited is published in the same transaction, and the
// auth module mails the link that lets that person choose a password nobody else
// chose for them. A create that left somebody unable to sign in and mailed to
// nobody is refused, which is why `pending` and `unverified` have their own door
// and why no status other than invited is a row the collection can make.
func refuseLifecycleOnCreate(ctx context.Context, tx db.Tx[db.Tenant], u *contracts.User) error {
	if u.Status == contracts.StatusPending || u.Status == contracts.StatusUnverified {
		return fmt.Errorf("%w: password registrations must be created by the registration service", crud.ErrInvalid)
	}
	if u.Status != contracts.StatusInvited {
		return fmt.Errorf("%w: the collection creates the person it invites; %s is a state some other door brings somebody to",
			crud.ErrInvalid, u.Status)
	}
	if err := events.Publish(ctx, tx, contracts.EventInvited, contracts.Invited{
		UserID: u.ID, Email: u.Email, Status: u.Status, At: db.Now(),
		Served: httpx.ServedFrom(ctx),
	}); err != nil {
		return err
	}
	if len(u.Roles) == 0 {
		return nil
	}
	return fmt.Errorf("%w: roles are granted by POST %s/{id}/roles, so that a grant is an event somebody can audit",
		crud.ErrInvalid, spec.Path)
}
