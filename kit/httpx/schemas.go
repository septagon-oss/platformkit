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
// fills one in, and nothing in this package imports it back. The shape a
// resource has is kit/entity's (Schema, Field); the one thing read from
// kit/crud is Query, the page a List asks for, because a list request is the
// storage adapter's contract and a screen fills it in the adapter's terms.

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Resource is one entity as a screen sees it: what it is called, where its API
// lives, which permissions guard it, what shape it has, and its operations
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
	// Module and Entity name it; Path is the collection path as the module
	// wrote it, relative to itself ("/tasks"), which is what a generated screen
	// mounts and links from. The composed addresses are Schema.Path — the API's
	// own, on the surface the reads are on — and Screen below.
	Module, Entity, Path string
	// Screen is the workspace address of the generated screens: /app/<module>
	// /<entity>. The kernel composed it at registration, from the module, the
	// path and the surface a page can exist on, which is why neither the
	// generating shell nor the module that owns the resource has to know where
	// the workspace is mounted. Empty for a resource nobody may read from the
	// workspace, which is a resource with no screens.
	Screen string
	// WritePath is the composed address the resource's writes answer at, for the
	// one resource whose writes do not answer at Schema.Path: a price list is
	// read by the tenant that pays for it on the workspace surface and written by
	// the installation on the control plane, and nothing a caller can derive from
	// the read address says so. Empty means the writes answer at Schema.Path,
	// which is what nearly every resource's do — so a catalog entry that names no
	// write address is saying the obvious thing rather than omitting one.
	WritePath string
	// Read and Write are the permissions the Spec declared. A screen carries
	// the same ones, so a person who cannot use the API cannot use the screen.
	Read, Write string
	// OperatorRead restricts reads and discovery to the operator's own tenant.
	// It is independent of OperatorWrite; leaving it false keeps shared rows
	// such as prices readable by ordinary tenants.
	OperatorRead bool
	// OperatorWrite restricts writes to the operator's own tenant. Routes and
	// resource closures carry the same declaration, so customer wildcards
	// cannot bypass the operator boundary. See docs/adr/0008.
	OperatorWrite bool
	// Immutable are the fields a command owns, shown read-only in a form.
	Immutable []string
	Schema    entity.Schema
	// Commands are the lifecycle routes beyond the five, filled in by
	// Resources from what AddCommand recorded.
	Commands []Command
	// Singleton says a tenant has exactly one of these and it has no id in its
	// path: the API is a GET and a PUT on Path itself, with no list, no create
	// and no delete. A shell that did not know would render a collection whose
	// list is one row, offering doors — New, Delete — that no route serves.
	// See rest.Singleton.
	Singleton bool

	// Count reads the total without a page. Registration guards it with Read
	// and supplies a List-based fallback for resources with custom loading.
	Count  func(ctx context.Context) (int64, error)
	List   func(ctx context.Context, q crud.Query) ([]map[string]any, int64, error)
	Get    func(ctx context.Context, id uuid.UUID) (map[string]any, error)
	Create func(ctx context.Context, values map[string]any) (map[string]any, error)
	Update func(ctx context.Context, id uuid.UUID, values map[string]any) (map[string]any, error)
	Delete func(ctx context.Context, id uuid.UUID) error

	// may is the authorization the closures above carry, installed by
	// RegisterResource. It is unexported because only this package may fill it
	// in: a Resource built anywhere else is unguarded until it is registered,
	// and registering is the only way anything can obtain one.
	may func(ctx context.Context, g tenancy.Grant) error
}

