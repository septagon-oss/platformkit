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
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Path is where this module's routes live.
const Path = "/api/v1/auth"

// RegisterRoutes mounts signing in and out, the caller's own identity, the
// three password routes and the two roles routes.
//
// All but the last two are about the caller themselves, which is why they
// declare no permission: the public ones are for somebody who cannot sign in,
// and the signed-in ones are about a person rather than a resource. The roles
// routes are the exception and say so with role:manage — a role is what
// everybody else in the tenant may do.
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
		// The verdict first, the sleep second, the transaction third, and that
		// order is the whole point of this being three statements.
		//
		// An account under a distributed attack earns a two-second pause
		// (contracts.SoftDelay). Taken inside the request's transaction — which
		// is where it used to be — those two seconds are two seconds holding
		// one of sixteen pool connections: twenty-four delayed logins from one
		// address held sixteen of a replica's seventeen, a legitimate request
		// waited twenty-nine seconds, and the background jobs starved. The
		// limiter is memory, so nothing has to be open to ask it, and a request
		// that is asleep holds a goroutine and nothing else.
		r, _ := httpx.RequestFrom(ctx)
		from := ClientOf(r)
		if svc.Precheck(ctx, in.Body.Email, from.IP) == contracts.Delay {
			pause(ctx, contracts.SoftDelay)
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		session, identity, err := svc.Login(ctx, tx, in.Body.Email, in.Body.Password, from)
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
				return nil, rest.Fault(err)
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
			return nil, rest.Fault(err)
		}
		return &identityOutput{Body: identity}, nil
	})

	httpx.Register(api, huma.Operation{
		OperationID: "auth-password-change",
		Method:      http.MethodPost,
		Path:        Path + "/password",
		Summary:     "Change my password",
		Description: "Requires the password in force. Every other session of this person ends; the one making the request does not, so changing a password does not sign you out of the page you changed it on.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusUnauthorized, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
		Extensions:  map[string]any{httpx.EventsExtension: []string{usercontracts.EventPasswordSet}},
	}, httpx.SignedIn(), func(ctx context.Context, in *changeInput) (*doneOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		p, ok := tenancy.PrincipalFrom(ctx)
		if !ok || p.UserID == uuid.Nil {
			return nil, problem.New(http.StatusForbidden, "this operation answers for the caller themselves")
		}
		// The session to keep is the caller's own, read off the cookie the
		// kernel already recognised. A caller who arrived some other way keeps
		// nothing, which is the safe direction.
		keep, _ := sessionOf(ctx)
		if err := svc.ChangePassword(ctx, tx, p.UserID, keep, in.Body.Current, in.Body.New); err != nil {
			return nil, refusal(err)
		}
		return done(), nil
	})

	httpx.Register(api, huma.Operation{
		OperationID: "auth-password-forgot",
		Method:      http.MethodPost,
		Path:        Path + "/password/forgot",
		Summary:     "Send me a reset link",
		Description: "An address nobody has and an address somebody has are the same answer and the same work: this route publishes one event and the worker decides whether anybody is there, so neither the body nor a stopwatch tells them apart. The mail that does not arrive is the message. The 429 is about the address asking, never the address asked about.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusTooManyRequests, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventResetRequested,
		}},
	}, httpx.Public(), func(ctx context.Context, in *forgotInput) (*doneOutput, error) {
		// The cap is on the address making the request and is checked before
		// the transaction, for the reason the login delay is: a public route
		// that costs a mail needs a limit, and twenty-three requests were
		// twenty-three mails to somebody who asked for none. It cannot be a
		// limit on the address asked about — that would be the oracle this
		// route exists not to be.
		r, _ := httpx.RequestFrom(ctx)
		if !svc.MayAsk(ctx, ClientOf(r).IP) {
			return nil, problem.New(http.StatusTooManyRequests,
				"too many reset requests from this address; wait and try again")
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		if err := svc.Forget(ctx, tx, in.Body.Email); err != nil {
			return nil, rest.Fault(err)
		}
		return done(), nil
	})

	httpx.Register(api, huma.Operation{
		OperationID: "auth-password-reset",
		Method:      http.MethodPost,
		Path:        Path + "/password/reset",
		Summary:     "Set a password with a link",
		Description: "Consumes the token the link carried and sets the password. Every session this person had ends, including any the caller holds. A token that is unknown, spent or expired is one answer.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusUnauthorized, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventPasswordReset, usercontracts.EventPasswordSet,
		}},
	}, httpx.Public(), func(ctx context.Context, in *resetInput) (*clearOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		if err := svc.Reset(ctx, tx, in.Body.Token, in.Body.New); err != nil {
			return nil, refusal(err)
		}
		// The cookie goes too. Every session ended, so one left in the browser
		// is a credential that names nothing, and clearing it is what makes the
		// next page load a sign-in rather than a silent 403.
		out := &clearOutput{SetCookie: cookies.Clear()}
		out.Body.SignedOut = true
		return out, nil
	})

	httpx.Register(api, huma.Operation{
		OperationID: "auth-role-list",
		Method:      http.MethodGet,
		Path:        Path + "/roles",
		Summary:     "List this tenant's roles",
		Description: "What every role name grants here, which is what everybody holding one may do.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusServiceUnavailable},
	}, httpx.Permission(contracts.PermissionRoleManage), func(ctx context.Context, _ *struct{}) (*rolesOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		roles, err := svc.Roles(ctx, tx)
		if err != nil {
			return nil, rest.Fault(err)
		}
		out := &rolesOutput{}
		out.Body.Items, out.Body.Total = roles, len(roles)
		return out, nil
	})

	httpx.Register(api, huma.Operation{
		OperationID: "auth-role-set",
		Method:      http.MethodPut,
		Path:        Path + "/roles/{name}",
		Summary:     "Set what a role grants",
		Description: "Creates the role if it is new. Every permission has to be one some module defines, and an operator permission is refused outside the operator's own tenant: both would otherwise be grants that look like authority and are not.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventRoleSet}},
	}, httpx.Permission(contracts.PermissionRoleManage), func(ctx context.Context, in *roleInput) (*roleOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		// The catalogue comes from the kernel, which read it off every
		// manifest before any route was registered. Asking each module would
		// make this one know its neighbours; asking the kernel makes it know
		// only that there is a list. See httpx.API.Declare.
		role, err := svc.SetRole(ctx, tx, in.Name, in.Body.Permissions, api.Permissions())
		return &roleOutput{Body: role}, rest.Fault(err)
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

type rolesOutput struct {
	Body struct {
		Items []*contracts.Role `json:"items"`
		Total int               `json:"total"`
	}
}
