package internal

// handlers.go holds one function per route of handler.go's list: the operation is
// the declaration, this is what answers it. Each returns the handler the kernel
// calls, closed over the service and the cookie names the module was composed
// with, so the list reads as eight addresses rather than eight pages.

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// asked puts one of this module's own caps on the answer that cap decided, and says
// whether the attempt may go on.
//
// The kernel's anonymous-write limit already states itself on every public write, and
// states nothing about the counters this module spends: a forgotten-password request is
// capped twice over, by two counters, and the one that decided it is the one the caller
// has to be told about — the name under `RateLimit-Policy`, the attempts left under it,
// and the wait before either moves. A route that refused on a counter and said nothing
// left a caller with a 429 and a guess, which is the retry traffic the refusal exists to
// prevent.
//
// The numbers are the deciding statement's own (kit/limit answers in one call for that
// reason), so nothing here re-counts a row other requests have moved since. A counter
// that could not be reached allows the attempt and states no limit: Limiter's stated
// failure mode, restated rather than reinvented here.
func asked(ctx context.Context, d contracts.Decision, sentence string) error {
	if !d.Decided() {
		return nil
	}
	httpx.SayCounted(ctx, httpx.Counted{
		Name:      d.Policy.Name,
		Quota:     d.Policy.Quota,
		Window:    d.Policy.Window,
		Remaining: d.Remaining,
		Left:      d.Left,
		Refused:   !d.Allowed,
	})
	if d.Allowed {
		return nil
	}
	return problem.New(http.StatusTooManyRequests, sentence)
}

// sayLockout puts the lockout on the answer it decided, and is the same rule as asked
// spent on the two mail caps: a refusal this module counted has to name the counter it
// counted, the quota left under it and the wait before either moves, because a 429 that
// says none of those leaves the caller to retry on a guess.
//
// It is called by the command rather than by the route, because the command is where
// the counter is read: the route sees ErrTooManyAttempts and nothing else, and a
// reading taken after that answer is a second look at a window other attempts have
// moved since. Both of this module's password doors — the sign-in and the second-factor
// leg — spend the one counter and so state the one policy.
//
// A counter that could not be read reports no Decision and this says nothing at all:
// the attempt was allowed on Limiter's stated failure mode, and the only numbers on
// offer are ones nobody read.
func sayLockout(ctx context.Context, d contracts.Decision, refused bool) {
	if !d.Decided() {
		return
	}
	httpx.SayCounted(ctx, httpx.Counted{
		Name:      d.Policy.Name,
		Quota:     d.Policy.Quota,
		Window:    d.Policy.Window,
		Remaining: d.Remaining,
		Left:      d.Left,
		Refused:   refused,
	})
}

func handleLogin(svc contracts.PasswordLogin, cookies Cookies) func(context.Context, *loginInput) (*sessionOutput, error) {
	return func(ctx context.Context, in *loginInput) (*sessionOutput, error) {
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
		// The kernel's cross-site check does not cover this route, and that is
		// correct in general and wrong here. It guards a request that carries a
		// session cookie, because a cookie is a credential the browser attached
		// on the caller's behalf; a sign-in carries none, so nothing was
		// attached. But a sign-in mints a credential rather than spending one:
		// another site can post a form here with its own account's password and
		// leave the visitor signed in as somebody else, and then read whatever
		// the visitor does next — a document they upload, an address they
		// enter. So this route asks the same question the middleware asks.
		//
		// The session cookie's SameSite=Lax is the layer underneath: a cookie
		// this response sets is stored, and the browser will not send it back on
		// a cross-site subrequest, so a page that forced a sign-in cannot then
		// act as that session from its own origin. What Lax does not stop is the
		// visitor's own next top-level navigation to this site, which is exactly
		// the attack — hence the check here rather than the cookie alone.
		if !httpx.SameSite(r) {
			return nil, problem.New(http.StatusForbidden,
				"this sign-in came from another site; sign in from the page itself")
		}
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
			SetCookie: cookies.Session(session.ID, session.CreatedAt),
			Body:      identity,
		}, nil
	}
}

