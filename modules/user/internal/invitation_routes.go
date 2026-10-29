package internal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

type invitationRevisionBody struct {
	ExpectedRevision int64 `json:"expectedRevision,omitempty"`
}
type invitationCommandInput struct {
	ID   uuid.UUID `path:"id"`
	Body invitationRevisionBody
}
type invitationCreated struct {
	Location string `header:"Location"`
	Body     *contracts.User
}

func registerInvitationRoutes(s httpx.Surfaces, svc *Service) {
	httpx.Register(s.App, huma.Operation{OperationID: "user-invitation-create", Method: http.MethodPost, Path: invitations,
		Errors:  []int{http.StatusConflict, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
		Summary: "Invite somebody into this tenant", DefaultStatus: http.StatusCreated, Tags: []string{"user"},
		Extensions: map[string]any{httpx.EventsExtension: []string{contracts.EventInvited, contracts.EventRolesSet}}}, httpx.Permission(contracts.PermissionUserManage),
		func(ctx context.Context, in *inviteInput) (*invitationCreated, error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
			}
			u, err := svc.createFromHTTP(ctx, tx, in.Body)
			if err != nil {
				return nil, InvitationFault(err)
			}
			return &invitationCreated{Location: "/api/v1/auth/invitations/" + u.ID.String(), Body: u}, nil
		})
	registerInvitationMutation(s, svc, "resend", http.StatusAccepted, contracts.EventInvitationResent, svc.ResendInvitation)
	registerInvitationMutation(s, svc, "revoke", http.StatusOK, contracts.EventInvitationRevoked, svc.RevokeInvitation)
}

func registerInvitationMutation(s httpx.Surfaces, svc *Service, verb string, status int, event string,
	run func(context.Context, db.Tx[db.Tenant], uuid.UUID, int64) (*contracts.InvitationSnapshot, error)) {
	httpx.Register(s.App, huma.Operation{OperationID: "user-invitation-" + verb, Method: http.MethodPost,
		Path: invitations + "/{id}/" + verb, Summary: verb + " invitation", DefaultStatus: status, Tags: []string{"user"},
		Extensions: map[string]any{httpx.EventsExtension: []string{event}}}, httpx.Permission(contracts.PermissionUserManage),
		func(ctx context.Context, in *invitationCommandInput) (*rest.Item[*contracts.InvitationSnapshot], error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
			}
			got, err := run(ctx, tx, in.ID, in.Body.ExpectedRevision)
			if cooldown, ok := errors.AsType[*contracts.InvitationCooldown](err); ok {
				retryAt := cooldown.RetryAt.UTC()
				if retryAt.Nanosecond() != 0 {
					retryAt = retryAt.Truncate(time.Second).Add(time.Second)
				}
				return nil, huma.ErrorWithHeaders(problem.New(http.StatusTooManyRequests, "request a replacement after "+cooldown.RetryAt.Format(time.RFC3339)),
					http.Header{"Retry-After": []string{retryAt.Format(http.TimeFormat)}})
			}
			if err != nil {
				return nil, InvitationFault(err)
			}
			return &rest.Item[*contracts.InvitationSnapshot]{Body: got}, nil
		})
}

// InvitationFault translates the owner refusals without returning a subject row.
func InvitationFault(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, contracts.ErrInvitationUnauthenticated):
		return problem.New(http.StatusUnauthorized, "authentication is required")
	case errors.Is(err, contracts.ErrInvitationForbidden):
		return problem.New(http.StatusForbidden, "current authority does not permit this invitation operation")
	case errors.Is(err, contracts.ErrInvitationUnavailable), errors.Is(err, contracts.ErrInvitationInitializing):
		return problem.New(http.StatusServiceUnavailable, err.Error())
	default:
		return rest.Fault(err)
	}
}

