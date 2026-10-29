package internal

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// RouteServices supplies one capability per account command.
type RouteServices struct {
	Invitations  contracts.InvitationWriter
	Lifecycle    *Service
	Approvals    contracts.RegistrationApprover
	Passwords    contracts.PasswordWriter
	Roles        contracts.RoleSetter
	Handles      contracts.HandleSetter
	Deactivation contracts.Deactivator
}

// RegisterRoutes mounts account commands on the resource owned by spec.
func RegisterRoutes(surfaces httpx.Surfaces, spec rest.Spec[*contracts.User], svc RouteServices) {
	if svc.Lifecycle != nil {
		registerInvitationRoutes(surfaces, svc.Lifecycle)
		mountApprovals(surfaces, spec, svc.Approvals)
		registerUserAccessCommands(surfaces, spec, svc.Lifecycle)
		mountHandles(surfaces, spec, svc.Handles)
		mountDeactivation(surfaces, spec, svc.Deactivation, svc.Lifecycle)
	} else {
		mountInvitations(surfaces, spec, svc.Invitations)
		mountApprovals(surfaces, spec, svc.Approvals)
		mountPasswords(surfaces, spec, svc.Passwords)
		mountRoles(surfaces, spec, svc.Roles)
		mountHandles(surfaces, spec, svc.Handles)
		mountDeactivation(surfaces, spec, svc.Deactivation, nil)
	}
}

func mountInvitations(surfaces httpx.Surfaces, spec rest.Spec[*contracts.User], svc contracts.InvitationWriter) {
	httpx.Register(surfaces.App, huma.Operation{
		OperationID:   "user-invitation-create",
		Method:        http.MethodPost,
		Path:          invitations,
		Summary:       "Invite somebody into this tenant",
		Description:   "Creates a person who cannot sign in yet and publishes user.invited, which is what mails them a link to choose a password. Inviting an address that is already here is a conflict. Roles are granted in the same transaction, so an invitation that names them is one event about who was made what.",
		Tags:          []string{"user"},
		DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity,
			http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventInvited, contracts.EventRolesSet,
		}},
	}, httpx.Permission(contracts.PermissionUserManage),
		func(ctx context.Context, in *inviteInput) (*rest.Item[*contracts.User], error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
			}
			u, err := svc.Invite(ctx, tx, in.Body.Email, in.Body.DisplayName)
			if err != nil {
				return nil, rest.Fault(err)
			}
			if len(in.Body.Roles) > 0 {
				// In the same transaction as the invitation, so that a person
				// who is invited as an administrator was never, for a moment, a
				// person with no roles who had already been mailed a link.
				if u, err = svc.SetRoles(ctx, tx, u.ID, in.Body.Roles); err != nil {
					return nil, rest.Fault(err)
				}
			}
			return &rest.Item[*contracts.User]{Body: u}, nil
		})
}

func mountApprovals(surfaces httpx.Surfaces, spec rest.Spec[*contracts.User], svc contracts.RegistrationApprover) {
	rest.Command(surfaces, spec, "approve-registration",
		"Approve a pending registration",
		"Activates a pending account without changing its password or roles. Active accounts are unchanged; invitations and deactivated accounts cannot be approved.",
		[]string{contracts.EventRegistrationApproved},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.User, error) {
			principal, _ := tenancy.PrincipalFrom(ctx)
			return svc.ApproveRegistration(ctx, tx, id, principal.UserID)
		}, rest.CommandOptions{Auth: httpx.Permission(contracts.PermissionRegistrationApprove), Present: entity.CommandHints{Label: "Approve"}})
}

func mountPasswords(surfaces httpx.Surfaces, spec rest.Spec[*contracts.User], svc contracts.PasswordWriter) {
	rest.Command(surfaces, spec, "set-password",
		"Set a user's password",
		"Stores an argon2id hash and makes an invited user active. It is not idempotent: setting a password to what it already was is still a password change, and somebody who did it deliberately has to see it in their own audit trail.",
		[]string{contracts.EventPasswordSet},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in passwordBody) (*contracts.User, error) {
			if err := svc.SetPassword(ctx, tx, id, in.Password); err != nil {
				return nil, err
			}
			return svc.Get(ctx, tx, id)
		}, rest.CommandOptions{Present: entity.CommandHints{Label: "Set password"}})
}