func handleLogout(svc contracts.SessionEnder, cookies Cookies) func(context.Context, *struct{}) (*clearOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*clearOutput, error) {
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
	}
}

func handleIdentity(svc contracts.IdentityReader) func(context.Context, *struct{}) (*identityOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*identityOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		id, ok := sessionOf(ctx)
		if !ok {
			// SignedIn was satisfied, so somebody is here — they simply arrived
			// with a bearer key rather than a cookie session. This route reports
			// a cookie session: its body is the person's roles and everything
			// those grant, which is wider than a scoped key is allowed to act as,
			// and answering a narrowed caller with the full list would be a
			// /me that lies about the caller's own authority. A scoped caller
			// gets the routes its scopes name; a "who am I, as this key" answer
			// is named as open in the module README rather than approximated here.
			return nil, problem.New(http.StatusForbidden, "this operation answers for a cookie session")
		}
		r, _ := httpx.RequestFrom(ctx)
		identity, err := svc.Identify(ctx, tx, id, ClientOf(r))
		if err != nil {
			return nil, rest.Fault(err)
		}
		return &identityOutput{Body: identity}, nil
	}
}

func handleChangePassword(svc contracts.PasswordChanger) func(context.Context, *changeInput) (*doneOutput, error) {
	return func(ctx context.Context, in *changeInput) (*doneOutput, error) {
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
	}
}

