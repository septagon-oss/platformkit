package rest

// singleton.go is the other shape an entity has: one row per tenant.
//
// Spec is five routes on a collection, and two modules of E5 wanted something
// else — a tenant's site settings, and a tenant's subscription. Both wrote the
// same three routes by hand, with the same transaction ritual, the same error
// mapping and the same public-door check, and neither registered a resource, so
// neither got a screen. The review named it: a singleton belongs in the kernel.

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Singleton is one row per tenant projected onto HTTP: a read, an optional
// write, and an optional public face.
//
// It is not Spec with the list taken out. A singleton has no id in its path, so
// there is no create — a tenant has one whether or not anybody has saved it —
// and no delete: what a tenant would delete is a row that comes back the moment
// anything reads it. What is left is GET and PUT, and the two are enough for a
// settings screen.
//
// Load and Save are the module's, because the one thing a singleton cannot be
// generic about is what "there is none yet" means. A site that has been
// configured with nothing still has settings, and answers with the defaults; a
// tenant that has never subscribed has no subscription, and answers 404. Both
// are right, and no flag would say which.
type Singleton[T crud.Entity] struct {
	// Module and Entity name it, as they do on a Spec: the operation ids, the
	// tag and the generated screen's path come from them.
	Module, Entity string
	// Path is the resource: "/api/v1/site/settings". There is no item path.
	Path string
	// Read guards the read; Write guards the PUT. An empty Write mounts no PUT
	// at all, which is the shape of a singleton whose changes are commands —
	// billing's subscription is moved by subscribe and cancel, and a PUT would
	// be a caller writing its own period.
	Read, Write string
	// Public mounts GET {Path}/public, unauthenticated. Face is what it
	// answers with, and it is required when Public is set: what a visitor may
	// see is a smaller thing than what an administrator configured, and a
	// public route that served the whole row would be an admin screen anybody
	// could read.
	Public bool
	Face   func(T) any
	// Event is what the PUT declares it publishes. Save is what publishes it;
	// this is the declaration kit/app's boot gate reads.
	Event string

	// Load is the tenant's row. Save writes it and returns what was stored.
	Load func(ctx context.Context, tx db.Tx[db.Tenant]) (T, error)
	Save func(ctx context.Context, tx db.Tx[db.Tenant], in T) (T, error)
}

// Mount registers the two routes, the public one when there is one, and the
// resource the admin generator reads.
func (s Singleton[T]) Mount(api *httpx.API) {
	s.check()
	if s.Write != "" {
		// Only a writable singleton is registered, and the reason is the
		// generator rather than a preference: modules/admin mounts five pages
		// per resource and two of them are forms, so a resource with no write
		// permission is a screen whose buttons declare an authorization nobody
		// can hold. A read-only singleton — billing's subscription, which is
		// moved by its commands — is served by its API route and by whatever
		// page a module writes for it.
		api.RegisterResource(s.resource())
	}

	httpx.Register(api, s.op("read", http.MethodGet, s.Path, "Read this tenant's "+s.Entity, "", nil),
		httpx.Permission(s.Read), func(ctx context.Context, _ *struct{}) (*Item[T], error) {
			out, err := s.load(ctx)
			if err != nil {
				return nil, err
			}
			return &Item[T]{Body: out}, nil
		})

	if s.Write != "" {
		var events []string
		if s.Event != "" {
			events = []string{s.Event}
		}
		httpx.Register(api, s.op("save", http.MethodPut, s.Path, "Save this tenant's "+s.Entity,
			"The whole of it: a PUT replaces what is there. Saving what is already stored publishes nothing.", events),
			httpx.Permission(s.Write), func(ctx context.Context, in *bodyInput[T]) (*Item[T], error) {
				tx, err := transaction(ctx)
				if err != nil {
					return nil, err
				}
				crud.Reset(in.Body) // the four fields the server owns, whatever a body said
				out, err := s.Save(ctx, tx, in.Body)
				if err != nil {
					return nil, Fault(err)
				}
				return &Item[T]{Body: out}, nil
			})
	}

	if !s.Public {
		return
	}
	// The public door. It is safe to be public because the tenant still comes
	// from the request's own host and the query still runs under that tenant's
	// policy: an anonymous caller reads one tenant's row and there is no
	// parameter that could widen it.
	httpx.Register(api, huma.Operation{
		OperationID: s.Module + "-" + s.Entity + "-public",
		Method:      http.MethodGet,
		Path:        s.Path + "/public",
		Summary:     "Read this " + s.Entity + " as a visitor sees it",
		Description: "What a public site may show. Everything else a tenant configures is nobody else's business.",
		Tags:        []string{s.Module},
		Errors:      []int{http.StatusNotFound, http.StatusServiceUnavailable},
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*Item[any], error) {
		if _, ok := tenancy.FromContext(ctx); !ok {
			// A public route is reached at hosts that resolve to no tenant — a
			// probe addressing the pod — and there is nothing there to read.
			return nil, problem.NotFound("no site is served at this host")
		}
		out, err := s.load(ctx)
		if err != nil {
			return nil, err
		}
		return &Item[any]{Body: s.Face(out)}, nil
	})
}

