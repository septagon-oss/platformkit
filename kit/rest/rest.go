// Package rest is an entity's projection onto HTTP: five routes, the commands
// beside them, and the one mapping from kit/crud's errors to statuses.
//
// A module declares one Spec per entity and gets list, create, read, update and
// delete as declared routes, each with its permission, each emitting the events
// the manifest promises. What a module writes is the struct, in its contracts/
// package, and the Spec, in its manifest; what it does not write is a
// repository, a service, a handler, a DTO or a mapper.
//
// It is a package of its own, and not the other half of kit/crud, because a
// contracts/ package imports the entity half and every consumer of a module
// compiles against its contracts/. Keeping the routes here is what keeps huma,
// chi and NATS out of the build graph of a module that only wanted to name a
// Task. See ARCHITECTURE.md, idea 3.
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
)

// Spec is one entity's presence in the application: five routes, two
// permissions, three events and a schema. A module writes one of these and
// mounts it; everything below is the same for every entity, which is why it is
// written once.
type Spec[T crud.Entity] struct {
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

	// Immutable names, by json field name, the fields a PATCH refuses because
	// a command of their own owns them: a task's assigneeId belongs to Assign,
	// which also moves the status and publishes task.assigned, so a caller who
	// could set it through the generic update would move the field alone and
	// tell nobody. The refusal is a 422 naming the field, so the caller is
	// told which door to use rather than left wondering why nothing happened.
	//
	// This is not ReadOnly. ReadOnly is the four fields Base contributes — the
	// server owns those at every door, and the create route discards whatever
	// a caller sent for them. An immutable field is writable, by exactly one
	// route. Every name here is checked against the entity's schema at mount,
	// so a misspelled one panics where it is written instead of silently
	// guarding nothing.
	Immutable []string

	// HookEvents names the events the hooks below publish. They are appended
	// to the create, update and delete operations' x-platformkit-events, so
	// the OpenAPI document says what a write can emit and kit/app's boot gate
	// sees it.
	//
	// It closes one direction and it is worth being plain about which. The
	// gate compares what the routes declare against what the manifests
	// declare; nothing reads what a handler actually publishes. So an event
	// named here and missing from a manifest fails startup, and an event a
	// hook publishes that nobody named here is still invisible to everything
	// but the outbox.
	HookEvents []string

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
func (s Spec[T]) Schema() crud.Schema {
	return crud.Schema{Module: s.Module, Entity: s.Entity, Path: s.Path, Fields: crud.Fields[T]()}
}

// Mount registers the five routes. Each one declares its permission, obtains
// the request's transaction from kit/httpx, and answers with kit/problem: 404
// for a row this tenant does not have, 422 for an entity that fails its own
// Validate or a query naming a field that does not exist, 409 for a unique
// constraint.
func (s Spec[T]) Mount(api *httpx.API) {
	s.check()
	schema := s.Schema()
	api.RegisterResource(s.resource()) // the same entity, for the generated screens

	httpx.Register(api, s.op("list", http.MethodGet, s.Path, 0,
		"List "+s.Entity+"s", "Sortable and filterable by: "+strings.Join(names(schema.Fields), ", ")),
		httpx.Permission(s.Read), func(ctx context.Context, in *listInput) (*Page[T], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			q, err := in.query(schema.Fields)
			if err != nil {
				return nil, Fault(err)
			}
			items, total, err := crud.List[T](tx, q)
			if err != nil {
				return nil, Fault(err)
			}
			out := &Page[T]{}
			out.Body.Items, out.Body.Total, out.Body.Limit, out.Body.Offset = items, total, q.Limit, q.Offset
			return out, nil
		})

	httpx.Register(api, s.op("create", http.MethodPost, s.Path, http.StatusCreated,
		"Create a "+s.Entity, "The tenant, the id and the timestamps are set by the server."),
		httpx.Permission(s.Write), func(ctx context.Context, in *bodyInput[T]) (*Item[T], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			e := in.Body
			crud.Reset(e) // whatever the caller sent for the read-only fields
			if err := crud.Create(ctx, tx, e); err != nil {
				return nil, Fault(err)
			}
			if err := s.emit(ctx, tx, Created, e, s.AfterCreate); err != nil {
				return nil, Fault(err)
			}
			return &Item[T]{Body: e}, nil
		})

	httpx.Register(api, s.op("read", http.MethodGet, s.item(), 0,
		"Read a "+s.Entity, ""),
		httpx.Permission(s.Read), func(ctx context.Context, in *idInput) (*Item[T], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			e, err := crud.Get[T](tx, in.ID)
			if err != nil {
				return nil, Fault(err)
			}
			return &Item[T]{Body: e}, nil
		})

	httpx.Register(api, s.op("update", http.MethodPatch, s.item(), 0,
		"Update a "+s.Entity, "Only the fields present in the body change; read-only fields are refused."),
		httpx.Permission(s.Write), func(ctx context.Context, in *patchInput) (*Item[T], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			e, err := crud.Get[T](tx, in.ID)
			if err != nil {
				return nil, Fault(err)
			}
			columns, err := merge(e, schema.Fields, s.Immutable, in.Body)
			if err != nil {
				return nil, Fault(err)
			}
			// Only the columns the body named, plus the stamp. Writing the
			// whole row would put every field back to what this request read,
			// which loses a concurrent patch of a field it never mentioned.
			if err := crud.Update(ctx, tx, e, append(columns, "updated_at")...); err != nil {
				return nil, Fault(err)
			}
			if err := s.emit(ctx, tx, Updated, e, s.AfterUpdate); err != nil {
				return nil, Fault(err)
			}
			return &Item[T]{Body: e}, nil
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
			e, err := crud.Get[T](tx, in.ID)
			if err != nil {
				return nil, Fault(err)
			}
			if err := crud.Delete[T](tx, in.ID, s.SoftDelete); err != nil {
				return nil, Fault(err)
			}
			if err := s.emit(ctx, tx, Deleted, e, s.AfterDelete); err != nil {
				return nil, Fault(err)
			}
			return nil, nil
		})
}

// Command registers one lifecycle route on a Spec's resource: POST
// {Path}/{id}/{verb}, guarded by the Spec's Write permission, taking the
// request's transaction, declaring the events it publishes, and answering
// failures with the same mapping the five routes above use.
//
// It is here rather than in each module because a command is the one thing a
// Spec cannot express — each is a rule about the state the entity is in, and
// each publishes an event of its own — while everything around it is what
// every module would otherwise write again and disagree with: a module with
// its own error mapping is a second opinion about what a 409 means, and one
// that forgot the events extension is a route the boot gate cannot see.
//
// I is the request body and it is optional, so a command that takes no
// arguments is a POST with no body at all; run is handed the zero I in that
// case. A command whose argument is missing is refused by run, with
// ErrInvalid, rather than by the decoder — which is what keeps "no assignee"
// and "an assignee that is not a user" the same 422.
func Command[I any, T crud.Entity](api *httpx.API, spec Spec[T], verb, summary, description string, events []string,
	run func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in I) (T, error),
) {
	op := huma.Operation{
		OperationID: spec.Module + "-" + spec.Entity + "-" + verb,
		Method:      http.MethodPost,
		Path:        spec.item() + "/" + verb,
		Summary:     summary,
		Description: description,
		Tags:        []string{spec.Module},
		Errors:      faults,
	}
	if len(events) > 0 {
		op.Extensions = map[string]any{httpx.EventsExtension: events}
	}
	httpx.Register(api, op, httpx.Permission(spec.Write),
		func(ctx context.Context, in *commandInput[I]) (*Item[T], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			var body I
			if in.Body != nil {
				body = *in.Body
			}
			e, err := run(ctx, tx, in.ID, body)
			if err != nil {
				return nil, Fault(err)
			}
			return &Item[T]{Body: e}, nil
		})
}