// Command is one lifecycle route on a resource, as kit/rest.Command mounted
// it: a rule about the state a row is in, with an event of its own, which is
// what a generic form cannot express. Verb is the last path segment — POST
// {Path}/{id}/{Verb}, or {Path}/{Verb} when Collection — Summary and
// Description are the API document's own, Auth is who may call it (not always
// the resource's write permission), and Fields is the shape of its argument,
// derived from the request body the way an entity's is derived from T.
type Command struct {
	Verb                 string
	Summary, Description string
	Collection           bool
	Auth                 Auth
	// Endpoint is the command's absolute address as the kernel composed it:
	// /api/v1/ops/billing/subscription/cancel. A shell used to derive a
	// command's path from the resource's own — {path}/{id}/{verb} — and that
	// derivation broke the moment a command could live on the surface its
	// resource's reads do not. The catalog publishes it for those commands and
	// only those, so a document that could be derived still is.
	Endpoint string
	Fields   []entity.Field
	// Run performs the command with the values a caller submitted, inside the
	// request's own transaction, and is the closure the command's HTTP route
	// calls. It has to be here because docs/adr/0007 promises a derived screen
	// "calls the same closures the routes do, in the same request transaction …
	// there is no second implementation to keep honest", and until now a command
	// was the one thing a screen could advertise and not perform: recording the
	// verb, the guard and the fields without the work left a shell with no door
	// to offer and no way to say so. A Command whose Run is nil is a description
	// only, and a screen must not turn one into a button.
	//
	// The id is uuid.Nil for a Collection command. A command that takes no
	// arguments receives an empty values map and runs anyway.
	Run func(ctx context.Context, id uuid.UUID, values map[string]any) error
}

// CommandsFor is the commands this caller may call. One they may not is left
// out rather than shown disabled, for the reason an unreadable resource is
// left out: what somebody may not do, they are not told about.
//
// Each is answered the way the request middleware answers it — public admits
// everybody, signed-in a recognised caller, a permission the Authorizer's
// question — so the catalog cannot promise a door the API would refuse. An
// undeclared Auth admits nobody, as at the route.
func (r Resource) CommandsFor(ctx context.Context) []Command {
	var out []Command
	// One answer per distinct declaration, not per command: the commands of a
	// resource mostly share its write permission, and the Authorizer reads the
	// roles table every time it is asked.
	asked := map[Auth]bool{}
	for _, c := range r.Commands {
		may, seen := asked[c.Auth]
		if !seen {
			may = r.mayUse(ctx, c.Auth)
			asked[c.Auth] = may
		}
		if may {
			out = append(out, c)
		}
	}
	return out
}

func (r Resource) mayUse(ctx context.Context, a Auth) bool {
	if r.may == nil || !a.Declared() {
		return false
	}
	switch a.kind {
	case kindPublic:
		return true
	case kindSignedIn:
		return recognised(ctx)
	}
	g, asks := a.grant()
	return asks && r.allowed(ctx, g)
}

// Readable reports whether the caller in ctx holds this resource's Read
// permission. Use it to decide whether to show navigation; operations already
// carry their own guard and need no preceding Readable check.
func (r Resource) Readable(ctx context.Context) bool {
	return r.allowed(ctx, r.read())
}

// Writable reports whether the caller in ctx holds this resource's Write
// permission, in a tenant that may exercise it.
func (r Resource) Writable(ctx context.Context) bool { return r.allowed(ctx, r.write()) }

// ReadAuth carries the same read declaration as the guarded resource closures.
func (r Resource) ReadAuth() Auth {
	if r.OperatorRead {
		return OperatorPermission(r.Read)
	}
	return Permission(r.Read)
}

// WriteAuth is the declaration a page that mounts a write route carries, so a
// screen and the API it stands in front of cannot disagree about which kind of
// permission this is.
func (r Resource) WriteAuth() Auth {
	if r.OperatorWrite {
		return OperatorPermission(r.Write)
	}
	return Permission(r.Write)
}

func (r Resource) write() tenancy.Grant {
	return tenancy.Grant{Permission: r.Write, Operator: r.OperatorWrite}
}

func (r Resource) read() tenancy.Grant {
	return tenancy.Grant{Permission: r.Read, Operator: r.OperatorRead}
}

func (r Resource) allowed(ctx context.Context, g tenancy.Grant) bool {
	return r.may != nil && r.may(ctx, g) == nil
}