func (s *Service) createFromHTTP(ctx context.Context, tx db.Tx[db.Tenant], in Invitation) (*contracts.User, error) {
	if s.invitationTiming.Lifetime > 0 {
		return s.CreateInvitation(ctx, tx, contracts.InvitationIntent{Email: in.Email, DisplayName: in.DisplayName, Roles: in.Roles})
	}
	u, err := s.Invite(ctx, tx, in.Email, in.DisplayName)
	if err != nil {
		return nil, err
	}
	if len(in.Roles) > 0 {
		return s.SetRoles(ctx, tx, u.ID, in.Roles)
	}
	return u, nil
}

func registerUserAccessCommands(s httpx.Surfaces, spec rest.Spec[*contracts.User], svc *Service) {
	rest.Command(s, spec, "roles", "Set a user's roles", "Replaces assigned roles; waiting invitations require expectedRevision.",
		[]string{contracts.EventRolesSet}, svc.rolesFromHTTP, rest.CommandOptions{Present: entity.CommandHints{Label: "Change roles"}})
	rest.Command(s, spec, "set-password", "Set a user's password", "Records direct admission for a waiting invitation at its expectedRevision.",
		[]string{contracts.EventPasswordSet, contracts.EventInvitationAccepted}, svc.passwordFromHTTP,
		rest.CommandOptions{Present: entity.CommandHints{Label: "Set password"}})
}

// invitationCommandRequired reports whether the row's own state makes the write
// an invitation command rather than the plain one: the invitation is still open,
// because its recorded history waits for the person's own password, or the person
// is still invited and the application runs that lifecycle. Only the row decides.
// A closed invitation is not a waiting one, so the person it left behind is
// deactivated, re-roled and given a password by the plain command, as they were
// before this module kept any history; a quoted expectedRevision decides nothing,
// because a settled row has no open invitation to quote and no write counter of
// its own to honour a quotation against.
func (s *Service) invitationCommandRequired(u *contracts.User) bool {
	return u.Invitation.Phase == contracts.InvitationPending || (u.Status == contracts.StatusInvited && s.invitationTiming.Lifetime > 0)
}

func (s *Service) rolesFromHTTP(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in rolesBody) (*contracts.User, error) {
	u, err := crud.GetForUpdate[*contracts.User](tx, id)
	if err != nil {
		return nil, err
	}
	if s.invitationCommandRequired(u) {
		got, err := s.SetInvitationRoles(ctx, tx, id, int64(in.ExpectedRevision), in.Roles)
		return got, InvitationFault(err)
	}
	return s.SetRoles(ctx, tx, id, in.Roles)
}

func (s *Service) passwordFromHTTP(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in passwordBody) (*contracts.User, error) {
	u, err := crud.GetForUpdate[*contracts.User](tx, id)
	if err != nil {
		return nil, err
	}
	if s.invitationCommandRequired(u) {
		err = s.SetInvitationPassword(ctx, tx, id, int64(in.ExpectedRevision), in.Password)
	} else {
		err = s.SetPassword(ctx, tx, id, in.Password)
	}
	if err != nil {
		return nil, InvitationFault(err)
	}
	return s.Get(ctx, tx, id)
}

func (s *Service) deactivateFromHTTP(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, revision int64) (*contracts.User, error) {
	u, err := crud.GetForUpdate[*contracts.User](tx, id)
	if err != nil {
		return nil, err
	}
	if s.invitationCommandRequired(u) {
		if _, err := s.RevokeInvitation(ctx, tx, id, revision); err != nil {
			return nil, InvitationFault(err)
		}
		return s.Get(ctx, tx, id)
	}
	return s.Deactivate(ctx, tx, id)
}

// deactivationRevision preserves the published empty JSON body. Invitation
// callers supply expectedRevision in the query or use the dedicated revoke body.
func deactivationRevision(ctx context.Context) (int64, error) {
	request, ok := httpx.RequestFrom(ctx)
	if !ok || request.URL.Query().Get("expectedRevision") == "" {
		return 0, nil
	}
	revision, err := strconv.ParseInt(request.URL.Query().Get("expectedRevision"), 10, 64)
	if err != nil || revision <= 0 {
		return 0, fmt.Errorf("%w: expectedRevision must be positive", crud.ErrInvalid)
	}
	return revision, nil
}
