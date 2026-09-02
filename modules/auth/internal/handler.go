package internal

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// Path is where this module's routes live.
const Path = "/api/v1/auth"

// RegisterRoutes mounts login, logout and me.
//
// All three are about the caller themselves, which is why none of them declares
// a permission: login is Public because somebody who is not signed in is the
// only caller it is for, and the other two are SignedIn because there is no
// resource to name a permission on.
func RegisterRoutes(api *httpx.API, svc contracts.Service, cookies Cookies) {
	httpx.Register(api, huma.Operation{
		OperationID: "auth-login",
		Method:      http.MethodPost,
		Path:        Path + "/login",
		Summary:     "Sign in with a password",
		Description: "Opens a session and sets the platformkit_session cookie. A wrong password and an address nobody has answer identically, and cost the same.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable},
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventLoggedIn, contracts.EventLoginFailed}},
	}, httpx.Public(), func(ctx context.Context, in *loginInput) (*sessionOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		r, _ := httpx.RequestFrom(ctx)
		session, identity, err := svc.Login(ctx, tx, in.Body.Email, in.Body.Password, ClientOf(r))
		if err != nil {
			return nil, refusal(err)
		}
		return &sessionOutput{
			SetCookie: cookies.Session(session.ID, session.ExpiresAt),
			Body:      identity,
		}, nil
	})

	httpx.Register(api, huma.Operation{
		OperationID: "auth-logout",
		Method:      http.MethodPost,
		Path:        Path + "/logout",
		Summary:     "Sign out",
		Description: "Deletes the session and clears the cookie. Signing out when already signed out is not an error.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusServiceUnavailable},
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventLoggedOut}},
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*clearOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		if id, ok := sessionOf(ctx); ok {
			if err := svc.Logout(ctx, tx, id); err != nil {
				return nil, crud.Fault(err)
			}
		}
		out := &clearOutput{SetCookie: cookies.Clear()}
		out.Body.SignedOut = true
		return out, nil
	})

	httpx.Register(api, huma.Operation{
		OperationID: "auth-me",
		Method:      http.MethodGet,
		Path:        Path + "/me",
		Summary:     "Who am I",
		Description: "The caller's identity and the permissions their roles grant in this tenant.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusServiceUnavailable},
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*identityOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		id, ok := sessionOf(ctx)
		if !ok {
			// SignedIn was satisfied, so somebody is here; they arrived with
			// something other than a session cookie, which nothing issues yet.
			return nil, problem.New(http.StatusForbidden, "this operation answers for a cookie session")
		}
		r, _ := httpx.RequestFrom(ctx)
		identity, err := svc.Identify(ctx, tx, id, ClientOf(r))
		if err != nil {
			return nil, crud.Fault(err)
		}
		return &identityOutput{Body: identity}, nil
	})
}

// sessionOf is the session id the caller presented, read back off the request
// the kernel carried down. The cookie was already parsed once, in Authenticate;
// it is parsed again here rather than smuggled through Principal, because a
// principal is who the caller is and not how they proved it.
func sessionOf(ctx context.Context) (uuid.UUID, bool) {
	r, ok := httpx.RequestFrom(ctx)
	if !ok {
		return uuid.Nil, false
	}
	cookie, err := r.Cookie(httpx.SessionCookie)
	if err != nil {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(cookie.Value)
	return id, err == nil
}

// refusal is the one mapping for a login that did not work: 401 for credentials
// that are wrong, 429 for an address that has been tried too often, and the
// caller's own error for anything else — which is an outage and reads as 500.
func refusal(err error) error {
	switch {
	case errors.Is(err, contracts.ErrCredentials):
		return problem.New(http.StatusUnauthorized, "those credentials are not right")
	case errors.Is(err, contracts.ErrTooManyAttempts):
		return problem.New(http.StatusTooManyRequests, "too many failed attempts for that address; wait and try again")
	}
	return crud.Fault(err)
}

// transaction is the request's, or a 503 saying why there is none.
func transaction(ctx context.Context) (db.Tx[db.Tenant], error) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return tx, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
	}
	return tx, nil
}

type loginInput struct {
	Body struct {
		Email    string `json:"email" format:"email" maxLength:"320" doc:"The address to sign in as"`
		Password string `json:"password" maxLength:"256" doc:"The password"`
	} `required:"true"`
}

type sessionOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      *contracts.Identity
}

// clearOutput is the answer to signing out: the cookie removed, and a body
// that says so rather than an empty one. There is nothing else to report —
// signing out when already signed out is the same answer, because the caller
// wanted to be signed out and they are.
type clearOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      struct {
		SignedOut bool `json:"signedOut"`
	}
}

type identityOutput struct {
	Body *contracts.Identity
}
