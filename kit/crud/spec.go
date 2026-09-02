package crud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
)

// Spec is one entity's presence in the application: five routes, two
// permissions, three events and a schema. A module writes one of these and
// mounts it; everything below is the same for every entity, which is why it is
// written once.
type Spec[T Entity] struct {
	// Module is the manifest's name. It prefixes the events, so the events a
	// Spec publishes are namespaced by the module that mounts it.
	Module string
	// Entity is this resource's name, lower-case: "task". It is the middle of
	// the event name and the noun in the operation ids.
	Entity string
	// Path is the collection's path: "/api/tasks". The item is Path + "/{id}".
	Path string
	// Read guards the list and the read; Write guards create, update and
	// delete. Both are permissions some module has to define, or the app
	// refuses to start.
	Read, Write string
	// SoftDelete keeps deleted rows, hidden, instead of removing them.
	SoftDelete bool
	// NoEvents mounts the routes without publishing anything. It is spelled in
	// the negative so that the zero Spec is the one that emits events: a
	// silence nobody asked for is how an integration goes missing.
	NoEvents bool

	// The hooks run inside the request's transaction, after the write and
	// after the event, so a hook can publish more events or write more rows and
	// all of it commits together.
	AfterCreate func(ctx context.Context, tx db.Tx[db.Tenant], e T) error
	AfterUpdate func(ctx context.Context, tx db.Tx[db.Tenant], e T) error
	AfterDelete func(ctx context.Context, tx db.Tx[db.Tenant], e T) error
}

// The three events every Spec publishes, unless NoEvents.
const (
	Created = "created"
	Updated = "updated"
	Deleted = "deleted"
)

// Event is the full name of one of this Spec's events, "<module>.<entity>.<verb>".
// A module lists these in its manifest's Events, and kit/app refuses to start
// when a Spec would publish one that nothing declared.
func (s Spec[T]) Event(verb string) string { return s.Module + "." + s.Entity + "." + verb }

// Events are the three names this Spec publishes, so a manifest can name them
// without spelling them, and none.
func (s Spec[T]) Events() []string {
	if s.NoEvents {
		return nil
	}
	return []string{s.Event(Created), s.Event(Updated), s.Event(Deleted)}
}

// Schema describes the entity to anything that did not compile against it: the
// generated screens of stage E4, and the list operation's own documentation.
func (s Spec[T]) Schema() Schema {
	return Schema{Module: s.Module, Entity: s.Entity, Path: s.Path, Fields: fieldsOf[T]()}
}

// Mount registers the five routes. Each one declares its permission, obtains
// the request's transaction from kit/httpx, and answers with kit/problem: 404
// for a row this tenant does not have, 422 for an entity that fails its own
// Validate or a query naming a field that does not exist, 409 for a unique
// constraint.
func (s Spec[T]) Mount(api *httpx.API) {
	s.check()
	schema := s.Schema()

	httpx.Register(api, s.op("list", http.MethodGet, s.Path, 0,
		"List "+s.Entity+"s", "Sortable and filterable by: "+strings.Join(names(schema.Fields), ", ")),
		httpx.Permission(s.Read), func(ctx context.Context, in *listInput) (*listOutput[T], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			q, err := in.query(schema.Fields)
			if err != nil {
				return nil, Fault(err)
			}
			items, total, err := List[T](tx, q)
			if err != nil {
				return nil, Fault(err)
			}
			out := &listOutput[T]{}
			out.Body.Items, out.Body.Total, out.Body.Limit, out.Body.Offset = items, total, q.Limit, q.Offset
			return out, nil
		})

	httpx.Register(api, s.op("create", http.MethodPost, s.Path, http.StatusCreated,
		"Create a "+s.Entity, "The tenant, the id and the timestamps are set by the server."),
		httpx.Permission(s.Write), func(ctx context.Context, in *bodyInput[T]) (*itemOutput[T], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			e := in.Body
			*e.base() = Base{} // whatever the caller sent for the read-only fields
			if err := Create(ctx, tx, e); err != nil {
				return nil, Fault(err)
			}
			if err := s.emit(ctx, tx, Created, e, s.AfterCreate); err != nil {
				return nil, Fault(err)
			}
			return &itemOutput[T]{Body: e}, nil
		})

	httpx.Register(api, s.op("read", http.MethodGet, s.item(), 0,
		"Read a "+s.Entity, ""),
		httpx.Permission(s.Read), func(ctx context.Context, in *idInput) (*itemOutput[T], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			e, err := Get[T](tx, in.ID)
			if err != nil {
				return nil, Fault(err)
			}
			return &itemOutput[T]{Body: e}, nil
		})

	httpx.Register(api, s.op("update", http.MethodPatch, s.item(), 0,
		"Update a "+s.Entity, "Only the fields present in the body change; read-only fields are refused."),
		httpx.Permission(s.Write), func(ctx context.Context, in *patchInput) (*itemOutput[T], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			e, err := Get[T](tx, in.ID)
			if err != nil {
				return nil, Fault(err)
			}
			if err := merge(e, schema.Fields, in.Body); err != nil {
				return nil, Fault(err)
			}
			if err := Update(ctx, tx, e); err != nil {
				return nil, Fault(err)
			}
			if err := s.emit(ctx, tx, Updated, e, s.AfterUpdate); err != nil {
				return nil, Fault(err)
			}
			return &itemOutput[T]{Body: e}, nil
		})

	httpx.Register(api, s.op("delete", http.MethodDelete, s.item(), http.StatusNoContent,
		"Delete a "+s.Entity, ""),
		httpx.Permission(s.Write), func(ctx context.Context, in *idInput) (*struct{}, error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			// The row is read first so that a delete of something this tenant
			// does not have is a 404, and so that the hook and the event carry
			// what was deleted rather than only its id.
			e, err := Get[T](tx, in.ID)
			if err != nil {
				return nil, Fault(err)
			}
			if err := Delete[T](tx, in.ID, s.SoftDelete); err != nil {
				return nil, Fault(err)
			}
			if err := s.emit(ctx, tx, Deleted, e, s.AfterDelete); err != nil {
				return nil, Fault(err)
			}
			return nil, nil
		})
}