func mountRoles(surfaces httpx.Surfaces, spec rest.Spec[*contracts.User], svc contracts.RoleSetter) {
	rest.Command(surfaces, spec, "roles",
		"Set a user's roles",
		"Replaces the roles this person holds. The same set again, in any order, changes nothing and publishes nothing.",
		[]string{contracts.EventRolesSet},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in rolesBody) (*contracts.User, error) {
			return svc.SetRoles(ctx, tx, id, in.Roles)
		}, rest.CommandOptions{Present: entity.CommandHints{Label: "Change roles"}})
}

func mountHandles(surfaces httpx.Surfaces, spec rest.Spec[*contracts.User], svc contracts.HandleSetter) {
	rest.Command(surfaces, spec, "handle",
		"Claim or change a user's handle",
		"The lower-case name this person answers to in this tenant, used in URLs and mentions. Renameable, because every key and audit entry names the uuid and nothing has to follow the name: the previous holder stays in user.handle_set. A handle somebody else holds is a conflict that names the rule and not the holder, and a handle cannot be released.",
		[]string{contracts.EventHandleSet},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in handleBody) (*contracts.User, error) {
			return svc.SetHandle(ctx, tx, id, in.Handle)
		}, rest.CommandOptions{Auth: httpx.Permission(contracts.PermissionUserManage), Present: entity.CommandHints{Label: "Set username"}})
}

func mountDeactivation(surfaces httpx.Surfaces, spec rest.Spec[*contracts.User], svc contracts.Deactivator, lifecycle *Service) {
	events := []string{contracts.EventDeactivated}
	if lifecycle != nil {
		events = append(events, contracts.EventInvitationRevoked)
	}
	rest.Command(surfaces, spec, "deactivate",
		"Deactivate a user",
		"Stops the person signing in. Their sessions stop working with them, because a session is only honoured for an active user, so there is no list of sessions to walk. Deactivating them again changes nothing.",
		events,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.User, error) {
			if lifecycle == nil {
				return svc.Deactivate(ctx, tx, id)
			}
			revision, err := deactivationRevision(ctx)
			if err != nil {
				return nil, err
			}
			return lifecycle.deactivateFromHTTP(ctx, tx, id, revision)
		}, rest.CommandOptions{Present: entity.CommandHints{
			Label:       "Deactivate",
			Destructive: true,
			Confirmation: &entity.CommandConfirmation{
				Title:        "Deactivate this person?",
				Body:         "They will not be able to sign in. An administrator can reactivate them.",
				ConfirmLabel: "Deactivate",
			},
		}})
}

// passwordBody and rolesBody are the arguments of the two commands that take
// one. deactivate takes none, so its body is the empty struct and a caller
// sends no body at all.
type passwordBody struct {
	Password         string `json:"password" ui:"label:New password" minLength:"12" maxLength:"256" doc:"The new password; at least twelve characters"`
	ExpectedRevision int64  `json:"expectedRevision,omitempty"`
}

type handleBody struct {
	Handle string `json:"handle" ui:"label:Username" minLength:"3" maxLength:"32" doc:"Lower-case letters, digits and interior . _ -, three to thirty-two characters, and not a name that stands for the platform" example:"ada"`
}

type rolesBody struct {
	Roles            []string `json:"roles" doc:"The roles this person holds from now on" example:"admin"`
	ExpectedRevision int64    `json:"expectedRevision,omitempty"`
}

// invitations is where somebody is invited. It is beside the users collection
// rather than under it because an invitation is not a sub-resource of the user
// it creates.
const invitations = "/invitations"

// Invitation is what a caller sends to invite somebody. It is a named type and
// not the anonymous struct it was, because huma names a schema after the Go
// type behind it and an anonymous one came out of the generator as
// "InviteInputBody1" — a name with this package's plumbing and a deduplication
// counter in it, published in the OpenAPI document and compiled into every
// generated client.
type Invitation struct {
	Email       string   `json:"email" format:"email" maxLength:"320" doc:"The address to invite" example:"ada@acme.example.com"`
	DisplayName string   `json:"displayName,omitempty" maxLength:"200" doc:"Name to show" example:"Ada Lovelace"`
	Roles       []string `json:"roles,omitempty" doc:"Roles to grant in the same transaction" example:"admin"`
}

type inviteInput struct {
	Body Invitation `required:"true"`
}
