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
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Spec is one entity's presence in the application: five routes, two
// permissions, three events and a schema. A module writes one of these and
// mounts it; everything below is the same for every entity, which is why it is
// written once.
type Spec[T crud.Entity] struct {
	// RichTextFiles resolves richtext image references in the request transaction.
	RichTextFiles richtext.Files
	// Module is the manifest's name. It prefixes the events, so the events a
	// Spec publishes are namespaced by the module that mounts it.
	Module string
	// Entity is this resource's name, lower-case: "task". It is the middle of
	// the event name and the noun in the operation ids.
	Entity string
	// Path is the collection's path relative to the module: "/tasks". The item
	// is Path + "/{id}".
	//
	// It is relative and not absolute because the address a route answers at is
	// not the module's to know: the kernel composes it from this path, the
	// module's name and the surface the permissions put it on — /api/v1/task/tasks
	// for a workspace resource, /api/v1/ops/billing/plans for one the
	// installation owns. Writing the prefix here was a module naming a surface
	// it had not chosen.
	Path string
	// Read guards the list and the read; Write guards create, update and
	// delete. Both are permissions some module has to define, or the app
	// refuses to start.
	Read, Write string
	// OperatorRead restricts reads and discovery to the operator's own tenant.
	// The manifest must declare Read with Operator: true. Writes retain their
	// own declaration; private control-plane rows normally set both flags.
	OperatorRead bool
	// OperatorWrite restricts writes to the operator's own tenant, independently
	// of reads. The manifest must declare Write with Operator: true; otherwise
	// kit/app refuses startup. See docs/adr/0008.
	OperatorWrite bool
	// Operations is which of the five routes this resource offers: rest.List,
	// rest.Read, rest.Create, rest.Update, rest.Delete. Empty is all five, so
	// every Spec written today is unchanged.
	//
	// A verb left out mounts no route, and the router answers accordingly: 405
	// where the address is mounted for another verb, 404 where nothing is mounted
	// there at all. Neither is a refusal naming a permission nobody asked for. It
	// draws no door on a generated page, and mounts no page. It is the resource's
	// own declaration and not a caller's: what differs per caller is whether they
	// may use a route that exists, which is Read's, Write's and ReadAuth's
	// question, never this one's. A module whose writes are commands rather than
	// CRUD says so by naming the verbs it has: a resource that offers no create
	// should not advertise one.
	//
	// Two shapes are answered differently rather than by the router. A set with no
	// list has no workspace address, so it mounts no generated page at all and
	// publishes no screen — such a module writes its own pages, the way
	// modules/admin writes the ones a Spec cannot describe. A list with no read is
	// refused at Mount, because the list page links every row's identity to the
	// record route and a resource that offers no read has no record route to answer
	// it. Everything else mounts: the doors, the pages and the catalogue are all
	// written from this one field.
	Operations []httpx.CRUD
	// ReadAuth and WriteAuth are the declarations the routes answer to, for the
	// guard a permission string cannot name: httpx.SignedIn(), or a permission that
	// also needs a plan feature. Zero means what Read and Write mean now — the two
	// shorthand fields, read through the two operator flags — and check() refuses a
	// Spec that spells the same guard both ways. An operator grant keeps the
	// shorthand (Read and OperatorRead), which is the spelling kit/app cross-checks
	// against the module manifest; the declaration is for what the shorthand cannot
	// say, and never for httpx.Public(), which would be a tenant's rows behind no
	// guard at all.
	//
	// SignedIn admits any caller carrying a principal for the resolved tenant and
	// nothing more (kit/httpx/authorize.go): the rows a signed-in read reaches
	// are exactly the rows this tenant's transaction may see, which is the
	// entity's own row-level security policy deciding. A write whose *row* the
	// caller names is not admitted by it, and check() refuses the mount: the
	// generic PATCH and DELETE ask only whose tenant a row is in
	// (crud.RecheckTenant), never whose row it is, so the door that wants an
	// object decision is rest.Command — which carries an Auth of its own and runs
	// the module's service, where tenancy.RequirePolicy lives.
	ReadAuth httpx.Auth
	// WriteAuth is the write side of the same two declarations.
	WriteAuth httpx.Auth
	// SoftDelete keeps deleted rows, hidden, instead of removing them.
	SoftDelete bool
	// Immutable names, by json field name, the fields a PATCH refuses because
	// a route of their own owns them: a task's assigneeId belongs to Assign,
	// which also moves the status and publishes task.assigned, so a caller who
	// could set it through the generic update would move the field alone and
	// tell nobody, and a content author belongs to the create that stamped it.
	// The refusal is a 422 naming the field, so the caller is told which door
	// to use rather than left wondering why nothing happened.
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
	//
	// There is no AfterUpdate, and the absence is a decision rather than an
	// omission: nothing in three repositories ever set one, and a hook nobody
	// writes is a parameter every reader of this struct has to rule out. A
	// module that needs one adds it back in the commit that uses it.
	AfterCreate func(ctx context.Context, tx db.Tx[db.Tenant], e T) error
	AfterDelete func(ctx context.Context, tx db.Tx[db.Tenant], e T) error
}

