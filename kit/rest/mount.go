package rest

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// Mount registers the offered CRUD routes and resource callbacks, declaring
// guards and events before serving. Read and write routers may differ for
// operator grants; generated screens stay in the workspace. Invalid Specs panic
// before mounting. Request failures use Fault.
func (s Spec[T]) Mount(surfaces httpx.Surfaces) {
	s.Module = mountedUnder(s.Module, surfaces.App, "Spec", s.Entity)
	s.check()
	schema := s.Schema()
	read, write := s.readRouter(surfaces), s.writeRouter(surfaces)
	res := s.resource()
	// Only a resource offering List has a generated collection page to advertise.
	if s.offers(httpx.CRUDList) {
		res.Screen = surfaces.App.PagePath(s.Path)
	}
	res.Schema.Path = read.Prefix() + s.Path
	if write.Prefix() != read.Prefix() && s.offersWrite() {
		// Publish a separate write address only when a write route exists there.
		res.WritePath = write.Prefix() + s.Path
	}
	surfaces.RegisterResource(res) // the same entity, for the generated screens
	if s.History {
		s.mountHistory(surfaces, read)
	}

	s.mountList(read, schema)
	s.mountCreate(write)
	s.mountRead(read, schema)
	s.mountUpdate(write, schema)
	s.mountDelete(write)
	// Translation commands require translatable fields and a write grant,
	// matching the generic row-write guard.
	if len(translatableFields[T]()) > 0 && s.writeAuth().NamesAGrant() {
		s.mountTranslationDoors(surfaces)
	}
	s.annotateFields(read, write, schema)
}