// load is the read both doors make.
func (s Singleton[T]) load(ctx context.Context) (T, error) {
	var zero T
	tx, err := transaction(ctx)
	if err != nil {
		return zero, err
	}
	out, err := s.Load(ctx, tx)
	if err != nil {
		return zero, Fault(err)
	}
	return out, nil
}

// op is one operation, with the events its handler declares.
func (s Singleton[T]) op(verb, method, path, summary, description string, events []string) huma.Operation {
	op := huma.Operation{
		OperationID: s.Module + "-" + s.Entity + "-" + verb,
		Method:      method,
		Path:        path,
		Summary:     summary,
		Description: description,
		Tags:        []string{s.Module},
		Errors:      faults,
	}
	if len(events) > 0 {
		op.Extensions = map[string]any{httpx.EventsExtension: events}
	}
	return op
}

// resource is this singleton as the admin generator sees it: a collection of
// one. List answers with the row there is, Get and Update take its id, and the
// two that a singleton has no meaning for refuse rather than being nil — the
// generator calls all five, and a nil closure is a panic where a sentence
// belongs.
func (s Singleton[T]) resource() httpx.Resource {
	one := func(ctx context.Context, run func(db.Tx[db.Tenant]) (T, error)) (map[string]any, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		e, err := run(tx)
		if err != nil {
			return nil, Fault(err)
		}
		return row(e)
	}
	refuse := func() error {
		return problem.Conflict("a tenant has one " + s.Entity + ", and it is neither created nor removed")
	}
	return httpx.Resource{
		Module: s.Module, Entity: s.Entity, Path: s.Path,
		Read: s.Read, Write: s.Write, Schema: crud.Schema{
			Module: s.Module, Entity: s.Entity, Path: s.Path, Fields: crud.Fields[T](),
		},
		List: func(ctx context.Context, _ crud.Query) ([]map[string]any, int64, error) {
			out, err := one(ctx, func(tx db.Tx[db.Tenant]) (T, error) { return s.Load(ctx, tx) })
			if err != nil {
				return nil, 0, err
			}
			return []map[string]any{out}, 1, nil
		},
		Get: func(ctx context.Context, _ uuid.UUID) (map[string]any, error) {
			// The id is ignored on purpose: there is one row, and a screen that
			// asked for another would be asking about a tenant it cannot see.
			return one(ctx, func(tx db.Tx[db.Tenant]) (T, error) { return s.Load(ctx, tx) })
		},
		Create: func(context.Context, map[string]any) (map[string]any, error) { return nil, refuse() },
		Delete: func(context.Context, uuid.UUID) error { return refuse() },
		Update: func(ctx context.Context, _ uuid.UUID, values map[string]any) (map[string]any, error) {
			return one(ctx, func(tx db.Tx[db.Tenant]) (T, error) {
				current, err := s.Load(ctx, tx)
				if err != nil {
					return current, err
				}
				// The PATCH route's own merge, so read-only fields are refused
				// at this door exactly as they are over HTTP.
				if _, err := merge(current, crud.Fields[T](), nil, values); err != nil {
					return current, err
				}
				return s.Save(ctx, tx, current)
			})
		},
	}
}

// check refuses a Singleton that could only produce broken routes. It panics at
// the mount site, as Spec.check does: this is a wiring mistake.
func (s Singleton[T]) check() {
	var bad string
	switch {
	case !strings.HasPrefix(s.Path, "/"):
		bad = fmt.Sprintf("Path %q does not start with /", s.Path)
	case s.Load == nil:
		bad = "Load is required; what \"there is none yet\" means is the module's answer"
	case !httpx.ValidPermission(s.Read):
		bad = fmt.Sprintf("Read %q is not %q", s.Read, "<resource>:<action>")
	case s.Write != "" && !httpx.ValidPermission(s.Write):
		bad = fmt.Sprintf("Write %q is not %q", s.Write, "<resource>:<action>")
	case s.Write != "" && s.Save == nil:
		bad = "Write is declared and Save is nil, so the PUT would have nothing to write with"
	case s.Public && s.Face == nil:
		bad = "Public is set and Face is nil; a public route that served the whole row would be an admin screen anybody could read"
	}
	if bad != "" {
		panic("rest: Singleton for " + s.Path + ": " + bad)
	}
}
