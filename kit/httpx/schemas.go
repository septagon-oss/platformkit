package httpx

// schemas.go is the other half of a resource's registration: the routes go to
// huma, and the resource itself is recorded here, so that something which did
// not compile against the entity can still list, read and write it.
//
// It is what makes ARCHITECTURE.md's eighth idea reachable. kit/rest already
// derived a schema from every Spec, and until now nothing read it — the screens
// that were supposed to be generated from it did not exist yet. This is the
// register they are generated from, and modules/admin is its one reader.
//
// The kernel stays ignorant of kit/rest: a Resource is declared here, kit/rest
// fills one in, and nothing in this package imports it back.

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Resource is one entity as a screen sees it: what it is called, where its API
// lives, which permissions guard it, what shape it has, and the five operations
// bound to its type.
//
// The operations are closures because generics do not survive the trip: a
// screen knows a resource by name and cannot name its Go type, so the type is
// closed over at registration instead. Each runs inside the request's own
// transaction, which it takes from the context — a screen holds no capability
// the route beside it does not.
//
// Rows are maps because that is what a schema-driven screen renders: a column
// is a field name from Schema, and a value is whatever the entity's own JSON
// says it is. There is no second serialization; it is encoding/json, once.
type Resource struct {
	Module, Entity, Path string
	// Read and Write are the permissions the Spec declared. A screen carries
	// the same ones, so a person who cannot use the API cannot use the screen.
	Read, Write string
	// Immutable are the fields a command owns, shown read-only in a form.
	Immutable []string
	Schema    crud.Schema

	List   func(ctx context.Context, q crud.Query) ([]map[string]any, int64, error)
	Get    func(ctx context.Context, id uuid.UUID) (map[string]any, error)
	Create func(ctx context.Context, values map[string]any) (map[string]any, error)
	Update func(ctx context.Context, id uuid.UUID, values map[string]any) (map[string]any, error)
	Delete func(ctx context.Context, id uuid.UUID) error

	// may is the authorization the five closures above carry, installed by
	// RegisterResource. It is unexported because only this package may fill it
	// in: a Resource built anywhere else is unguarded until it is registered,
	// and registering is the only way anything can obtain one.
	may func(ctx context.Context, permission string) error
}

// Readable reports whether the caller in ctx holds this resource's Read
// permission. A page that lists resources — the dashboard — asks it before it
// renders a card, so a person is not shown a count of something they may not
// look at. It is the same question the closures ask; this is only the form that
// answers without producing an error to swallow.
func (r Resource) Readable(ctx context.Context) bool { return r.allowed(ctx, r.Read) }

// Writable reports whether the caller in ctx holds this resource's Write
// permission.
func (r Resource) Writable(ctx context.Context) bool { return r.allowed(ctx, r.Write) }

func (r Resource) allowed(ctx context.Context, permission string) bool {
	return r.may != nil && r.may(ctx, permission) == nil
}

// RegisterResource records a resource, with this API's authorization wrapped
// around each of its five operations. kit/rest calls it from Spec.Mount, in the
// same breath as the routes, so a resource and its API cannot disagree about a
// permission or a path.
//
// The wrapping is here rather than in kit/rest because this is where the
// Authorizer is: a Resource is the entity without its routes, and the routes
// are where the permission used to live. A hand-written page holds a Resource
// and calls List on it directly — the dashboard does — so a closure that did
// not ask would be a page that reads past the permission whenever whoever wrote
// it forgot to. Now forgetting is not available.
func (a *API) RegisterResource(r Resource) {
	r = a.guard(r)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resources = append(a.resources, r)
}

