package internal

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// The routes a person's own keys are managed through, all about the caller —
// which is why none takes a permission, and why there is no route here that lets
// one person mint a key as another. Three are SignedIn; the fourth, a key
// retiring itself, is BearerKey, the one credential command a scoped key may
// run (docs/adr/0020-a-credential-may-retire-itself.md).
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

	httpx.Register(surfaces.App, huma.Operation{
		OperationID: "auth-token-revoke-self",
		Method:      http.MethodPost,
		Path:        "/tokens/revoke-self",
		Summary:     "Revoke the key that signed this request in",
		Description: "Ends the key that authenticated this request, and no other: there is no id to name, and no management grant is needed, so a key too narrow for anything else can still retire itself. 204 with no body, because the credential it would describe no longer exists. A session is refused with 403, since it carries no key; a revoked, expired or unknown key is not recognised and is answered 401 with a Bearer challenge before this operation runs. X-Expected-Principal is honoured if sent and never required.",
		Tags:        []string{"auth"}, DefaultStatus: http.StatusNoContent,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventAPITokenRevoked,
		}},
	}, httpx.BearerKey(), handleRevokeOwnToken(tokens))
	registerSelfRevocation(surfaces, tokens)
}

// registerSelfRevocation puts the key-retiring command in the resource
// catalogue, so a client whose only credential is a key can find it. The token
// resource it rides on declares no read and offers no screen — its list is a
// SignedIn route, never this document — so the entry is the command alone, and
// only for a caller BearerKey admits.
func registerSelfRevocation(surfaces httpx.Surfaces, tokens contracts.Tokens) {
	surfaces.RegisterResource(httpx.Resource{
		Module: "auth", Entity: "token", Path: "/tokens",
		Schema: entity.Schema{Module: "auth", Entity: "token", Path: surfaces.App.Path("/tokens"), Fields: []entity.Field{}},
	})
	surfaces.AddCommand("auth", "token", httpx.Command{
		Verb: "revoke-self", Collection: true, Auth: httpx.BearerKey(),
		Summary:     "Revoke the key that signed this request in",
		Description: "Ends the key that authenticated this request, and no other.",
		Endpoint:    surfaces.App.Path("/tokens/revoke-self"),
		Run: func(ctx context.Context, _ uuid.UUID, _ map[string]any) error {
			return revokeOwnToken(ctx, tokens)
		},
	})
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

func handleRevokeOwnToken(tokens contracts.Tokens) func(context.Context, *struct{}) (*struct{}, error) {
	return func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		return nil, revokeOwnToken(ctx, tokens)
	}
}

// revokeOwnToken ends the key that authenticated ctx's request, in its
// transaction: the route's work and the catalogue command's, so the two cannot
// differ. A caller with no key is refused 403 and nothing is written.
func revokeOwnToken(ctx context.Context, tokens contracts.Tokens) error {
	tx, err := transaction(ctx)
	if err != nil {
		return err
	}
	caller, _ := tenancy.PrincipalFrom(ctx)
	if err := tokens.RevokeOwnToken(ctx, tx, caller); err != nil {
		if errors.Is(err, contracts.ErrCallerWithoutKey) {
			return problem.New(http.StatusForbidden,
				"this request carries no bearer key to revoke; revoke a key by id at /tokens/{id}/revoke")
		}
		return rest.Fault(err)
	}
	return nil
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
