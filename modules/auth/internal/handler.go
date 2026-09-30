package internal

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Every path this module mounts is relative to itself and to the surface it
// chose. The workspace's JSON lands at /api/v1/auth/… as it always did, and the
// anonymous legs — the ones a person with no account makes — land under
// /api/v1/public/auth/…, which is where every public door answers. Neither
// address is written here.
// RegisterRoutes mounts signing in and out, the caller's own identity, the
// caller's own sessions, the three password routes and the two roles routes.
//
// All but the last two are about the caller themselves, which is why they
// declare no permission: the public ones are for somebody who cannot sign in,
// and the signed-in ones are about a person rather than a resource. The roles
// routes are the exception and say so with role:manage — a role is what
// everybody else in the tenant may do.
func RegisterRoutes(surfaces httpx.Surfaces, svc contracts.Service, cookies Cookies) {
	app, public := surfaces.App, surfaces.Public
	httpx.Register(app, huma.Operation{
		OperationID: "auth-login",
		Method:      http.MethodPost,
		Path:        "/login",
		Summary:     "Sign in with a password",
		Description: "Opens a session and sets the platformkit_session cookie. A wrong password and an address nobody has answer identically, and cost the same.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable},
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventLoggedIn, contracts.EventLoginFailed}},
	}, httpx.Public(), handleLogin(svc, cookies))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-logout",
		Method:      http.MethodPost,
		Path:        "/logout",
		Summary:     "Sign out",
		Description: "Deletes the session and clears the cookie. Signing out when already signed out is not an error.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusServiceUnavailable},
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventLoggedOut}},
	}, httpx.SignedIn(), handleLogout(svc, cookies))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-me",
		Method:      http.MethodGet,
		Path:        "/me",
		Summary:     "Who am I",
		Description: "The caller's identity and the permissions their roles grant in this tenant.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusServiceUnavailable},
	}, httpx.SignedIn(), handleIdentity(svc))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-password-change",
		Method:      http.MethodPost,
		Path:        "/password",
		Summary:     "Change my password",
		Description: "Requires the password in force. Every other session of this person ends; the one making the request does not, so changing a password does not sign you out of the page you changed it on.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusUnauthorized, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			usercontracts.EventPasswordSet, contracts.EventSessionRevoked,
		}},
	}, httpx.SignedIn(), handleChangePassword(svc))

	httpx.Register(public, huma.Operation{
		OperationID: "auth-password-forgot",
		Method:      http.MethodPost,
		Path:        "/password/forgot",
		Summary:     "Send me a reset link",
		Description: "An address nobody has and an address somebody has are the same answer and the same work: this route publishes one event and the worker decides whether anybody is there, so neither the body nor a stopwatch tells them apart. The mail that does not arrive is the message. The 429 is about the address asking, never the address asked about.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusTooManyRequests, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventResetRequested,
		}},
	}, httpx.Public(), handleForgotPassword(svc))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-password-reset",
		Method:      http.MethodPost,
		Path:        "/password/reset",
		Summary:     "Set a password with a link",
		Description: "Consumes the token the link carried and sets the password. Every session this person had ends, including any the caller holds. A token that is unknown, spent or expired is one answer.",
		Tags:        []string{"auth"},
		Errors: []int{http.StatusUnauthorized, http.StatusUnprocessableEntity,
			http.StatusTooManyRequests, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventPasswordReset, usercontracts.EventPasswordSet,
			contracts.EventSessionRevoked,
		}},
	}, httpx.Public(), handleResetPassword(svc, cookies))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-session-list",
		Method:      http.MethodGet,
		Path:        "/sessions",
		Summary:     "List my sessions",
		Description: "Every live session this person has, most recently seen first, with the browser and address each was opened with and the one making the request marked. A session is named by its ref and never by its id: this list is something a person reads, not something a client presents.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusServiceUnavailable},
	}, httpx.SignedIn(), handleListSessions(svc))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-session-revoke",
		Method:      http.MethodPost,
		Path:        "/sessions/{ref}/revoke",
		Summary:     "Revoke one of my sessions",
		Description: "Ends the session this person names by its ref. A ref that is not one of their live sessions is a 404 whether it was never there, is somebody else's, or belongs to another tenant. POST rather than DELETE so the page's form can make the write as a CSRF-covered request.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusNotFound, http.StatusServiceUnavailable},
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventSessionRevoked}},
	}, httpx.SignedIn(), handleRevokeSession(svc))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-session-revoke-all",
		Method:      http.MethodPost,
		Path:        "/sessions/revoke-all",
		Summary:     "Sign out everywhere",
		Description: "Ends every session this person has, including the one making the request, and clears the cookie, so the browser lands on the sign-in page rather than holding a credential that names nothing.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusServiceUnavailable},
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventSessionRevoked}},
	}, httpx.SignedIn(), handleRevokeAllSessions(svc, cookies))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-role-list",
		Method:      http.MethodGet,
		Path:        "/roles",
		Summary:     "List this tenant's roles",
		Description: "What every role name grants here, which is what everybody holding one may do.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusServiceUnavailable},
	}, httpx.Permission(contracts.PermissionRoleManage), handleListRoles(svc))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-role-set",
		Method:      http.MethodPut,
		Path:        "/roles/{name}",
		Summary:     "Set what a role grants",
		Description: "Creates the role if it is new. Every permission has to be one some module defines, and an operator permission is refused outside the operator's own tenant: both would otherwise be grants that look like authority and are not.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventRoleSet}},
	}, httpx.Permission(contracts.PermissionRoleManage), handleSetRole(svc, surfaces))
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
	cookie, ok := httpx.SessionCookieOf(r)
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(cookie.Value)
	return id, err == nil
}