// commandInput is a command's path id and its body. The body is a pointer
// because huma reads a struct body as required, and "{}" is not something a
// caller should have to send to say nothing.
type commandInput[I any] struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"The row's id"`
	Body *I
}

// emit publishes the event for a write and then runs the module's hook, both
// inside the request's transaction: an event that describes a change that
// rolled back is never seen, because it rolled back too.
func (s Spec[T]) emit(ctx context.Context, tx db.Tx[db.Tenant], verb string, e T, hook func(context.Context, db.Tx[db.Tenant], T) error) error {
	if !s.NoEvents {
		if err := events.Publish(ctx, tx, s.Event(verb), e); err != nil {
			return err
		}
	}
	if hook == nil {
		return nil
	}
	return hook(ctx, tx, e)
}

// faults are the statuses every operation here can answer with: the three a
// caller can act on, and the one that says the database is not reachable. They
// are declared so the OpenAPI document lists them; Fault and transaction are
// what produce them.
var faults = []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusServiceUnavailable}

// writes maps the three operations that change something to the event each
// publishes.
var writes = map[string]string{"create": Created, "update": Updated, "delete": Deleted}

// op builds the operation, including the declaration of the events the handler
// will publish — its own, and whatever its hook promises in HookEvents.
// kit/app reads that declaration back off the recorded operations and refuses
// to start when a module did not promise them.
func (s Spec[T]) op(verb, method, path string, status int, summary, description string) huma.Operation {
	op := huma.Operation{
		OperationID:   s.Module + "-" + s.Entity + "-" + verb,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Description:   description,
		Tags:          []string{s.Module},
		DefaultStatus: status,
		Errors:        faults,
	}
	published, ok := writes[verb]
	if !ok {
		return op
	}
	var names []string
	if !s.NoEvents {
		names = append(names, s.Event(published))
	}
	names = append(names, s.HookEvents...)
	if len(names) > 0 {
		op.Extensions = map[string]any{httpx.EventsExtension: names}
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
	// An Immutable name the entity has no field for guards nothing, silently
	// and forever, so it is a mount-time panic like everything else here.
	for _, name := range s.Immutable {
		if bad != "" {
			break
		}
		if _, ok := crud.FieldNamed(crud.Fields[T](), name); !ok {
			bad = fmt.Sprintf("Immutable names %q, which is not a field of the entity", name)
		}
	}
	if bad != "" {
		panic("rest: Spec for " + s.Path + ": " + bad)
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
	case errors.Is(err, crud.ErrNotFound):
		return problem.NotFound("no such row, or none this tenant may see")
	case errors.Is(err, crud.ErrInvalid):
		return problem.New(http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, crud.ErrConflict):
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
//
// required, because T is a pointer type and huma reads a pointer body as
// optional: a POST with no body at all used to arrive here as a nil entity and
// panic on the first field the handler stamped. The tag makes it a 400 that
// says what is missing; crud.Create's own nil check is the second half, because
// this package is also called from a module's own handlers.
type bodyInput[T any] struct {
	Body T `required:"true"`
}

// patchInput is the update route's body: the fields to change, and no others.
type patchInput struct {
	ID   uuid.UUID      `path:"id" format:"uuid" doc:"The row's id"`
	Body map[string]any `doc:"The writable fields to change"`
}

// Item is one entity as a response body, and Page is a page of them.
//
// They are exported because a module that writes its own handlers answers in
// the same shape as the five routes here — modules/audit and
// modules/notification both do, an append-only trail and a per-recipient list
// being things a Spec is not — and three separately declared response shapes
// are three spellings of the same JSON for a client to discover the hard way.
type Item[T any] struct {
	Body T
}

// Page is a page of rows and the total it came from. The limit and the offset
// are echoed, so a caller that sent neither knows what it got.
type Page[T any] struct {
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

func (in *listInput) query(fields []crud.Field) (crud.Query, error) {
	q := crud.Query{Limit: in.Limit, Offset: in.Offset, Sort: in.Sort}
	if len(in.Filter) == 0 {
		return q, nil
	}
	q.Filter = make(map[string]any, len(in.Filter))
	for _, raw := range in.Filter {
		name, value, ok := strings.Cut(raw, ":")
		if !ok {
			return crud.Query{}, fmt.Errorf("%w: filter %q is not \"field:value\"", crud.ErrInvalid, raw)
		}
		f, known := crud.FieldNamed(fields, name)
		if !known {
			return crud.Query{}, fmt.Errorf("%w: there is no field %q to filter on", crud.ErrInvalid, name)
		}
		typed, err := coerce(f, value)
		if err != nil {
			return crud.Query{}, err
		}
		q.Filter[name] = typed
	}
	return q, nil
}

// coerce reads a query parameter as the field's own type, so a filter on an
// integer column compares integers. Postgres would raise on the mismatch, which
// is a 500 for what is really a malformed request.
func coerce(f crud.Field, raw string) (any, error) {
	var (
		v   any
		err error
	)
	switch f.Type {
	case crud.TypeInt:
		v, err = strconv.ParseInt(raw, 10, 64)
	case crud.TypeFloat:
		v, err = strconv.ParseFloat(raw, 64)
	case crud.TypeBool:
		v, err = strconv.ParseBool(raw)
	case crud.TypeUUID:
		v, err = uuid.Parse(raw)
	case crud.TypeTime:
		v, err = time.Parse(time.RFC3339, raw)
	default:
		v = raw
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s is a %s and %q is not one", crud.ErrInvalid, f.Name, f.Type, raw)
	}
	return v, nil
}

// merge applies a PATCH body to an entity, field by field, through the schema,
// and reports the database columns it changed so that Update writes those and
// no others. A name the schema does not know, one it knows as read-only, or one
// the Spec reserved to a command of its own is refused rather than ignored,
// because a caller who spells a field wrong — or reaches for the wrong door —
// has to be told.
func merge(e any, fields []crud.Field, immutable []string, patch map[string]any) ([]string, error) {
	target := reflect.ValueOf(e).Elem()
	columns := make([]string, 0, len(patch))
	for name, value := range patch {
		f, ok := crud.FieldNamed(fields, name)
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: there is no field %q", crud.ErrInvalid, name)
		case f.ReadOnly:
			return nil, fmt.Errorf("%w: %s is read-only", crud.ErrInvalid, name)
		case slices.Contains(immutable, name):
			return nil, fmt.Errorf("%w: %s is changed by a command of its own, not by a patch", crud.ErrInvalid, name)
		}
		// Round-tripping through JSON is what makes this the same decoder the
		// request body went through: one set of rules for "3" as an int and for
		// null as an empty pointer.
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %s", crud.ErrInvalid, name, err)
		}
		if err := json.Unmarshal(encoded, target.FieldByIndex(f.Index).Addr().Interface()); err != nil {
			return nil, fmt.Errorf("%w: %s: %s", crud.ErrInvalid, name, err)
		}
		columns = append(columns, f.Column)
	}
	return columns, nil
}

// names are the fields the list route can be sorted and filtered by, which is
// every field but a list: a column holding many values is not one an equality
// compares against, and a document that offered it would be describing a 422.
func names(fields []crud.Field) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f.Type != crud.TypeList {
			out = append(out, f.Name)
		}
	}
	return out
}