// emit publishes the event for a write and then runs the module's hook, both
// inside the request's transaction: an event that describes a change that
// rolled back is never seen, because it rolled back too.
func (s Spec[T]) emit(ctx context.Context, tx db.Tx[db.Tenant], verb string, e T, hook func(context.Context, db.Tx[db.Tenant], T) error) error {
	if !s.NoEvents {
		if err := events.Publish(tx, s.Event(verb), e); err != nil {
			return err
		}
	}
	if hook == nil {
		return nil
	}
	return hook(ctx, tx, e)
}

// op builds the operation, including the declaration of the events the handler
// will publish. kit/app reads that declaration back off the recorded operations
// and refuses to start when a module did not promise them.
func (s Spec[T]) op(verb, method, path string, status int, summary, description string) huma.Operation {
	op := huma.Operation{
		OperationID:   s.Module + "-" + s.Entity + "-" + verb,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Description:   description,
		Tags:          []string{s.Module},
		DefaultStatus: status,
		Errors:        []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}
	if published, ok := map[string]string{"create": Created, "update": Updated, "delete": Deleted}[verb]; ok && !s.NoEvents {
		op.Extensions = map[string]any{httpx.EventsExtension: []string{s.Event(published)}}
	}
	return op
}

func (s Spec[T]) item() string { return strings.TrimSuffix(s.Path, "/") + "/{id}" }

// check refuses a Spec that could only produce broken routes or unroutable
// events. It panics, at the mount site, for the same reason httpx.Permission
// does: this is a wiring mistake, not a request-time condition.
func (s Spec[T]) check() {
	var bad string
	switch {
	case !strings.HasPrefix(s.Path, "/"):
		bad = fmt.Sprintf("Path %q does not start with /", s.Path)
	case !events.ValidName(s.Event(Created)):
		bad = fmt.Sprintf("Module %q and Entity %q do not make an event name", s.Module, s.Entity)
	case !httpx.ValidPermission(s.Read):
		bad = fmt.Sprintf("Read %q is not %q", s.Read, "<resource>:<action>")
	case !httpx.ValidPermission(s.Write):
		bad = fmt.Sprintf("Write %q is not %q", s.Write, "<resource>:<action>")
	}
	if bad != "" {
		panic("crud: Spec for " + s.Path + ": " + bad)
	}
}

// transaction is the request's, or a 503 saying why there is none. A handler
// that cannot reach the database has nothing to answer with, and the middleware
// has already logged the cause.
func transaction(ctx context.Context) (db.Tx[db.Tenant], error) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return tx, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
	}
	return tx, nil
}