// RegisterResource records a resource, with this API's authorization wrapped
// around each of its operations. kit/rest calls it from Spec.Mount, in the
// same breath as the routes, so a resource and its API cannot disagree about a
// permission or a path.
//
// The wrapping is here rather than in kit/rest because this is where the
// Authorizer is: a Resource is the entity without its routes, and the routes
// are where the permission used to live. A hand-written page holds a Resource
// and calls an operation directly, so a closure that did
// not ask would be a page that reads past the permission whenever whoever wrote
// it forgot to. Now forgetting is not available.
func (a *API) RegisterResource(r Resource) {
	r = a.guard(r)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resources = append(a.resources, r)
}

// guard returns r with its closures behind the two permissions it
// declares: Read for count/list/get, Write for the three writes. The
// same pairing the routes declare, from the same two fields.
func (a *API) guard(r Resource) Resource {
	list, get, create, update, remove := r.List, r.Get, r.Create, r.Update, r.Delete
	r.may = a.may
	count := r.Count
	if count == nil && list != nil {
		count = func(ctx context.Context) (int64, error) {
			_, total, err := list(ctx, crud.Query{Limit: 1})
			return total, err
		}
	}
	if count != nil {
		r.Count = func(ctx context.Context) (int64, error) {
			if err := a.may(ctx, r.read()); err != nil {
				return 0, err
			}
			return count(ctx)
		}
	}
	if list != nil {
		r.List = func(ctx context.Context, q crud.Query) ([]map[string]any, int64, error) {
			if err := a.may(ctx, r.read()); err != nil {
				return nil, 0, err
			}
			return list(ctx, q)
		}
	}
	if get != nil {
		r.Get = func(ctx context.Context, id uuid.UUID) (map[string]any, error) {
			if err := a.may(ctx, r.read()); err != nil {
				return nil, err
			}
			return get(ctx, id)
		}
	}
	if create != nil {
		r.Create = func(ctx context.Context, values map[string]any) (map[string]any, error) {
			if err := a.may(ctx, r.write()); err != nil {
				return nil, err
			}
			return create(ctx, values)
		}
	}
	if update != nil {
		r.Update = func(ctx context.Context, id uuid.UUID, values map[string]any) (map[string]any, error) {
			if err := a.may(ctx, r.write()); err != nil {
				return nil, err
			}
			return update(ctx, id, values)
		}
	}
	if remove != nil {
		r.Delete = func(ctx context.Context, id uuid.UUID) error {
			if err := a.may(ctx, r.write()); err != nil {
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
func (a *API) may(ctx context.Context, g tenancy.Grant) error {
	t, hasTenant := tenancy.FromContext(ctx)
	if !hasTenant {
		return problem.New(http.StatusForbidden, "AUTH_NO_TENANT: this is tenant work and the host resolved to none")
	}
	if !recognised(ctx) {
		return problem.New(http.StatusForbidden, "AUTH_ANONYMOUS: this requires a signed-in caller")
	}
	// The operator check comes before the Authorizer, exactly as it does in the
	// request middleware: a customer's administrator holds the wildcard in
	// their own tenant, so asking the roles table first would be asking a
	// question whose answer is always yes.
	if g.Operator && !t.Operator {
		return problem.New(http.StatusForbidden, "AUTH_NOT_OPERATOR: "+g.Permission+" is the operator's, and this is not the operator's tenant")
	}
	allowed, err := a.opts.Authorize.Allowed(ctx, t, g)
	if err != nil {
		a.rlog(ctx).ErrorContext(ctx, "httpx: authorization decision unavailable",
			"permission", g.Permission, "tenant", t.Slug, "error", err)
		return problem.New(http.StatusServiceUnavailable, "authorization is temporarily unavailable")
	}
	if !allowed {
		return problem.New(http.StatusForbidden, "AUTH_DENIED: this requires "+g.Permission)
	}
	return nil
}

// recognised is a caller this installation knows, in a tenant it resolved: the
// question asked before the Authorizer, and the whole of what SignedIn wants.
func recognised(ctx context.Context) bool {
	if _, hasTenant := tenancy.FromContext(ctx); !hasTenant {
		return false
	}
	p, hasPrincipal := tenancy.PrincipalFrom(ctx)
	return hasPrincipal && p.UserID != uuid.Nil
}

// AddCommand records a command on the resource of module and entity. kit/rest
// calls it from Command, beside the route, so a catalog and its API cannot
// disagree about which doors exist. It does not look the resource up: a module
// registers its commands in a call of its own, which may run before or after
// the Spec — a test mounts them without it — so Resources joins the two.
func (a *API) AddCommand(module, entity string, c Command) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.commands == nil {
		a.commands = map[string][]Command{}
	}
	a.commands[module+"/"+entity] = append(a.commands[module+"/"+entity], c)
}

// Resources is every registered resource, in mount order, each carrying the
// commands recorded for it. modules/admin reads it in Routes, which is why the
// shell is composed last: a module that mounts after it registers a resource no
// screen was generated for.
func (a *API) Resources() []Resource {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := append([]Resource(nil), a.resources...)
	for i := range out {
		out[i].Commands = a.commands[out[i].Module+"/"+out[i].Entity]
	}
	return out
}

// writeElsewhere answers the address that performs this request's write, and the
// entity it is about, for the one case where the answer is not the address the
// caller wrote to: a resource whose reads answer here and whose writes answer on
// the surface its write permission belongs on — a price list, read by the tenant
// that pays for it and written by the installation — asked with a verb that
// changes something.
//
// It answers "" in every other case, and one case is refused on purpose: the
// installation's own address is not named to a caller standing at a host the
// control plane is not served at. An address nobody can reach is not a direction
// worth giving, and a refusal that named one would say more about the control
// plane than this host says about it anywhere else.
func (a *API) writeElsewhere(r *http.Request) (at, entity string) {
	if !unsafeMethod(r.Method) {
		return "", ""
	}
	for _, res := range a.Resources() {
		if res.WritePath == "" {
			continue
		}
		tail, ok := res.itemTail(r.URL.Path)
		if !ok {
			continue
		}
		at = res.WritePath + tail
		if classify(at) == SurfaceOps && !a.servesOps(r.Host) {
			continue
		}
		return at, res.Entity
	}
	return "", ""
}

// itemTail reports whether path is this resource's collection address or one of
// its rows, and names the row segment back so the other door can be given at the
// same place in its own address.
func (r Resource) itemTail(path string) (string, bool) {
	if path == r.Schema.Path {
		return "", true
	}
	if rest, moved := strings.CutPrefix(path, r.Schema.Path+"/"); moved && !strings.Contains(rest, "/") {
		return "/" + rest, true
	}
	return "", false
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
// The next parameter retains this request's path and query, so signing in does
// not discard the page's filters or selection. Both destinations must satisfy
// the kernel's local-path rule before next is escaped into the redirect. The
// sign-in page must validate next again because it is also reachable directly.
func signInFor(ctx huma.Context) (string, bool) {
	op := ctx.Operation()
	if ctx.Method() != http.MethodGet || op == nil || op.Extensions == nil {
		return "", false
	}
	to, ok := op.Extensions[SignInExtension].(string)
	if !ok || !LocalPath(to) || !strings.Contains(ctx.Header("Accept"), "text/html") {
		return "", false
	}
	requestURL := ctx.URL()
	target, err := url.Parse(to)
	if err != nil || requestURL.Path == target.Path {
		return "", false
	}
	// Validate the decoded path's structure without treating a literal percent
	// as a URL escape. EscapedPath then preserves encoded path boundaries.
	if !LocalPath(strings.ReplaceAll(requestURL.Path, "%", "%25")) {
		return "", false
	}
	here := requestURL.EscapedPath()
	if requestURL.RawQuery != "" {
		here += "?" + requestURL.RawQuery
	}
	if !LocalPath(here) {
		return "", false
	}
	query := target.Query()
	query.Set("next", here)
	target.RawQuery = query.Encode()
	return target.String(), true
}
