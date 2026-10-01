package internal

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// The routes a person's own keys are managed through. Three, all SignedIn, all
// about the caller — which is why none takes a permission, and why there is no
// route here that lets one person mint a key as another.
//
// The revocation is a POST rather than a DELETE for the reason the session
// revocations give: the page that offers it must be able to make the write as a
// CSRF-covered form request, and a form cannot send a DELETE. A JSON client that
// asks for DELETE gets 405 and the same answer.
func RegisterTokenRoutes(surfaces httpx.Surfaces, tokens contracts.Tokens) {
	httpx.Register(surfaces.App, huma.Operation{
		OperationID: "auth-token-issue",
		Method:      http.MethodPost,
		Path:        "/tokens",
		Summary:     "Issue a bearer token",
		Description: "Mints a key a script or a mobile shell presents as Authorization: Bearer. Every scope must be one some module defines and one this person's own roles already grant, so a key can never hold authority its holder does not have, and an operator scope is refused outside the operator's own tenant. The token appears once, in this response; the table keeps only its hash.",
		Tags:        []string{"auth"}, DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventAPITokenIssued,
		}},
	}, httpx.SignedIn(), handleIssueToken(tokens, surfaces))

	httpx.Register(surfaces.App, huma.Operation{
		OperationID: "auth-token-list",
		Method:      http.MethodGet,
		Path:        "/tokens",
		Summary:     "List my bearer tokens",
		Description: "This person's own keys: what each may do, when each dies, and which are already stopped. No token, hash or prefix appears in it — a list of keys would be a list of credentials, and this list is a screen somebody reads.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusServiceUnavailable},
	}, httpx.SignedIn(), handleListTokens(tokens))

	httpx.Register(surfaces.App, huma.Operation{
		OperationID: "auth-token-revoke",
		Method:      http.MethodPost,
		Path:        "/tokens/{id}/revoke",
		Summary:     "Revoke one of my bearer tokens",
		Description: "Stops one key of this person's own and leaves their sessions alone: a stolen key and a stolen laptop are different incidents with different answers. An id that is not one of theirs is a 404 whether it never existed, is a colleague's, or belongs to another tenant.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusNotFound, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventAPITokenRevoked,
		}},
	}, httpx.SignedIn(), handleRevokeToken(tokens))
}

func handleIssueToken(tokens contracts.Tokens, surfaces httpx.Surfaces) func(context.Context, *issueTokenInput) (*issuedTokenOutput, error) {
	return func(ctx context.Context, in *issueTokenInput) (*issuedTokenOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		caller, ok := tenancy.PrincipalFrom(ctx)
		if !ok || caller.UserID == uuid.Nil {
			return nil, problem.New(http.StatusForbidden, "this operation answers for the caller themselves")
		}
		// The catalogue comes from the kernel at request time, the way SetRole
		// takes it: a module that knew which permissions exist would know its
		// neighbours, and one that checked scopes against a stale list would
		// refuse a key for a permission its own application added this morning.
		issued, err := tokens.IssueToken(ctx, tx, caller.UserID, contracts.TokenIntent{
			Name: in.Body.Name, Scopes: in.Body.Scopes, ExpiresAt: in.Body.ExpiresAt,
		}, surfaces.Permissions())
		if err != nil {
			if errors.Is(err, contracts.ErrTokenScope) {
				// 422 and not 500: a scope that is not the caller's to delegate
				// is a correctable input, and the one message covers the three
				// facts it covers so that a refused request cannot be used to
				// discover which permissions exist in another tenant.
				return nil, problem.New(http.StatusUnprocessableEntity,
					"every scope has to be a permission this account already holds")
			}
			return nil, rest.Fault(err)
		}
		out := &issuedTokenOutput{}
		out.Body = *issued
		return out, nil
	}
}

func handleListTokens(tokens contracts.Tokens) func(context.Context, *struct{}) (*tokensOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*tokensOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		caller, ok := tenancy.PrincipalFrom(ctx)
		if !ok || caller.UserID == uuid.Nil {
			return nil, problem.New(http.StatusForbidden, "this operation answers for the caller themselves")
		}
		items, err := tokens.ListTokens(ctx, tx, caller.UserID)
		if err != nil {
			return nil, rest.Fault(err)
		}
		out := &tokensOutput{}
		out.Body.Items, out.Body.Total = items, len(items)
		return out, nil
	}
}

func handleRevokeToken(tokens contracts.Tokens) func(context.Context, *revokeTokenInput) (*doneOutput, error) {
	return func(ctx context.Context, in *revokeTokenInput) (*doneOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		caller, ok := tenancy.PrincipalFrom(ctx)
		if !ok || caller.UserID == uuid.Nil {
			return nil, problem.New(http.StatusForbidden, "this operation answers for the caller themselves")
		}
		if err := tokens.RevokeToken(ctx, tx, caller.UserID, in.ID); err != nil {
			return nil, rest.Fault(err)
		}
		return done(), nil
	}
}

type issueTokenInput struct {
	Body struct {
		Name      string    `json:"name" minLength:"1" maxLength:"80" required:"true" doc:"What this key is, in the words whoever reads the list would use: \"Deploy bot\", \"Ada's laptop\""`
		Scopes    []string  `json:"scopes" minItems:"1" doc:"Permission keys this key may use. Each must be one some module defines and one the issuer's own roles already grant; an empty list is refused rather than meaning everything"`
		ExpiresAt time.Time `json:"expiresAt,omitempty" required:"false" doc:"When the key stops working. Omit for the default of 30 days; the maximum is 180 and nothing extends either"`
	} `json:"body" required:"true"`
}

type issuedTokenOutput struct {
	Body contracts.IssuedToken
}

type tokensOutput struct {
	Body struct {
		Items []*contracts.APIToken `json:"items"`
		Total int                   `json:"total"`
	}
}

type revokeTokenInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The key to stop, as the list showed it"`
}