// mountedUnder is the kernel's fill of the one identity field a resource may
// leave empty: the module a resource lives in is the module whose router it
// mounts on, so a manifest with nothing to add can leave Module out and the
// operation ids, the document tag and the event names all carry the name the
// composition registered. A Spec that names a different module is refused here,
// in rest's own shape for a wiring mistake — a panic at the mount site, naming
// both — because filing routes and events under a module whose manifest declares
// neither is a fault no later gate could explain.
func mountedUnder(declared string, app *httpx.Router, kind, entity string) string {
	// A router that names no module is the composition's own, or the zero
	// httpx.Surfaces a test mounts on to ask check() alone. Neither fills
	// anything, so neither contradicts: the declared name stands and check()
	// stays the one that answers.
	router := ""
	if app != nil {
		router = app.Module()
	}
	if router == "" {
		return declared
	}
	if declared == "" {
		return router
	}
	if declared != router {
		panic(fmt.Sprintf("rest: %s for %q is mounted on module %q's router and names module %q; a resource lives in the module whose manifest declares it", kind, entity, router, declared))
	}
	return declared
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

// readAuth is this Spec's read guard, asked of httpx rather than derived here:
// the three fields that can state it go into the value whose method already owns
// the rule, so one answer decides this route's surface, the registered resource
// beside it and the document above both.
func (s Spec[T]) readAuth() httpx.Auth {
	return (httpx.Resource{Read: s.Read, OperatorRead: s.OperatorRead, ReadBy: s.ReadAuth}).ReadAuth()
}

// writeAuth is the declaration the three write routes and every Command carry.
// See readAuth for why it is asked and not derived.
func (s Spec[T]) writeAuth() httpx.Auth {
	return (httpx.Resource{Write: s.Write, OperatorWrite: s.OperatorWrite, WriteBy: s.WriteAuth}).WriteAuth()
}

func (s Spec[T]) mountList(read *httpx.Router, schema crud.Schema) {
	if s.offers(httpx.CRUDList) {
		listOp := s.op("list", http.MethodGet, s.Path, 0,
			"List "+s.Entity+"s", "Sortable and filterable by: "+strings.Join(names(schema.Fields), ", "))
		if len(translatableFields[T]()) > 0 {
			httpx.Register(read, listOp, s.readAuth(), func(ctx context.Context, in *translatedListInput) (*TranslatedPage[T], error) {
				out, served, err := s.pageRows(ctx, in.page(), schema, in.Lang)
				if err != nil {
					return nil, err
				}
				out.ContentLanguage = served
				return out, nil
			})
		} else {
			httpx.Register(read, listOp, s.readAuth(), func(ctx context.Context, in *listInput) (*Page[T], error) {
				out, err := s.plainRows(ctx, *in, schema)
				return out, err
			})
		}
	}
}

func (s Spec[T]) mountCreate(write *httpx.Router) {
	if s.offers(httpx.CRUDCreate) {
		httpx.Register(write, s.op("create", http.MethodPost, s.Path, http.StatusCreated,
			"Create a "+s.Entity, "The tenant, the id and the timestamps are set by the server."),
			s.writeAuth(), func(ctx context.Context, in *bodyInput[T]) (*Item[T], error) {
				tx, err := transaction(ctx)
				if err != nil {
					return nil, err
				}
				// Create refuses command-owned fields just as PATCH does; otherwise a
				// caller could bypass the owning command before the first update.
				if err := refuseImmutable(in.RawBody, s.Immutable); err != nil {
					return nil, Fault(err)
				}
				candidate, err := s.choiceCreateBody(in.RawBody, in.Body)
				if err != nil {
					return nil, Fault(err)
				}
				e, err := s.createRow(ctx, tx, candidate)
				if err != nil {
					return nil, Fault(err)
				}
				return &Item[T]{Body: e}, nil
			})
	}
}

func (s Spec[T]) mountRead(read *httpx.Router, schema crud.Schema) {
	if s.offers(httpx.CRUDRead) {
		readOp := s.op("read", http.MethodGet, s.item(), 0, "Read a "+s.Entity, "")
		if len(translatableFields[T]()) > 0 {
			httpx.Register(read, readOp, s.readAuth(), func(ctx context.Context, in *translatedIDInput) (*TranslatedItem[T], error) {
				out, served, err := s.itemRow(ctx, in.ID, in.Lang)
				if err != nil {
					return nil, err
				}
				out.ContentLanguage = served
				return out, nil
			})
		} else {
			httpx.Register(read, readOp, s.readAuth(), func(ctx context.Context, in *idInput) (*taggedItem[T], error) {
				e, err := s.oneRow(ctx, in.ID)
				if err != nil {
					return nil, err
				}
				return &taggedItem[T]{ETag: s.revisionTag(schema.Fields, e), Body: e}, nil
			})
		}
	}
}

func (s Spec[T]) mountUpdate(write *httpx.Router, schema crud.Schema) {
	if s.offers(httpx.CRUDUpdate) {
		if len(translatableFields[T]()) > 0 {
			// Translated PATCH honors the same revision tag as a source edit, then
			// dispatches according to the requested language. A foreign-language body
			// must not overwrite the source columns.
			httpx.Register(write, s.op("update", http.MethodPatch, s.item(), 0,
				"Update a "+s.Entity,
				"Only the fields present in the body change; read-only fields are refused. With ?lang= naming a language other than the tenant's own, the body writes that language's translation of its fields and the record itself is untouched."),
				s.writeAuth(), func(ctx context.Context, in *translatedPatchInput) (*taggedItem[T], error) {
					tx, err := transaction(ctx)
					if err != nil {
						return nil, err
					}
					if in.Lang != "" && in.Lang != db.TenantOf(tx).Languages.Default {
						e, err := s.localePatch(ctx, tx, in.ID, in.Lang, in.Body, in.IfMatch)
						return &taggedItem[T]{ETag: s.revisionTag(schema.Fields, e), Body: e}, Fault(err)
					}
					e, err := s.updateChoiceRow(ctx, tx, in.ID, schema.Fields, in.Body, in.IfMatch)
					if err != nil {
						return nil, Fault(err)
					}
					return &taggedItem[T]{ETag: s.revisionTag(schema.Fields, e), Body: e}, nil
				})
		} else {
			httpx.Register(write, s.op("update", http.MethodPatch, s.item(), 0,
				"Update a "+s.Entity, "Only the fields present in the body change; read-only fields are refused."),
				s.writeAuth(), func(ctx context.Context, in *patchInput) (*taggedItem[T], error) {
					tx, err := transaction(ctx)
					if err != nil {
						return nil, err
					}
					e, err := s.updateChoiceRow(ctx, tx, in.ID, schema.Fields, in.Body, in.IfMatch)
					if err != nil {
						return nil, Fault(err)
					}
					return &taggedItem[T]{ETag: s.revisionTag(schema.Fields, e), Body: e}, nil
				})
		}
	}
}

func (s Spec[T]) mountDelete(write *httpx.Router) {
	if s.offers(httpx.CRUDDelete) {
		httpx.Register(write, s.op("delete", http.MethodDelete, s.item(), http.StatusNoContent,
			"Delete a "+s.Entity, ""),
			s.writeAuth(), func(ctx context.Context, in *deleteInput) (*struct{}, error) {
				tx, err := transaction(ctx)
				if err != nil {
					return nil, err
				}
				if _, err := s.deleteRow(ctx, tx, in.ID, in.IfMatch); err != nil {
					return nil, Fault(err)
				}
				return nil, nil
			})
	}
}

func (s Spec[T]) annotateFields(read, write *httpx.Router, schema crud.Schema) {
	for _, field := range schema.Fields {
		if field.Widget == "richtext" {
			httpx.SetFieldMediaType[T](read, field.Name, "text/markdown")
		}
		if field.Type == entity.TypePicture {
			httpx.SetFieldPicture[T](read, field.Name)
		}
	}
	if s.offers(httpx.CRUDUpdate) {
		var writable []crud.Field
		for _, field := range schema.Fields {
			accepted, ok := crud.FieldNamed(schema.Fields, field.Name)
			if ok && !entity.ServerOwned(accepted) && foldedName(map[string]any{field.Name: nil}, s.Immutable) == "" {
				writable = append(writable, accepted)
			}
		}
		httpx.SetPatchSchema[T](write, s.item(), writable)
	}
}
