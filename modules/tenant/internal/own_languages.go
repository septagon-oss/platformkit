package internal

// own_languages.go is the tenant's own languages as a door, not as a page.
//
// The command has existed since the tenant could say this about itself: read the
// declaration, replace it at the revision it was read at, refuse a language with no
// copy and an empty default, publish one event. What it did not have was an address.
// The only way in was the HTML page modules/admin writes, which makes the fact
// unreachiable from anything that is not a browser — a native shell reading
// /api/v1/app/resources found a tenant it could not set the language of, and a
// script that wanted to move a customer to Portuguese had to post form fields at a
// screen. So: GET and PUT at the module's own app address, both performing the one
// command, and the resource and command the catalog answers a shell with so the
// door is discoverable rather than folklore.
//
// It is mounted on the tenant's own transaction (`db.Tx[db.Tenant]`), which is the
// difference between this and the five control-plane routes in handler.go: those
// take a system transaction and name a tenant, and this one has no tenant to name
// because the request already resolved to one. A door that took a tenant id would be
// a door a customer could point at their neighbour, and row-level security would be
// the only thing standing between them — which is why the guard below is a tenant
// permission and not an operator one, and why no argument of any kind names a
// subject here.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/google/uuid"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// The types below carry the module's name in theirs. huma names a schema after the
// Go type, across the whole installation, not after the package — so a module whose
// type is called `languageOutput` refuses to mount beside another module's, with a
// panic at composition. That is the one reason the names are longer than this
// package needs them to be.

// ownLanguages is the address, relative to this module's app surface:
// /api/v1/tenant/languages at a composed installation.
const ownLanguages = "/languages"