func handleForgotPassword(svc contracts.ResetRequester) func(context.Context, *forgotInput) (*doneOutput, error) {
	return func(ctx context.Context, in *forgotInput) (*doneOutput, error) {
		// The cap is on the address making the request and is checked before
		// the transaction, for the reason the login delay is: a public route
		// that costs a mail needs a limit, and twenty-three requests were
		// twenty-three mails to somebody who asked for none. It cannot be a
		// limit on the address asked about — that would be the oracle this
		// route exists not to be.
		r, _ := httpx.RequestFrom(ctx)
		if err := asked(ctx, svc.MayAsk(ctx, ClientOf(r).IP),
			"too many reset requests from this address; wait and try again"); err != nil {
			return nil, err
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		if err := svc.Forget(ctx, tx, in.Body.Email); err != nil {
			return nil, rest.Fault(err)
		}
		return done(), nil
	}
}

func handleResetPassword(svc contracts.PasswordResetter, cookies Cookies) func(context.Context, *resetInput) (*clearOutput, error) {
	return func(ctx context.Context, in *resetInput) (*clearOutput, error) {
		// Asking for a link is capped and so is spending one, before the
		// transaction and for the same reason: a public route that a stranger
		// can call in a loop needs a limit whether or not the thing it checks is
		// hard to guess.
		r, _ := httpx.RequestFrom(ctx)
		// The same question the sign-in route asks, for the same reason, and it is
		// asked here rather than not at all because this route now hands out the
		// cookie too. A page on another site cannot mint a session here without a
		// token it cannot get, but it can spend one a visitor already has open in a
		// tab — and a forced sign-in that leaves the visitor somebody else is the
		// attack the check exists for. See handleLogin for the whole of it.
		if !httpx.SameSite(r) {
			return nil, problem.New(http.StatusForbidden,
				"this request came from another site; set the password from the page itself")
		}
		if err := asked(ctx, svc.MayRedeem(ctx, ClientOf(r).IP),
			"too many reset attempts from this address; wait and try again"); err != nil {
			return nil, err
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		session, err := svc.Reset(ctx, tx, in.Body.Token, in.Body.New, ClientOf(r))
		if err != nil {
			return nil, refusal(err)
		}
		out := &clearOutput{}
		// Every session this person held before the link was spent is gone, in
		// both answers: that is what spending the link does, and it is what
		// signedOut has always reported here.
		out.Body.SignedOut = true
		if session == nil {
			// The password is set and every session this person held is gone, and
			// this browser holds none: the account also answers with a second
			// factor, which a mailed link does not supply. The cookie goes, because
			// one left behind names a session that no longer exists, and the page
			// sends the person to the sign-in form to finish the sign-in there.
			out.SetCookie = cookies.Clear()
			return out, nil
		}
		out.SetCookie = cookies.Session(session.ID, session.CreatedAt)
		out.Body.SignedIn = true
		return out, nil
	}
}

// handleListSessions answers for the caller themselves, so it takes no
// permission and no argument: who is asking comes from the principal the kernel
// resolved, and the sessions that come back are that person's own — which is a
// row-level-security fact, not a filter written here.
func handleListSessions(svc contracts.SessionReader) func(context.Context, *struct{}) (*sessionsOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*sessionsOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		caller, ok := tenancy.PrincipalFrom(ctx)
		if !ok || caller.UserID == uuid.Nil {
			return nil, problem.New(http.StatusForbidden, "this operation answers for the caller themselves")
		}
		current, _ := sessionOf(ctx)
		items, err := svc.Sessions(ctx, tx, caller.UserID, current)
		if err != nil {
			return nil, rest.Fault(err)
		}
		out := &sessionsOutput{}
		out.Body.Items, out.Body.Total = items, len(items)
		return out, nil
	}
}

// handleRevokeSession ends one session named by its ref. The caller's own
// sessions are the only ones the command can see, so a ref copied from another
// person's list is a 404 here and not a 403 — the refusal does not confirm that
// the ref was somebody's.
func handleRevokeSession(svc contracts.SessionRevoker) func(context.Context, *revokeSessionInput) (*doneOutput, error) {
	return func(ctx context.Context, in *revokeSessionInput) (*doneOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		caller, ok := tenancy.PrincipalFrom(ctx)
		if !ok || caller.UserID == uuid.Nil {
			return nil, problem.New(http.StatusForbidden, "this operation answers for the caller themselves")
		}
		if err := svc.RevokeSession(ctx, tx, caller.UserID, in.Ref); err != nil {
			return nil, rest.Fault(err)
		}
		return done(), nil
	}
}

// handleRevokeAllSessions ends everything and clears the cookie: the caller
// asked for every machine out, and a browser left holding a session that has
// just been deleted is a page that 403s instead of signing in again.
func handleRevokeAllSessions(svc contracts.AllSessionsRevoker, cookies Cookies) func(context.Context, *struct{}) (*clearOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*clearOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		caller, ok := tenancy.PrincipalFrom(ctx)
		if !ok || caller.UserID == uuid.Nil {
			return nil, problem.New(http.StatusForbidden, "this operation answers for the caller themselves")
		}
		if _, err := svc.RevokeAllSessions(ctx, tx, caller.UserID); err != nil {
			return nil, rest.Fault(err)
		}
		out := &clearOutput{SetCookie: cookies.Clear()}
		out.Body.SignedOut = true
		return out, nil
	}
}

func handleListRoles(svc contracts.RoleReader) func(context.Context, *struct{}) (*rolesOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*rolesOutput, error) {
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
	}
}

func handleSetRole(svc contracts.RoleWriter, surfaces httpx.Surfaces) func(context.Context, *roleInput) (*roleOutput, error) {
	return func(ctx context.Context, in *roleInput) (*roleOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		// The catalogue comes from the kernel, which read it off every
		// manifest before any route was registered. Asking each module would
		// make this one know its neighbours; asking the kernel makes it know
		// only that there is a list. See httpx.API.Declare.
		role, err := svc.SetRole(ctx, tx, in.Name, in.Body.Permissions, surfaces.Permissions())
		return &roleOutput{Body: role}, rest.Fault(err)
	}
}
