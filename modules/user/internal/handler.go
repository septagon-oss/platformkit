package internal

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// RegisterRoutes mounts the lifecycle commands on the same resource the
// Spec mounts the five CRUD routes on.
//
// They are routes rather than fields of a PATCH because each is a rule about
// the state the user is in and each publishes an event. Setting a password
// through a generic update would put a password in a request body beside a
// display name, and in the update event beside it too; granting a role through
// one would make "who was made an administrator, and when" a question with no
// answer. spec.Immutable is the other half of that argument, and rest.Command
// is the part all three share.
func RegisterRoutes(surfaces httpx.Surfaces, spec rest.Spec[*contracts.User], svc *Service) {
	registerInvitationRoutes(surfaces, svc)

	rest.Command(surfaces, spec, "approve-registration",
		"Approve a pending registration",
		"Activates a pending account without changing its password or roles. Active accounts are unchanged; invitations and deactivated accounts cannot be approved.",
		[]string{contracts.EventRegistrationApproved},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.User, error) {
			principal, _ := tenancy.PrincipalFrom(ctx)
			return svc.ApproveRegistration(ctx, tx, id, principal.UserID)
		}, rest.CommandOptions{Auth: httpx.Permission(contracts.PermissionRegistrationApprove), Present: entity.CommandHints{Label: "Approve"}})

	registerUserAccessCommands(surfaces, spec, svc)

	rest.Command(surfaces, spec, "handle",
		"Claim or change a user's handle",
		"The lower-case name this person answers to in this tenant, used in URLs and mentions. Renameable, because every key and audit entry names the uuid and nothing has to follow the name: the previous holder stays in user.handle_set. A handle somebody else holds is a conflict that names the rule and not the holder, and a handle cannot be released.",
		[]string{contracts.EventHandleSet},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in handleBody) (*contracts.User, error) {
			return svc.SetHandle(ctx, tx, id, in.Handle)
		}, rest.CommandOptions{Auth: httpx.Permission(contracts.PermissionUserManage), Present: entity.CommandHints{Label: "Set username"}})

	rest.Command(surfaces, spec, "deactivate",
		"Deactivate a user",
		"Stops the person signing in. Their sessions stop working with them, because a session is only honoured for an active user, so there is no list of sessions to walk. Deactivating them again changes nothing.",
		[]string{contracts.EventDeactivated, contracts.EventInvitationRevoked},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.User, error) {
			revision, err := deactivationRevision(ctx)
			if err != nil {
				return nil, err
			}
			return svc.deactivateFromHTTP(ctx, tx, id, revision)
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

// passwordBody and rolesBody retain their existing fields and accept an
// observed invitation revision. Deactivation keeps its published empty body.
type passwordBody struct {
	ExpectedRevision int64  `json:"expectedRevision,omitempty"`
	Password         string `json:"password" ui:"label:New password" minLength:"12" maxLength:"256" doc:"The new password; at least twelve characters"`
}

type handleBody struct {
	Handle string `json:"handle" ui:"label:Username" minLength:"3" maxLength:"32" doc:"Lower-case letters, digits and interior . _ -, three to thirty-two characters, and not a name that stands for the platform" example:"ada"`
}

type rolesBody struct {
	ExpectedRevision int64    `json:"expectedRevision,omitempty"`
	Roles            []string `json:"roles" doc:"The roles this person holds from now on" example:"admin"`
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