// The five operations a Spec may offer, spelled the way the routes that mount
// them are spelled. The values are httpx.CRUD — the same strings the operation
// ids end with and the keys of the `writes` map near the bottom of this file —
// so one verb means the same thing in the route table, on a screen and in the
// catalogue a phone parses.
const (
	List   = httpx.CRUDList
	Read   = httpx.CRUDRead // a constant, not the field: Spec.Read is a permission string
	Create = httpx.CRUDCreate
	Update = httpx.CRUDUpdate
	Delete = httpx.CRUDDelete
)

// The three events every Spec publishes.
const (
	Created = "created"
	Updated = "updated"
	Deleted = "deleted"
)

// Event is the full name of one of this Spec's events, "<module>.<entity>.<verb>".
// A module lists these in its manifest's Declared, and kit/app refuses to start
// when a Spec would publish one that nothing declared.
func (s Spec[T]) Event(verb string) string { return s.Module + "." + s.Entity + "." + verb }

// Events are the three names this Spec publishes, so a manifest can name them
// without spelling them.
func (s Spec[T]) Events() []string {
	return []string{s.Event(Created), s.Event(Updated), s.Event(Deleted)}
}

// Declared is the same three names, each with T as its payload type: what
// emit marshals is the entity, so the manifest's promise about a Spec's event is
// the entity type and nothing else. A module that mounts a Spec puts this in
// its manifest rather than writing the three types out beside the three names.
func (s Spec[T]) Declared() []events.Declared {
	return []events.Declared{
		events.Declare[T](s.Event(Created)),
		events.Declare[T](s.Event(Updated)),
		events.Declare[T](s.Event(Deleted)),
	}
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
//
// The reads and the writes may not live at the same address, and the two
// operator flags say which: a route the installation owns belongs on the Ops
// surface, and a route a customer's own administrator uses belongs on the
// workspace. A price list is the worked case — readable by the tenant that pays
// for it, written only by the operator — and it is why there are two routers
// here rather than one. The generated screens are mounted on the workspace
// whatever the answer is, because a screen a person stands in front of is
// always workspace work: it declares the same permission the write route does.
func (s Spec[T]) Mount(surfaces httpx.Surfaces) {
	s.check()
	schema := s.Schema()
	read, write := s.readRouter(surfaces), s.writeRouter(surfaces)
	res := s.resource()
	// The screens' address is the collection page's address, and it exists when
	// the collection page is mounted. A resource that offers no list has no
	// workspace page, and ui/screens mounts no generated page behind an address
	// that answers 404 — such a module serves its own pages, which is what
	// modules/admin's own mount does for the ones a Spec cannot describe.
	// Publishing a screen the kernel never mounted is the lie the catalogue
	// exists to keep.
	if s.offers(httpx.CRUDList) {
		res.Screen = surfaces.App.PagePath(s.Path)
	}
	res.Schema.Path = read.Prefix() + s.Path
	if write.Prefix() != read.Prefix() && s.offersWrite() {
		// The one resource whose writes do not answer where its reads do: the
		// catalog and the kernel's own refusal name that door, because nothing a
		// caller can derive from the read address says it is elsewhere, and a
		// shell that derived it anyway would be sent to an address that answers
		// the read and refuses the write.
		//
		// Which writes are mounted is asked here and not assumed: the pointer is a
		// sentence about an address, and an operation set that names no write
		// builds no write door on either surface, so the honest answer at the read
		// door is the router's own — nothing is served for this verb — rather than
		// a direction to a door that was never built. See offersWrite.
		res.WritePath = write.Prefix() + s.Path
	}
	surfaces.RegisterResource(res) // the same entity, for the generated screens

	if s.offers(httpx.CRUDList) {
		httpx.Register(read, s.op("list", http.MethodGet, s.Path, 0,
			"List "+s.Entity+"s", "Sortable and filterable by: "+strings.Join(names(schema.Fields), ", ")),
			s.readAuth(), func(ctx context.Context, in *listInput) (*Page[T], error) {
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
	}

	if s.offers(httpx.CRUDCreate) {
		httpx.Register(write, s.op("create", http.MethodPost, s.Path, http.StatusCreated,
			"Create a "+s.Entity, "The tenant, the id and the timestamps are set by the server."),
			s.writeAuth(), func(ctx context.Context, in *bodyInput[T]) (*Item[T], error) {
				tx, err := transaction(ctx)
				if err != nil {
					return nil, err
				}
				// Immutable is refused at this door as well as at the patch. A
				// create used to be the way past it: content's author is stamped by
				// Validate from the actor, and a body naming it stored whatever the
				// caller said instead, silently, with the entity's own
				// documentation saying otherwise. See named.
				if err := refuseImmutable(in.RawBody, s.Immutable); err != nil {
					return nil, Fault(err)
				}
				e, err := s.createRow(ctx, tx, in.Body)
				if err != nil {
					return nil, Fault(err)
				}
				return &Item[T]{Body: e}, nil
			})
	}

	if s.offers(httpx.CRUDRead) {
		httpx.Register(read, s.op("read", http.MethodGet, s.item(), 0,
			"Read a "+s.Entity, ""),
			s.readAuth(), func(ctx context.Context, in *idInput) (*Item[T], error) {
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
	}

	if s.offers(httpx.CRUDUpdate) {
		httpx.Register(write, s.op("update", http.MethodPatch, s.item(), 0,
			"Update a "+s.Entity, "Only the fields present in the body change; read-only fields are refused."),
			s.writeAuth(), func(ctx context.Context, in *patchInput) (*Item[T], error) {
				tx, err := transaction(ctx)
				if err != nil {
					return nil, err
				}
				e, err := s.updateRow(ctx, tx, in.ID, schema.Fields, in.Body)
				if err != nil {
					return nil, Fault(err)
				}
				return &Item[T]{Body: e}, nil
			})
	}

	if s.offers(httpx.CRUDDelete) {
		httpx.Register(write, s.op("delete", http.MethodDelete, s.item(), http.StatusNoContent,
			"Delete a "+s.Entity, ""),
			s.writeAuth(), func(ctx context.Context, in *idInput) (*struct{}, error) {
				tx, err := transaction(ctx)
				if err != nil {
					return nil, err
				}
				if _, err := s.deleteRow(ctx, tx, in.ID); err != nil {
					return nil, Fault(err)
				}
				return nil, nil
			})
	}
	for _, field := range schema.Fields {
		if field.Widget == "richtext" {
			httpx.SetFieldMediaType[T](read, field.Name, "text/markdown")
		}
	}
}

// JSON routes and in-process resources share their write orchestration.
func (s Spec[T]) createRow(ctx context.Context, tx db.Tx[db.Tenant], e T) (T, error) {
	crud.Reset(e) // IDs, tenancy and timestamps belong to the server at both doors.
	if err := s.prepareRichText(ctx, tx, e, nil); err != nil {
		return e, err
	}
	if err := crud.Create(ctx, tx, e); err != nil {
		return e, err
	}
	return e, s.emit(ctx, tx, Created, e, s.AfterCreate)
}

// The row lock
// must precede the merge and validation; locking only at the write leaves
// responses, hooks and events based on a stale snapshot after contention.
func (s Spec[T]) updateRow(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, fields []crud.Field, values map[string]any) (T, error) {
	e, err := crud.GetForUpdate[T](tx, id)
	if err != nil {
		return e, err
	}
	columns, err := merge(e, fields, s.Immutable, values)
	if err != nil {
		return e, err
	}
	// A body that named no column changed nothing, so this is not a write: no
	// UPDATE, no validation, no moved timestamp, no event. merge applies one
	// column per key it accepts and refuses every key it does not, so the entity
	// is still the row as it was read under the lock — its caller's answer, once
	// the row is the caller's at all. The lock says a row is there, not whose it
	// is: on a table whose read policy shows one list to every tenant it is the
	// write's tenant recheck that refuses, and this body never reaches that write.
	// So ask the recheck here, and a foreign row answers 404 as it answers a body
	// that names a column; the silence below is about writing, never about whose.
	if len(columns) == 0 {
		if err := crud.RecheckTenant(tx, e); err != nil {
			return e, err
		}
		return e, nil
	}
	if err := s.prepareRichText(ctx, tx, e, columns); err != nil {
		return e, err
	}
	// Write only the submitted columns and timestamp. Untouched fields retain
	// the preceding committed values used by validation and the emitted event.
	if err := crud.Update(ctx, tx, e, append(columns, "updated_at")...); err != nil {
		return e, err
	}
	return e, s.emit(ctx, tx, Updated, e, nil)
}

func (s Spec[T]) deleteRow(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (T, error) {
	e, err := crud.GetForUpdate[T](tx, id)
	if err != nil {
		return e, err
	}
	// Whose row it is, asked here for the same reason the update asks it, and with
	// more to lose: row-level security filters a DELETE by its USING clause alone
	// — there is no new row for a WITH CHECK to inspect — so on a table every
	// tenant may read, the statement below reaches a row the request's tenant may
	// not write, and the event would be published over a row it removed.
	if err := crud.RecheckTenant(tx, e); err != nil {
		return e, err
	}
	if err := crud.Delete[T](tx, id, s.SoftDelete); err != nil {
		return e, err
	}
	return e, s.emit(ctx, tx, Deleted, e, s.AfterDelete)
}

// CreateRow, UpdateRow and DeleteRow are this resource's write orchestration at
// the door an in-process caller stands at. The JSON routes and the generated
// screens go through the same three bodies; a seed writer, or any composition-time
// caller that is not a person's request, stands here. The reason they are
// exported and not copied is that the three things a write must never skip are
// inside those bodies — the row lock, the tenant recheck a DELETE cannot get from
// row-level security alone, and the event that announces the change — and a
// second spelling of them in an application is a spelling that can drift.
// Authorization is not one of them: a route asks httpx, a seed run asks its
// Authorizer, and neither answer belongs in this file.
func (s Spec[T]) CreateRow(ctx context.Context, tx db.Tx[db.Tenant], e T) (T, error) {
	return s.createRow(ctx, tx, e)
}

// UpdateRow patches the columns `values` names, of this Spec's own schema, and
// is the one door that keeps a caller from writing a field the Spec declared
// immutable: the check is inside the merge those columns go through.
func (s Spec[T]) UpdateRow(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, values map[string]any) (T, error) {
	return s.updateRow(ctx, tx, id, crud.Fields[T](), values)
}

func (s Spec[T]) DeleteRow(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (T, error) {
	return s.deleteRow(ctx, tx, id)
}

// CommandOptions is what a command may differ from its Spec in. It is a struct
// and not two more parameters because a command that differs in nothing has to
// be able to say so in one word: rest.CommandOptions{}.
type CommandOptions struct {
	// Auth is who may run this command. The zero value is the Spec's own write
	// permission, which is right for a command that moves a row an
	// administrator owns and wrong for one somebody runs on their own thing:
	// adding an item to a basket is httpx.SignedIn(), and the alternative was a
	// second permission every tenant would have to grant to every shopper.
	Auth httpx.Auth

	// Collection mounts the command on the collection rather than on a row —
	// POST {Path}/{verb} — and run is handed uuid.Nil.
	//
	// The id is the whole difference, and it was a second exported function
	// until the review counted the lines: twenty-eight of its thirty-two were
	// this one's. A command about a row somebody names — resolve this task —
	// carries the id; a command about the collection, where the row is what the
	// command finds or produces, cannot. Redeeming a code is the example: the
	// caller knows the code and not the row it belongs to, so {id} in the path
	// would be asking them for the answer.
	Collection bool
}

// Command registers one lifecycle route on a Spec: POST {Path}/{id}/{verb}, or
// POST {Path}/{verb} when opts says Collection. It is guarded by the Spec's
// Write permission unless opts says otherwise, takes the request's transaction,
// declares the events it publishes, and answers failures with the same mapping
// the five routes above use.
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
func Command[I any, T crud.Entity](surfaces httpx.Surfaces, spec Spec[T], verb, summary, description string, events []string,
	run func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in I) (T, error), opts CommandOptions,
) {
	path := spec.item() + "/" + verb
	if opts.Collection {
		path = strings.TrimSuffix(spec.Path, "/") + "/" + verb
	}
	auth := spec.writeAuth()
	if opts.Auth.Declared() {
		auth = opts.Auth
	}
	// A command the installation owns is a control-plane route, whatever the
	// resource it acts on is otherwise: the guard is the fact, and the surface
	// follows the guard rather than the other way round.
	router := surfaces.App
	if auth.Operator() {
		router = surfaces.Ops
	}
	op := huma.Operation{
		OperationID: spec.Module + "-" + spec.Entity + "-" + verb,
		Method:      http.MethodPost,
		Path:        path,
		Summary:     summary,
		Description: description,
		Tags:        []string{spec.Module},
		Errors:      faults,
	}
	if len(events) > 0 {
		op.Extensions = map[string]any{httpx.EventsExtension: events}
	}
	// The same declaration, recorded on the resource, so a shell generated
	// from the catalog offers this door rather than inventing one — and recorded
	// with the work beside it, because "offers this door" was always a promise
	// this line could not keep. A screen's write goes through the closure below,
	// so the form and the JSON route perform one implementation through one
	// guard. See docs/adr/0007 and httpx.Command.Run.
	fields := crud.FieldsOf(reflect.TypeFor[I]())
	// Endpoint is the command's own absolute address, which is the one path a
	// shell can no longer derive for itself: once a command may live on the
	// control-plane surface, {path}/{id}/{verb} stops being the rule. The
	// catalog publishes it for exactly that reason — see screens.Command.Path.
	surfaces.AddCommand(spec.Module, spec.Entity, httpx.Command{
		Verb: verb, Summary: summary, Description: description,
		Collection: opts.Collection, Auth: auth,
		Endpoint: router.Prefix() + path,
		Fields:   fields,
		Run: func(ctx context.Context, id uuid.UUID, values map[string]any) error {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return problem.New(http.StatusServiceUnavailable,
					"this command is only performed inside a request")
			}
			// The same decode the PATCH route uses: one set of rules for "3" as an
			// int and for null as an empty pointer, and a field the command does not
			// take is refused rather than quietly dropped.
			var in I
			if _, err := merge(&in, fields, nil, values); err != nil {
				return err
			}
			_, err := run(ctx, tx, id, in)
			return err
		},
	})
	// The two mounts differ in their input type and in nothing else, and the
	// type is what tells huma whether there is a path parameter to bind. That
	// is the irreducible half of the difference; everything above it and the
	// answer below are shared.
	answer := func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, body *I) (T, error) {
		var in I
		if body != nil {
			in = *body
		}
		return run(ctx, tx, id, in)
	}
	if opts.Collection {
		Operation(router, op, auth, func(ctx context.Context, tx db.Tx[db.Tenant], _ uuid.UUID, in *collectionInput[I]) (T, error) {
			return answer(ctx, tx, uuid.Nil, in.Body)
		}, OperationOptions{})
		return
	}
	Operation(router, op, auth, func(ctx context.Context, tx db.Tx[db.Tenant], _ uuid.UUID, in *commandInput[I]) (T, error) {
		return answer(ctx, tx, in.ID, in.Body)
	}, OperationOptions{})
}

// commandInput is a command's path id and its body. The body is a pointer
// because huma reads a struct body as required, and "{}" is not something a
// caller should have to send to say nothing.
type commandInput[I any] struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"The row's id"`
	Body *I
}

// collectionInput is the same without the id, for a command whose path has no
// {id} to bind.
type collectionInput[I any] struct {
	Body *I
}

// emit publishes the event for a write and then runs the module's hook, both
// inside the request's transaction: an event that describes a change that
// rolled back is never seen, because it rolled back too.
func (s Spec[T]) emit(ctx context.Context, tx db.Tx[db.Tenant], verb string, e T, hook func(context.Context, db.Tx[db.Tenant], T) error) error {
	if err := events.Publish(ctx, tx, s.Event(verb), e); err != nil {
		return err
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
	names := append([]string{s.Event(published)}, s.HookEvents...)
	if len(names) > 0 {
		op.Extensions = map[string]any{httpx.EventsExtension: names}
	}
	return op
}

func (s Spec[T]) item() string { return strings.TrimSuffix(s.Path, "/") + "/{id}" }

// readRouter and writeRouter are the derivation the surface table makes from
// the two flags: an operator read or write is a control-plane route and lives
// where only the installation's host answers, and everything else is workspace
// work. The flags have already been checked against the manifest by kit/app's
// validatePermissions, which is what makes reading them here a fact rather than
// a second opinion about who may call this.
func (s Spec[T]) readRouter(surfaces httpx.Surfaces) *httpx.Router {
	if s.OperatorRead || s.readAuth().Operator() {
		return surfaces.Ops
	}
	return surfaces.App
}

func (s Spec[T]) writeRouter(surfaces httpx.Surfaces) *httpx.Router {
	if s.OperatorWrite || s.writeAuth().Operator() {
		return surfaces.Ops
	}
	return surfaces.App
}

// offers reports whether this Spec mounted the route for a verb. An empty
// Operations means all five — the answer every Spec written before the field
// existed means — which is stated once, on httpx.Resource.Offers, and read here
// through the value that carries it to the screens.
func (s Spec[T]) offers(c httpx.CRUD) bool {
	return httpx.Resource{Operations: s.Operations}.Offers(c)
}

// offersWrite reports whether this Spec mounts at least one of the three writes.
// It is the question `WritePath` answers yes to: that address is quoted at a
// caller who posted to the read door and published as the catalogue's
// `write_path`, and both are promises that something takes the write there.
func (s Spec[T]) offersWrite() bool {
	return s.offers(httpx.CRUDCreate) || s.offers(httpx.CRUDUpdate) || s.offers(httpx.CRUDDelete)
}

func (s Spec[T]) readAuth() httpx.Auth {
	if s.ReadAuth.Declared() {
		return s.ReadAuth
	}
	return (httpx.Resource{Read: s.Read, OperatorRead: s.OperatorRead}).ReadAuth()
}

// writeAuth is the declaration the three write routes and every Command carry.
func (s Spec[T]) writeAuth() httpx.Auth {
	if s.WriteAuth.Declared() {
		return s.WriteAuth
	}
	return (httpx.Resource{Write: s.Write, OperatorWrite: s.OperatorWrite}).WriteAuth()
}

// check refuses a Spec that could only produce broken routes or unroutable
// events. It panics, at the mount site, for the same reason httpx.Permission
// does: this is a wiring mistake, not a request-time condition.
func (s Spec[T]) check() {
	var bad string
	switch {
	case !strings.HasPrefix(s.Path, "/"):
		bad = fmt.Sprintf("Path %q does not start with /", s.Path)
	case s.Path == "/"+s.Module || strings.HasPrefix(s.Path, "/"+s.Module+"/") ||
		strings.HasPrefix(s.Path, "/api/v1") || strings.HasPrefix(s.Path, "/app") ||
		strings.HasPrefix(s.Path, "/ops") || strings.HasPrefix(s.Path, "/public"):
		bad = fmt.Sprintf("Path %q must be relative to the module — %q", s.Path, "/"+strings.TrimPrefix(strings.TrimPrefix(s.Path, "/api/v1/"+s.Module), "/"))
	case !events.ValidName(s.Event(Created)):
		bad = fmt.Sprintf("Module %q and Entity %q do not make an event name", s.Module, s.Entity)
	case !s.ReadAuth.Declared() && !httpx.ValidPermission(s.Read):
		// A declared guard replaces the shorthand rather than adding to it, so
		// an empty Read beside ReadAuth is the shape R3 insists on and not the
		// typo this case refuses. operationsFault names the pair incoherent when
		// both are spelled.
		bad = fmt.Sprintf("Read %q is not %q", s.Read, "<resource>:<action>")
	case !s.WriteAuth.Declared() && !httpx.ValidPermission(s.Write):
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
	if bad == "" {
		bad = s.operationsFault()
	}
	if bad == "" {
		bad = widgetFault(crud.Fields[T]())
	}
	if bad == "" {
		for _, field := range crud.Fields[T]() {
			if field.Widget != "richtext" {
				continue
			}
			if field.Type != entity.TypeString && field.Type != entity.TypeText {
				bad = fmt.Sprintf("richtext field %q must be a string", field.Name)
				break
			}
			if s.RichTextFiles == nil {
				bad = fmt.Sprintf("richtext field %q needs a Files port", field.Name)
				break
			}
		}
	}
	if bad == "" {
		bad = presentationFault(crud.Fields[T]())
	}
	if bad == "" {
		bad = displayFieldFault(crud.Fields[T]())
	}
	if bad != "" {
		panic("rest: Spec for " + s.Module + "." + s.Entity + ": " + bad)
	}
}

// operationsFault names what this Spec's operation set and its two declared
// guards get wrong, and "" when they are coherent. Every refusal here is a
// mount-time panic, like the rest of check: the alternative to refusing the
// mount is a route table that answers a verb nobody declared, a page whose
// button leads to a 404, or a guard that guards nothing.
//
// The set is checked before the guards because a guard is only coherent beside
// the routes it guards: R7's refusal of a signed-in write is a sentence about
// Update and Delete being offered.
func (s Spec[T]) operationsFault() string {
	seen := map[httpx.CRUD]bool{}
	for _, c := range s.Operations {
		if !httpx.ValidCRUD(string(c)) {
			return fmt.Sprintf("Operations names %q, which is not one of list, read, create, update or delete", string(c))
		}
		if seen[c] {
			return fmt.Sprintf("Operations names %q twice; an operation set is a set, and the catalog would publish the verb twice", string(c))
		}
		seen[c] = true
	}
	// An empty Operations means all five, and nil and an empty slice both mean
	// it: nothing here refuses it, and nothing here reads it as "none".
	//
	// A list with no read is the one withheld verb that leaves a lie on a page that
	// *is* mounted: ui/resource.table makes the identity column the way into the
	// record, so every row of a list whose read route is not mounted would link to
	// the 404 of a place nothing is served. That is the sentence the doors were
	// gated with, and here it is refused rather than drawn without its links: a
	// table of rows nobody may open is not a screen with one door missing, it is a
	// screen that does not work.
	if s.offers(httpx.CRUDList) && !s.offers(httpx.CRUDRead) {
		return "offers list without read: the list page links every row to the record route, and a resource that offers no read mounts no record route to answer it"
	}
	for _, d := range []struct {
		field, shorthand string
		auth             httpx.Auth
		operator         bool
	}{{"ReadAuth", s.Read, s.ReadAuth, s.OperatorRead}, {"WriteAuth", s.Write, s.WriteAuth, s.OperatorWrite}} {
		if !d.auth.Declared() {
			continue
		}
		switch {
		case d.shorthand != "":
			return fmt.Sprintf("declares %s and the permission %q; a guard is stated once, and %s overrides the permission it would be spelled with",
				d.field, d.shorthand, d.field)
		case d.operator:
			return fmt.Sprintf("declares %s with Operator%s; the declaration says whether it is the operator's — httpx.OperatorPermission carries that fact itself",
				d.field, strings.TrimSuffix(d.field, "Auth"))
		case d.auth.IsPublic():
			// Public would be a tenant's rows behind no guard at all. A public
			// face has a door of its own — rest.Singleton's Public and Face — and a
			// route a module mounts itself has both the surface and the sentence.
			return fmt.Sprintf("%s is public; a Spec serves a tenant's rows, so a public face is rest.Singleton's Face or a route you mount yourself", d.field)
		case d.auth.NamesAGrant() && d.auth.Feature() == "":
			// A permission has a shorthand, and two spellings of one guard is two
			// things to keep honest. With .Needing it is the only spelling that can
			// say "the grant *and* the plan feature", so that one stays. An operator
			// permission is refused here too, and its sentence has to name *both*
			// fields: its shorthand is the permission and the operator flag, and the
			// declaration beside that flag is the doubling R4 refuses above. Naming
			// only the permission would send an author to the customer's surface with
			// the operator's grant in hand.
			shorthand := strings.TrimSuffix(d.field, "Auth")
			if d.auth.Operator() {
				return fmt.Sprintf("%s names an operator permission (%s); say it with %s and Operator%s, which is the spelling kit/app checks against the manifest", d.field, d.auth, shorthand, shorthand)
			}
			return fmt.Sprintf("%s names a plain permission (%s); say it with %s, and name a plan feature with .Needing when the grant is not the whole question", d.field, d.auth, shorthand)
		}
	}
	// A write whose row the caller names, under a guard that decides nothing but
	// membership: the generic PATCH and DELETE reach the row through
	// crud.RecheckTenant, which asks whose tenant and never whose row. The door
	// for this is rest.Command, which carries an Auth and runs the module's own
	// service, where tenancy.RequirePolicy decides the object.
	if s.WriteAuth.Declared() && !s.WriteAuth.NamesAGrant() && (s.offers(httpx.CRUDUpdate) || s.offers(httpx.CRUDDelete)) {
		return "mounts update or delete under a write guard that is no grant; a write about a row the caller names is a rest.Command or a permission, because the generic routes check the tenant and not the row"
	}
	// Immutable is refused at the create and the patch, and shown read-only in
	// the two forms. A guard that guards no door is a misspelled name one level
	// up: it looks like a rule and changes nothing.
	if len(s.Immutable) > 0 && !s.offers(httpx.CRUDCreate) && !s.offers(httpx.CRUDUpdate) {
		return fmt.Sprintf("names %d Immutable field(s) and mounts neither create nor update, so nothing writes them and nothing refuses them", len(s.Immutable))
	}
	return ""
}

// widgetFault names the first field whose `ui:"widget:…"` is a name no screen
// can draw, and "" when every widget the entity names is inside the vocabulary
// kit/entity owns.
//
// The check belongs here because a Spec is where an entity, a schema and a
// mount are all in hand at once. It exists because the failure it prevents is
// silent: a widget name the renderer had not heard of drew a plain text input,
// so `widget:file` gave an installation a file component in the library and not
// one form that could ask for it. The vocabulary is kit/entity's because
// ui/forms draws the controls, and a kernel package below the presentation
// layer cannot import it to ask what a name means.
// layer cannot import it to ask what a name means.
func widgetFault(fields []crud.Field) string {
	for _, f := range fields {
		if !entity.ValidWidget(f.Widget) {
			return fmt.Sprintf("field %q names widget %q, which no screen can draw", f.Name, f.Widget)
		}
	}
	return ""
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
	if fieldErr, ok := errors.AsType[*richTextFieldError](err); ok {
		p := &richTextProblem{
			Problem: problem.New(http.StatusUnprocessableEntity, fieldErr.Error()),
			field:   fieldErr.field, issues: fieldErr.refused.Issues,
		}
		for _, issue := range fieldErr.refused.Issues {
			p.Errors = append(p.Errors, fmt.Sprintf("%s: line %d: %s: %s", fieldErr.field, issue.Line, issue.Construct, issue.Remedy))
		}
		return p
	}
	switch {
	case errors.Is(err, tenancy.ErrPolicyUnavailable), errors.Is(err, tenancy.ErrInvalidPolicyRequest):
		return problem.New(http.StatusServiceUnavailable, "POLICY_UNAVAILABLE: authorization is temporarily unavailable")
	case errors.Is(err, tenancy.ErrPolicyDenied):
		return problem.New(http.StatusForbidden, "POLICY_DENIED: this action is not allowed")
	case errors.Is(err, crud.ErrNotFound):
		return problem.NotFound("no such row, or none this tenant may see")
	case errors.Is(err, crud.ErrInvalid):
		return problem.New(http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, crud.ErrConflict):
		if _, unique := errors.AsType[*crud.UniqueConflict](err); unique {
			return problem.Conflict("A record already uses one of these values. Change the duplicate value and try again.")
		}
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
	// RawBody is the same bytes, which huma fills from the same read. It is
	// here for one question the decoded entity cannot answer: did the caller
	// name this field at all. See refuseImmutable.
	RawBody []byte
}

// patchInput is the update route's body: the fields to change, and no others.
//
// It keeps no RawBody: Body carries every top-level key the bytes carried, so
// merge's foldedName sees what this route's decoder saw. The published media
// types are not that question, and neither route's list is honest about it —
// the create declares application/octet-stream, which huma's own registry then
// answers 415 to, and both bind a +json suffix the document never declared.
// Which bodies reach which door is pinned at the door, by
// TestTheCreateDoorRefusesTheReservedNameUnderEveryMediaTypeItAdvertises; the
// mismatch in the published list is older than the rule above it.
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
// that folds onto a name the Spec reserved is refused rather than ignored,
// because a caller who spells a field wrong — or reaches for the wrong door —
// has to be told.
func merge(e any, fields []crud.Field, immutable []string, patch map[string]any) ([]string, error) {
	// The reserved names are asked of the whole body first, so the answer does
	// not depend on map order, and folded, because this map is a decoded body:
	// encoding/json binds "Status" into Status, and the exact lookup below
	// would answer "there is no field" about a field that is there.
	if name := foldedName(patch, immutable); name != "" {
		return nil, immutableRefusal(name)
	}
	target := reflect.ValueOf(e).Elem()
	columns := make([]string, 0, len(patch))
	for name, value := range patch {
		f, ok := crud.FieldNamed(fields, name)
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: there is no field %q", crud.ErrInvalid, name)
		case f.ReadOnly:
			return nil, fmt.Errorf("%w: %s is read-only", crud.ErrInvalid, name)
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

// refuseImmutable answers 422 when a create body names a field a route of its
// own owns.
//
// It reads the raw body rather than the decoded entity, because the two are not
// the same question: a decoded entity cannot say whether the caller sent
// "author": null, "author": "0000…" or nothing at all, and only the last is
// allowed. So the create route asks for the bytes as well as the struct — huma
// fills both from one read — and foldedName names what it refuses.
//
// The refusal is the patch's, word for word, because it is the same rule: a
// field a command owns is written by that command at every door. Read-only
// fields are not this; those the server owns outright and crud.Reset discards
// whatever arrived for them, which is right, because a caller sending an id is
// not reaching for a door of its own.
func refuseImmutable(body []byte, immutable []string) error {
	if len(immutable) == 0 || len(body) == 0 {
		return nil
	}
	var sent map[string]json.RawMessage
	if err := json.Unmarshal(body, &sent); err != nil {
		// A body that is not an object is huma's refusal to give, not this
		// one's: it has already failed to decode into the entity.
		return nil
	}
	if name := foldedName(sent, immutable); name != "" {
		return immutableRefusal(name)
	}
	return nil
}

// foldedName is the one question every write door asks of the top-level keys it
// was handed — a request's bytes after one read, the map that decoded into a
// struct, the form a browser posted — and asks it the way the decoder does:
// strings.EqualFold, the Unicode simple case folding encoding/json binds a field
// by when no key matches exactly, long s included. A map lookup is not that
// question, and the decoder is the authority: a key it binds into a command's
// field has to be refused by the door in front of it. It answers with the
// declared field, or "" when the body names none; of two, the Spec's order wins.
func foldedName[V any](sent map[string]V, immutable []string) string {
	for _, name := range immutable {
		for key := range sent {
			if strings.EqualFold(key, name) {
				return name
			}
		}
	}
	return ""
}

// immutableRefusal is the one message every door but the form's gives, so a
// caller who reaches for a field a command owns is told the same thing whether
// they created or patched, over the wire or beneath the HTTP.
func immutableRefusal(name string) error {
	return fmt.Errorf("%w: %s belongs to a route of its own, not to this one", crud.ErrInvalid, name)
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
