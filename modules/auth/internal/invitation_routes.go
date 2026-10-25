package internal

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

type invitationReadInput struct {
	ID uuid.UUID `path:"id"`
}
type invitationListInput struct {
	Limit  int    `query:"limit" default:"25" minimum:"1" maximum:"100"`
	Offset int    `query:"offset" default:"0" minimum:"0"`
	State  string `query:"state" enums:"pending,expired,accepted,revoked"`
}
type invitationReadResponse struct {
	CacheControl string `header:"Cache-Control"`
	Body         *contracts.InvitationReading
}
type invitationListResponse struct {
	CacheControl string `header:"Cache-Control"`
	Body         []*contracts.InvitationReading
}

// RegisterInvitationReadings mounts authorized redacted snapshots on auth's surface.
func RegisterInvitationReadings(s httpx.Surfaces, svc *Service) {
	httpx.Register(s.App, huma.Operation{OperationID: "auth-invitation-read", Method: http.MethodGet, Path: "/invitations/{id}",
		Summary: "Read an invitation and its delivery observation", Tags: []string{"auth"}}, httpx.Permission(user.PermissionUserRead),
		func(ctx context.Context, in *invitationReadInput) (*invitationReadResponse, error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, invitationReadFault(user.ErrInvitationUnavailable)
			}
			row, err := svc.GetInvitationReading(ctx, tx, in.ID)
			if err != nil {
				return nil, invitationReadFault(err)
			}
			return &invitationReadResponse{CacheControl: "no-store", Body: row}, nil
		})
	httpx.Register(s.App, huma.Operation{OperationID: "auth-invitation-list", Method: http.MethodGet, Path: "/invitations",
		Summary: "List invitations and their delivery observations", Tags: []string{"auth"}}, httpx.Permission(user.PermissionUserRead),
		func(ctx context.Context, in *invitationListInput) (*invitationListResponse, error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, invitationReadFault(user.ErrInvitationUnavailable)
			}
			rows, err := svc.ListInvitationReadings(ctx, tx, user.InvitationQuery{Limit: in.Limit, Offset: in.Offset, State: in.State})
			if err != nil {
				return nil, invitationReadFault(err)
			}
			return &invitationListResponse{CacheControl: "no-store", Body: rows}, nil
		})
}

func invitationReadFault(err error) error {
	switch {
	case errors.Is(err, user.ErrInvitationUnauthenticated):
		return problem.New(http.StatusUnauthorized, "authentication is required")
	case errors.Is(err, user.ErrInvitationForbidden):
		return problem.New(http.StatusForbidden, "current authority does not permit reading invitations")
	case errors.Is(err, user.ErrInvitationUnavailable), errors.Is(err, user.ErrInvitationInitializing):
		return problem.New(http.StatusServiceUnavailable, err.Error())
	default:
		return rest.Fault(err)
	}
}