// mountOwnLanguages registers the two routes, the resource they belong to, and the
// command the catalog names them by. One `guard` value is handed to all four, so the
// page, the JSON door and the catalog entry cannot disagree about who may use them —
// and `Service.SetOwnLocale` asks the same question again inside the transaction that
// writes, which is what makes the guard an early refusal rather than the whole answer.
func mountOwnLanguages(s httpx.Surfaces, svc contracts.Service) {
	guard := httpx.Permission(contracts.PermissionLocaleManage)
	app := s.App

	httpx.Register(app, huma.Operation{
		OperationID: "tenant-languages-read",
		Method:      http.MethodGet,
		Path:        ownLanguages,
		Summary:     "Read this tenant's languages",
		Description: "The language a request with no usable preference is answered in, the set the browser's list is intersected with, and the revision both are held at. The revision is what to send back as expectedRevision: it moves once per accepted declaration, and a declaration that changes nothing leaves it where it was.",
		Tags:        []string{"tenant"},
		Errors:      []int{http.StatusForbidden, http.StatusServiceUnavailable},
	}, guard, func(ctx context.Context, _ *struct{}) (*ownLanguagesOutput, error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
		}
		held, err := svc.OwnLocale(ctx, tx)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return &ownLanguagesOutput{Body: ownLanguagesRead{
			Default: held.Default, Supported: held.Supported, Revision: held.Revision,
		}}, nil
	})

	httpx.Register(app, huma.Operation{
		OperationID: "tenant-languages-set",
		Method:      http.MethodPut,
		Path:        ownLanguages,
		Summary:     "Say which languages this tenant is served in",
		Description: "Replaces this tenant's declaration at the revision it was read at. A language this installation ships no copy for is refused, an empty default is refused, and an expected revision the row has left behind is refused with 409 and writes nothing: the page that brought it saw a declaration somebody else has since replaced. A declaration that changes nothing publishes nothing, however stale the revision it came with.",
		Tags:        []string{"tenant"},
		Errors: []int{http.StatusForbidden, http.StatusUnprocessableEntity,
			http.StatusConflict, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{contracts.EventLocaleSet}},
	}, guard, func(ctx context.Context, in *ownLanguagesInput) (*ownLanguagesOutput, error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
		}
		landed, err := svc.SetOwnLocale(ctx, tx, in.Body.ExpectedRevision, contracts.SetLocale{
			Default: in.Body.Default, Supported: in.Body.Supported,
		})
		if err != nil {
			// A refusal answers with no body at all: `rest.Fault` keeps the
			// conflict a conflict, and a response that looked like the declaration
			// that was refused would be a lie about what happened.
			return nil, rest.Fault(err)
		}
		return &ownLanguagesOutput{Body: ownLanguagesRead{
			Default: landed.DefaultLocale, Supported: landed.Locales, Revision: landed.LocaleRevision,
		}}, nil
	})

	// The resource the two routes belong to. It is registered here, beside them,
	// for the reason kit/rest registers one from Spec.Mount: a catalog and an API
	// that were written separately disagree eventually, and a shell that asked the
	// catalog would be asking something nobody maintains.
	//
	// Its read and write closures are the same two commands the routes call, in the
	// same request transaction — so a page that holds this resource performs the
	// write the JSON door performs, and there is no second implementation to keep
	// honest. Create and Delete refuse rather than being nil: a tenant has one set
	// of languages, and it is neither made nor removed.
	res := httpx.Resource{
		Module: "tenant", Entity: "languages",
		Name:      entity.Name{One: "Language", Other: "Languages"},
		Path:      ownLanguages,
		Read:      contracts.PermissionLocaleManage,
		Write:     contracts.PermissionLocaleManage,
		Singleton: true,
		Screen:    app.PagePath(ownLanguages),
		Schema: crud.Schema{
			Module: "tenant", Entity: "languages", Path: app.Prefix() + ownLanguages,
			Fields: languageFields(),
		},
		List: func(ctx context.Context, _ crud.Query) ([]map[string]any, int64, error) {
			one, err := answeredLanguages(ctx, svc)
			if err != nil {
				return nil, 0, err
			}
			return []map[string]any{one}, 1, nil
		},
		Get: func(ctx context.Context, _ uuid.UUID) (map[string]any, error) {
			return answeredLanguages(ctx, svc)
		},
		Create: func(context.Context, map[string]any) (map[string]any, error) {
			return nil, oneSetOfLanguages()
		},
		Delete: func(context.Context, uuid.UUID) error { return oneSetOfLanguages() },
		Update: func(ctx context.Context, _ uuid.UUID, values map[string]any) (map[string]any, error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, unreachableDatabase()
			}
			in, err := declaredLanguages(values)
			if err != nil {
				return nil, err
			}
			landed, err := svc.SetOwnLocale(ctx, tx, in.ExpectedRevision, contracts.SetLocale{
				Default: in.Default, Supported: in.Supported,
			})
			if err != nil {
				return nil, rest.Fault(err)
			}
			return declared(landed.DefaultLocale, landed.Locales, landed.LocaleRevision), nil
		},
	}
	s.RegisterResource(res)

	// The command the catalog names the write by. A shell cannot derive this
	// address the way it derives {resource}/{id}/{verb}: the write *is* the
	// resource's own path, and it is a replace rather than a verb beneath it, so
	// Endpoint is what a native client has to read rather than guess.
	//
	// Self because the caller is the subject: nothing in the arguments names a
	// tenant, and a control that writes the reader while labelled with a row they
	// were looking at is a control that lies about what it does.
	s.AddCommand("tenant", "languages", httpx.Command{
		Verb: "languages", Collection: true, Self: true, Auth: guard,
		Summary: "Say which languages this tenant is served in",
		Description: "Replaces the declaration of which languages this tenant is answered in. " +
			"Bring the revision the read answered with; a revision the tenant has left behind is refused and writes nothing.",
		Endpoint: app.Prefix() + ownLanguages,
		Fields:   languageFields(),
		Run: func(ctx context.Context, _ uuid.UUID, values map[string]any) error {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return unreachableDatabase()
			}
			in, err := declaredLanguages(values)
			if err != nil {
				return err
			}
			_, err = svc.SetOwnLocale(ctx, tx, in.ExpectedRevision, contracts.SetLocale{
				Default: in.Default, Supported: in.Supported,
			})
			return rest.Fault(err)
		},
	})
}

