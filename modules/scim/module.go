// Package scim is the module manifest: the door an identity provider provisions
// through.
//
// It is the shape of modules/task with no screen and no job. What it owns is two
// facts no other door in this platform records — which collection a provider
// says a person belongs to, and which of its own identifiers it calls them by —
// plus the tenant's own binding from a collection to a role. Everything about
// the person themselves stays modules/user's: this module invites, activates,
// renames and deactivates by calling that module's commands, so a person
// provisioned from here and a person invited from the screen are the same row,
// published by the same events, guarded by the same rules.
package scim

import (
	"errors"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
	"github.com/septagon-oss/platformkit/modules/scim/internal"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Deps is what this module cannot make for itself, and it is three things, all of
// which are somebody else's rows.
type Deps struct {
	// Users is the only writer of people this module knows. Provisioning is
	// Invite, ConfirmAddress, SetRoles and Deactivate; this module writes no column
	// of the users table and locks a person's row only to decide what their roles
	// become, because the rules about who may become what — and who may take the
	// last administrator away — live behind those commands.
	// Deprecated: use Provisioning.
	Users usercontracts.Service
	// Provisioning owns account lifecycle commands and takes precedence over Users.
	Provisioning usercontracts.Provisioning

	// Roles answers whether this tenant knows a role name at all, which is the
	// question a group→role binding has to be able to ask. Role definitions are
	// the auth module's table and this module reads no other module's rows, so the
	// composition answers it — see contracts.RoleDirectory.
	Roles contracts.RoleDirectory

	// Authority answers whether the caller of a write holds the permission that
	// command names: the provisioning door's own, for a person or a collection, and
	// user:manage for the binding that decides what a membership grants. Which roles a person holds is
	// modules/user's column and what those roles grant is the auth module's table;
	// this module reads neither, so the composition answers the joined question —
	// see contracts.Authority.
	//
	// It is required for the reason the other two are: a composition that supplies
	// none gets a panic at boot rather than a mapping door whose command-level
	// recheck silently answers "yes" for whoever reaches it by name.
	Authority contracts.Authority
}

// permissions is what the manifest declares: the one grant the provider's
// credential carries. The mapping door needs no declaration of its own because it
// is guarded by the user module's existing user:read and user:manage, and a
// permission key that no route asks for is a key somebody could hand out for
// nothing.
var permissions = []module.Permission{
	{Key: contracts.PermissionProvision, Label: "provision people and groups"},
}

// New returns the service and the manifest built on it.
//
// Every dependency is required, and refused here rather than at first use: a
// composition that gets a nil and boots anyway has a provisioning door that
// either cannot write a person, binds groups to roles nobody declared, or writes
// a role grant it never asked anybody about, and all three answers look like
// success until a customer's directory is half-imported.
//
// Deprecated: use NewChecked, which answers this refusal as an error.
func New(deps Deps) (contracts.Service, module.Module) {
	svc, manifest, err := NewChecked(deps)
	if err != nil {
		panic(err.Error())
	}
	return svc, manifest
}

// NewChecked builds the service and the manifest. Every port is required and
// refused by name — the people this module provisions, the role names a group
// may bind to, and whoever resolves a caller's roles — because each one answers
// a write that would otherwise look like success. It opens nothing, runs no
// query and is safe to call twice.
func NewChecked(deps Deps) (contracts.Service, module.Module, error) {
	people := deps.Provisioning
	if people == nil {
		people = deps.Users
	}
	if people == nil {
		return nil, module.Module{}, errors.New("scim.Module: Deps.Users is required; the people this module provisions are modules/user's rows and only its commands may write them")
	}
	if deps.Roles == nil {
		return nil, module.Module{}, errors.New("scim.Module: Deps.Roles is required; wire contracts.RoleDirectory over whatever owns the tenant's role names, or a group can be bound to a role that does not exist")
	}
	if deps.Authority == nil {
		return nil, module.Module{}, errors.New("scim.Module: Deps.Authority is required; wire contracts.Authority over whoever resolves a caller's roles into permissions, or a command's recheck of who is asking has nothing to ask")
	}
	svc := internal.NewService(people, deps.Roles, deps.Authority)
	return svc, module.Module{
		Name:       "scim",
		Migrations: Migrations.Files,
		// Three uuid columns of this module's SQL resolve to a person, and one text
		// column may hold their words: the identifier is whatever string the
		// provisioning client sent, and a directory that keys people by mail sends
		// their address (internal/personaldata.go is what takes it out again). A
		// group is this module's own row, never a person, and a membership is two
		// uuids and no words.
		PersonalData: personaldata.Implements(
			personaldata.Table{Name: "scim_group_members", About: []string{"user_id"}, NotPerson: []string{"group_id"}},
			personaldata.Table{Name: "scim_group_mappings", NotPerson: []string{"group_id"}},
			personaldata.Table{Name: "scim_user_external", About: []string{"user_id"},
				Text: []string{"external_id"}},
		),
		Adopts:      Migrations.Adopts,
		RulesFrom:   Migrations.RulesFrom,
		Permissions: permissions,
		// The five events of the state and the writes this module owns: the
		// collection, its membership, its removal, this tenant's binding of a
		// collection to a role, and the two attributes of a person no other module
		// publishes. What happens to a person otherwise stays modules/user's event —
		// an identity created by a connector and one created by an administrator are
		// the same fact and are published by one name — but a group declared with
		// nobody in it changes nobody's roles, a withdrawn mapping decides what a role
		// costs without writing a single user row, and modules/user owns no command
		// for a name, so a connector that renamed a person or re-labelled them with
		// one of its own identifiers would otherwise leave no trace anywhere at all.
		// The audit trail subscribes to every name in every manifest, so declaring
		// these is what makes a group or a person write auditable at the door it
		// arrived through.
		Declared: []events.Declared{
			events.Declare[contracts.GroupCreated](contracts.EventGroupCreated),
			events.Declare[contracts.GroupChanged](contracts.EventGroupChanged),
			events.Declare[contracts.GroupRemoved](contracts.EventGroupRemoved),
			events.Declare[contracts.MappingChanged](contracts.EventMappingChanged),
			events.Declare[contracts.UserChanged](contracts.EventUserChanged),
		},
		Nav:           nil,
		Jobs:          nil,
		Roles:         nil,
		Subscriptions: nil,
		Routes: func(s httpx.Surfaces) {
			internal.RegisterRoutes(s, svc)
		},
	}, nil
}

// PersonalData is this module's part of a person's export and erasure, for the
// composition to hand to the module that walks them. It is a package function and
// not a method of the service because the wiring that places it lives where this
// module's two ports are answered — in the application — and neither of its two
// operations asks the provisioning door anything.
func PersonalData() personaldata.Part { return internal.PersonalData() }