// Fault turns the three errors a caller can act on into the response that says
// so: 404 for a row this tenant does not have, 422 for something the caller
// sent, 409 for a state the write contradicts. Anything else is an outage and
// reaches huma as a 500 with its cause in the log and nothing in the body.
//
// It is exported because a module's own handlers answer with the same three
// errors as the five routes here, and one mapping is the point: a module that
// wrote its own would be a second opinion about what a 404 means.
func Fault(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return problem.NotFound("no such row, or none this tenant may see")
	case errors.Is(err, ErrInvalid):
		return problem.New(http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrConflict):
		return problem.Conflict(err.Error())
	default:
		return err
	}
}

// idInput is the item routes' path parameter.
type idInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The row's id"`
}

// bodyInput is the create route's body: the entity itself, minus whatever it
// says about the fields the server owns.
type bodyInput[T any] struct {
	Body T
}

// patchInput is the update route's body: the fields to change, and no others.
type patchInput struct {
	ID   uuid.UUID      `path:"id" format:"uuid" doc:"The row's id"`
	Body map[string]any `doc:"The writable fields to change"`
}

type itemOutput[T any] struct {
	Body T
}

type listOutput[T any] struct {
	Body struct {
		Items  []T   `json:"items"`
		Total  int64 `json:"total"`
		Limit  int   `json:"limit"`
		Offset int   `json:"offset"`
	}
}

// listInput is the page, the order and the filters, as query parameters.
// Filters repeat — ?filter=status:open&filter=priority:2 — rather than sharing
// one comma-separated value, because a value may contain a comma and a field
// name may not contain a colon.
type listInput struct {
	Limit  int      `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Rows per page"`
	Offset int      `query:"offset" minimum:"0" doc:"Rows to skip"`
	Sort   string   `query:"sort" doc:"A field name, or a field name prefixed with - for descending"`
	Filter []string `query:"filter" doc:"field:value, repeated"`
}

func (in *listInput) query(fields []Field) (Query, error) {
	q := Query{Limit: in.Limit, Offset: in.Offset, Sort: in.Sort}
	if len(in.Filter) == 0 {
		return q, nil
	}
	q.Filter = make(map[string]any, len(in.Filter))
	for _, raw := range in.Filter {
		name, value, ok := strings.Cut(raw, ":")
		if !ok {
			return Query{}, fmt.Errorf("%w: filter %q is not \"field:value\"", ErrInvalid, raw)
		}
		f, known := fieldNamed(fields, name)
		if !known {
			return Query{}, fmt.Errorf("%w: there is no field %q to filter on", ErrInvalid, name)
		}
		typed, err := coerce(f, value)
		if err != nil {
			return Query{}, err
		}
		q.Filter[name] = typed
	}
	return q, nil
}

// coerce reads a query parameter as the field's own type, so a filter on an
// integer column compares integers. Postgres would raise on the mismatch, which
// is a 500 for what is really a malformed request.
func coerce(f Field, raw string) (any, error) {
	var (
		v   any
		err error
	)
	switch f.Type {
	case TypeInt:
		v, err = strconv.ParseInt(raw, 10, 64)
	case TypeFloat:
		v, err = strconv.ParseFloat(raw, 64)
	case TypeBool:
		v, err = strconv.ParseBool(raw)
	case TypeUUID:
		v, err = uuid.Parse(raw)
	case TypeTime:
		v, err = time.Parse(time.RFC3339, raw)
	default:
		v = raw
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s is a %s and %q is not one", ErrInvalid, f.Name, f.Type, raw)
	}
	return v, nil
}

// merge applies a PATCH body to an entity, field by field, through the schema:
// a name the schema does not know, or one it knows as read-only, is refused
// rather than ignored, because a caller who spells a field wrong has to be told.
func merge(e any, fields []Field, patch map[string]any) error {
	target := reflect.ValueOf(e).Elem()
	for name, value := range patch {
		f, ok := fieldNamed(fields, name)
		switch {
		case !ok:
			return fmt.Errorf("%w: there is no field %q", ErrInvalid, name)
		case f.ReadOnly:
			return fmt.Errorf("%w: %s is read-only", ErrInvalid, name)
		}
		// Round-tripping through JSON is what makes this the same decoder the
		// request body went through: one set of rules for "3" as an int and for
		// null as an empty pointer.
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("%w: %s: %s", ErrInvalid, name, err)
		}
		if err := json.Unmarshal(encoded, target.FieldByIndex(f.index).Addr().Interface()); err != nil {
			return fmt.Errorf("%w: %s: %s", ErrInvalid, name, err)
		}
	}
	return nil
}

func names(fields []Field) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, f.Name)
	}
	return out
}