// ownLanguagesWrite is the declaration a caller replaces the tenant's with, and the
// revision it was read at. Locale fields are not pointers: an empty set of ticks is
// a value — a tenant served in one language alone is a real declaration — which is
// why the command's floor is the default and not the list.
type ownLanguagesWrite struct {
	Default string `json:"default" maxLength:"35" doc:"The language a request with no usable preference is answered in" example:"pt-PT"`
	// Supported is a set: duplicates are dropped and the order means nothing,
	// because the only position that means anything is Default.
	Supported        []string `json:"supported" doc:"Every other language this tenant is served in" example:"en"`
	ExpectedRevision int64    `json:"expectedRevision" doc:"The revision GET /api/v1/tenant/languages answered" example:"2"`
}

// ownLanguagesRead is what the tenant's declaration answers with: the pair, and the
// revision a write has to bring back. It is a type of its own rather than the write
// with one field renamed because the API document is built from Go type names — one
// schema standing for both directions would be one document that cannot say which
// fields it takes and which it gives.
type ownLanguagesRead struct {
	Default   string   `json:"default" doc:"The language a request with no usable preference is answered in" example:"pt-PT"`
	Supported []string `json:"supported" doc:"Every other language this tenant is served in" example:"en"`
	Revision  int64    `json:"revision" doc:"The revision the declaration is held at, which is what to send back as expectedRevision" example:"2"`
}

type ownLanguagesInput struct {
	Body ownLanguagesWrite `required:"true"`
}

type ownLanguagesOutput struct {
	Body ownLanguagesRead `required:"true"`
}

// languageFields is the schema both routes publish and the two closures decode
// against. Revision is read-only because the row owns it: a body that could set it
// would be a caller writing its own optimistic-locking counter.
func languageFields() []entity.Field {
	fields := crud.FieldsOf(reflect.TypeFor[ownLanguagesWrite]())
	for _, f := range crud.FieldsOf(reflect.TypeFor[ownLanguagesRead]()) {
		if f.Name == "revision" {
			f.ReadOnly = true
			fields = append(fields, f)
		}
	}
	return fields
}

// answeredLanguages is the read both resource closures make, in the caller's own
// tenant transaction — the same read the GET route performs.
func answeredLanguages(ctx context.Context, svc contracts.Service) (map[string]any, error) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return nil, unreachableDatabase()
	}
	l, err := svc.OwnLocale(ctx, tx)
	if err != nil {
		return nil, rest.Fault(err)
	}
	return declared(l.Default, l.Supported, l.Revision), nil
}

// oneSetOfLanguages is what Create and Delete answer with: there is one declaration
// per tenant, and it is neither made nor removed. It is a refusal rather than a nil
// closure because the generator and the catalog call every door, and a nil is a
// panic where a sentence belongs.
func oneSetOfLanguages() error {
	return problem.Conflict("a tenant has one set of languages, and it is neither created nor removed")
}

// unreachableDatabase is what a closure that needs a transaction answers without
// one — the same answer every other door in this module gives, so one 503 means one
// thing here.
func unreachableDatabase() error {
	return problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
}

// declared is one declaration as the catalog's row maps: the same three names the
// schema publishes, so a screen's field and a route's body field are one name.
func declared(def string, supported []string, revision int64) map[string]any {
	return map[string]any{"default": def, "supported": supported, "revision": revision}
}

// declaredLanguages reads what a caller submitted into the declaration. A key this
// door does not take is refused rather than dropped: a form that posted
// `tenantId` and was quietly told nothing is a form that believes it wrote the
// neighbour, which is the same rule kit/rest's PATCH merge keeps.
func declaredLanguages(values map[string]any) (ownLanguagesWrite, error) {
	raw, err := json.Marshal(values)
	if err != nil {
		return ownLanguagesWrite{}, problem.New(http.StatusUnprocessableEntity,
			"this declaration could not be read")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var in ownLanguagesWrite
	if err := decoder.Decode(&in); err != nil {
		return ownLanguagesWrite{}, problem.New(http.StatusUnprocessableEntity,
			"this declaration could not be read: "+err.Error())
	}
	return in, nil
}