// pause is the soft delay, interruptible: a shutdown must not wait two seconds
// per request in flight. It is here rather than in the service because it is
// the handler that has to take it — before the transaction, see the login
// route.
func pause(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// done is the answer to a command with nothing to report: a body that says it
// worked rather than an empty one, the shape clearOutput already uses.
func done() *doneOutput {
	out := &doneOutput{}
	out.Body.Done = true
	return out
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
	return rest.Fault(err)
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

type changeInput struct {
	Body struct {
		Current string `json:"current" maxLength:"256" doc:"The password in force"`
		New     string `json:"new" minLength:"12" maxLength:"256" doc:"The new password; at least twelve characters"`
	} `required:"true"`
}

type forgotInput struct {
	Body struct {
		Email string `json:"email" format:"email" maxLength:"320" doc:"The address to send a reset link to"`
	} `required:"true"`
}

type resetInput struct {
	Body struct {
		Token string `json:"token" maxLength:"128" doc:"The token the link carried"`
		New   string `json:"new" minLength:"12" maxLength:"256" doc:"The new password; at least twelve characters"`
	} `required:"true"`
}

// doneOutput is the answer to a command with nothing to report. A body that
// says it worked beats an empty one, for the reason clearOutput's does.
type doneOutput struct {
	Body struct {
		Done bool `json:"done"`
	}
}

type roleInput struct {
	Name string `path:"name" maxLength:"64" doc:"The role's name, a lower-case identifier"`
	Body struct {
		Permissions []string `json:"permissions" doc:"What this role grants from now on" example:"task:read"`
	} `required:"true"`
}

type roleOutput struct {
	Body *contracts.Role
}

// sessionsOutput is the caller's own list: items and total, the shape every
// other list route here answers with.
type sessionsOutput struct {
	Body struct {
		Items []*contracts.SessionListing `json:"items"`
		Total int                         `json:"total"`
	}
}

type revokeSessionInput struct {
	Ref string `path:"ref" minLength:"64" maxLength:"64" doc:"The session's ref, as the list shows it. Not the session id, which is the cookie credential and never appears in a response."`
}

type rolesOutput struct {
	Body struct {
		Items []*contracts.Role `json:"items"`
		Total int               `json:"total"`
	}
}