// guard returns r with its five closures behind the two permissions it
// declares: Read for the list and the read, Write for the three writes. The
// same pairing the routes declare, from the same two fields.
func (a *API) guard(r Resource) Resource {
	list, get, create, update, remove := r.List, r.Get, r.Create, r.Update, r.Delete
	r.may = a.may
	if list != nil {
		r.List = func(ctx context.Context, q crud.Query) ([]map[string]any, int64, error) {
			if err := a.may(ctx, r.Read); err != nil {
				return nil, 0, err
			}
			return list(ctx, q)
		}
	}
	if get != nil {
		r.Get = func(ctx context.Context, id uuid.UUID) (map[string]any, error) {
			if err := a.may(ctx, r.Read); err != nil {
				return nil, err
			}
			return get(ctx, id)
		}
	}
	if create != nil {
		r.Create = func(ctx context.Context, values map[string]any) (map[string]any, error) {
			if err := a.may(ctx, r.Write); err != nil {
				return nil, err
			}
			return create(ctx, values)
		}
	}
	if update != nil {
		r.Update = func(ctx context.Context, id uuid.UUID, values map[string]any) (map[string]any, error) {
			if err := a.may(ctx, r.Write); err != nil {
				return nil, err
			}
			return update(ctx, id, values)
		}
	}
	if remove != nil {
		r.Delete = func(ctx context.Context, id uuid.UUID) error {
			if err := a.may(ctx, r.Write); err != nil {
				return err
			}
			return remove(ctx, id)
		}
	}
	return r
}

// may asks the same Authorizer the middleware asks, in the tenant the request
// resolved to, about the caller the request was recognised as. The refusals are
// the middleware's: 403 for a caller who may not, 503 for a decision that could
// not be made, because sending somebody away from work they are entitled to do
// is worse than telling them to try again.
func (a *API) may(ctx context.Context, permission string) error {
	t, hasTenant := tenancy.FromContext(ctx)
	if !hasTenant {
		return problem.New(http.StatusForbidden, "AUTH_NO_TENANT: this is tenant work and the host resolved to none")
	}
	p, hasPrincipal := tenancy.PrincipalFrom(ctx)
	if !hasPrincipal || p.UserID == uuid.Nil {
		return problem.New(http.StatusForbidden, "AUTH_ANONYMOUS: this requires a signed-in caller")
	}
	allowed, err := a.opts.Authorize.Allowed(ctx, t, tenancy.Grant{Permission: permission})
	if err != nil {
		a.rlog(ctx).ErrorContext(ctx, "httpx: authorization decision unavailable",
			"permission", permission, "tenant", t.Slug, "error", err)
		return problem.New(http.StatusServiceUnavailable, "authorization is temporarily unavailable")
	}
	if !allowed {
		return problem.New(http.StatusForbidden, "AUTH_DENIED: this requires "+permission)
	}
	return nil
}

// Resources is every registered resource, in mount order. modules/admin reads
// it in Routes, which is why the shell is composed last: a module that mounts
// after it registers a resource no screen was generated for.
func (a *API) Resources() []Resource {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Resource(nil), a.resources...)
}

// SignInExtension is where an operation names the form an anonymous caller
// should be sent to instead of being refused.
//
// It is an operation's own declaration rather than an option of the API,
// because "there is a login page and it is at /admin/login" is knowledge the
// module that serves that page has and the kernel does not. modules/admin
// writes it on every HTML page it mounts; nothing else writes it, and an
// application with no shell has no redirect and no line of configuration
// saying so.
const SignInExtension = "x-platformkit-signin"

// SignIn declares that this operation is a page and names the sign-in form.
// It is written into op.Extensions, beside the authorization, so a reviewer
// reading /openapi.json sees both.
func SignIn(op *huma.Operation, path string) {
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions[SignInExtension] = path
}

// signInFor is where an anonymous caller of this request should be sent instead
// of being refused, and whether there is anywhere at all.
//
// Three conditions, and each is load-bearing. A GET, because a redirect is not
// an answer to a write. An operation that named a form, because the kernel
// knows of none. And a caller that asked for HTML, because a program that gets
// a 303 to a login page where it expected a 403 has to guess what happened —
// the JSON routes keep problem+json exactly as they are.
//
// The next parameter is this request's own path, which is where the guard is:
// it is a value the router produced, not one the caller sent, and it is escaped
// on the way out. modules/admin's login page validates it a second time, which
// is the one that matters, because that page is also reachable directly.
func signInFor(ctx huma.Context) (string, bool) {
	op := ctx.Operation()
	if ctx.Method() != http.MethodGet || op == nil || op.Extensions == nil {
		return "", false
	}
	to, ok := op.Extensions[SignInExtension].(string)
	if !ok || to == "" || !strings.Contains(ctx.Header("Accept"), "text/html") {
		return "", false
	}
	here := ctx.URL().Path
	if here == to {
		return "", false
	}
	if here != "" {
		to += "?next=" + url.QueryEscape(here)
	}
	return to, true
}
