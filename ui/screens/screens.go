package screens

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// perPage is a screenful: the page the renderers link and the page the
// collection route serves by default are one number, which the adapter's test
// holds the two owners to.
const perPage = resource.PerPage

// Typed request inputs for collection, record and form routes.
type (
	readInput struct {
		ID string `path:"id"`
	}
	listInput struct {
		Page   int      `query:"page" minimum:"1" default:"1"`
		Sort   string   `query:"sort"`
		Filter []string `query:"filter,explode"`
	}
	itemInput struct {
		ID uuid.UUID `path:"id" format:"uuid"`
	}
	formInput struct {
		RawBody []byte `contentType:"application/x-www-form-urlencoded"`
	}
	itemFormInput struct {
		ID      uuid.UUID `path:"id" format:"uuid"`
		RawBody []byte    `contentType:"application/x-www-form-urlencoded"`
	}
)

// Mount registers the offered resource screens with their declared guards.
// router is the resource module's workspace router. A collection needs a screen
// address; a singleton uses its own read/write paths. Commands retain their
// own guards and a verb collision panics. See docs/adr/0008.
func Mount(router *httpx.Router, s page.Shell, o Options, res httpx.Resource) {
	if res.Guided != nil {
		mountGuided(router, s, o, res)
		return
	}
	// A read-only query answers a form and nothing else: no list, no record, no
	// write route to withhold.
	if res.InputRun != nil {
		mountReadInput(router, s, o, res)
		return
	}
	// at names the served page; rel is its module-relative router address.
	at, rel := res.Screen, res.Path
	if at == "" && !res.Singleton {
		// No address to link from, so nothing to mount. kit/rest composes this
		// address only when it mounts the list route beside it.
		return
	}
	router = router.WithViews(declaredViews(res)...)
	id := "screen-" + res.Module + "-" + res.Entity + "-"
	read, write := res.ReadAuth(), res.WriteAuth()
	mountFormSteps(router, s, o, res)
	if res.Singleton {
		mountSingleton(router, s, o, res, at, rel, id, read, write)
		return
	}

	mountCollectionList(router, s, o, res, id)
	mountCollectionNew(router, s, o, res, at, rel, id, write)
	mountCollectionCreate(router, s, o, res, at, rel, id, write)
	mountCollectionRead(router, s, o, res, at, rel, id, read)
	mountCollectionEdit(router, s, o, res, at, rel, id, write)
	mountCollectionUpdate(router, s, o, res, at, rel, id, write)
	mountCollectionDelete(router, s, o, res, at, rel, id, write)

	mountCommands(router, s, o, res, at, rel, id)
}

// screens are the verbs this adapter already mounts a route behind. A command named
// one of them would be mounted twice under one operation id, and two routes with one
// id is a shell that cannot mount at all — the failure mode that once shipped an album
// section nobody could open. It is refused at the mount site, in the application's own
// boot, rather than discovered by a person clicking the wrong thing.
var taken = []string{"list", "new", "create", "read", "edit", "update", "delete"}

// mountCommands registers callable command POST routes with their own guards.
// CRUD-name collisions panic at composition; missing closures mount nothing.
func mountCommands(router *httpx.Router, s page.Shell, o Options, res httpx.Resource, at, rel, id string) {
	for _, c := range res.Commands {
		for _, verb := range taken {
			if c.Verb == verb {
				panic("screens: " + res.Module + "." + res.Entity + " has a command called " + verb +
					", which is already a screen route of every entity; name a command after what it does")
			}
		}
		if c.Run == nil || c.Present.System || c.Collection && !res.Offers(httpx.CRUDList) || !c.Collection && !res.Offers(httpx.CRUDRead) {
			continue
		}
		// An argumentless command accepts an empty POST body.
		if c.Collection {
			collection := func(ctx context.Context, req page.Request, body []byte) (page.View, error) {
				return commandResponse(res, c, ctx, uuid.Nil, body, localized(o, req), at, at)
			}
			if len(c.Fields) == 0 {
				page.Serve(router, s, page.Route{View: screenRef(res, "command", c.Verb), Response: commandResponseKind(c), ID: id + c.Verb, Method: http.MethodPost,
					Path: strings.TrimSuffix(rel, "/") + "/" + c.Verb, Summary: c.Summary, MintsCredential: c.MintsCredential}, c.Auth,
					func(ctx context.Context, req page.Request, _ *page.Empty) (page.View, error) {
						return collection(ctx, req, nil)
					})
			} else {
				page.Serve(router, s, page.Route{View: screenRef(res, "command", c.Verb), Response: commandResponseKind(c), ID: id + c.Verb, Method: http.MethodPost,
					Path: strings.TrimSuffix(rel, "/") + "/" + c.Verb, Summary: c.Summary, MintsCredential: c.MintsCredential}, c.Auth,
					func(ctx context.Context, req page.Request, in *formInput) (page.View, error) {
						return collection(ctx, req, in.RawBody)
					})
			}
			continue
		}
		item := func(ctx context.Context, req page.Request, rowID uuid.UUID, body []byte) (page.View, error) {
			return commandResponse(res, c, ctx, rowID, body, localized(o, req), at, at+"/"+rowID.String())
		}
		if len(c.Fields) == 0 {
			page.Serve(router, s, page.Route{View: screenRef(res, "command", c.Verb), Response: commandResponseKind(c), ID: id + c.Verb, Method: http.MethodPost,
				Path: rel + "/{id}/" + c.Verb, Summary: c.Summary, MintsCredential: c.MintsCredential}, c.Auth,
				func(ctx context.Context, req page.Request, in *itemInput) (page.View, error) {
					return item(ctx, req, in.ID, nil)
				})
			continue
		}
		page.Serve(router, s, page.Route{View: screenRef(res, "command", c.Verb), Response: commandResponseKind(c), ID: id + c.Verb, Method: http.MethodPost,
			Path: rel + "/{id}/" + c.Verb, Summary: c.Summary, MintsCredential: c.MintsCredential}, c.Auth,
			func(ctx context.Context, req page.Request, in *itemFormInput) (page.View, error) {
				return item(ctx, req, in.ID, in.RawBody)
			})
	}
}

// perform is the form's half of rest.Command: the arguments come through the same
// refusal of unknown names the update form uses, and the work is the closure the
// command was registered with — the one its JSON route calls. A command that
// disagrees with an argument says so itself, with ErrInvalid, and the kernel maps that
// to the same 422 the API gives, so the form is not a second opinion about what a bad
// argument means.
func perform(c httpx.Command, ctx context.Context, id uuid.UUID, body []byte) error {
	values, err := rest.CommandValues(body, c.Fields)
	if err != nil {
		return rest.Fault(err)
	}
	// rest.Fault is the same mapping the JSON route answers with, which is the
	// whole reason the form does not need to know what a refused argument looks
	// like: 422 for something sent wrong, 409 for a state that contradicts the
	// command, 404 for a row this tenant cannot see. A screen with its own mapping
	// would be a second opinion about what those mean.
	return rest.Fault(c.Run(ctx, id, values))
}

// mountSingleton registers the id-less read/update routes the resource offers.
// It never creates collection, new or delete routes.
func mountSingleton(router *httpx.Router, s page.Shell, o Options, res httpx.Resource, at, rel, id string, read, write httpx.Auth) {
	page.Serve(router, s, page.Route{View: screenRef(res, "detail", ""), ID: id + "read", Method: http.MethodGet, Path: rel, Summary: "The " + res.Entity + " record"}, read,
		func(ctx context.Context, req page.Request, _ *page.Empty) (page.View, error) {
			row, err := res.Get(ctx, uuid.Nil)
			if err != nil {
				return page.View{}, err
			}
			return detailView(res, ctx, localized(o, req), at, row, res.Writable(ctx))
		})

	if !writeAvailable(res, httpx.CRUDUpdate) || stepped(res, "update") {
		return
	}

	page.Serve(router, s, page.Route{View: screenRef(res, "edit", ""), ID: id + "edit", Method: http.MethodGet, Path: rel + "/edit", Summary: "The edit-" + res.Entity + " form"}, write,
		func(ctx context.Context, req page.Request, _ *page.Empty) (page.View, error) {
			row, err := res.Get(ctx, uuid.Nil)
			if err != nil {
				return page.View{}, err
			}
			o := localized(o, req)
			return Form(res, o, at, o.Text("screens.edit_item", "Edit %s", entryName(res, o)), row, nil, "", false), nil
		})

	page.Serve(router, s, page.Route{View: screenRef(res, "edit", ""), ID: id + "update", Method: http.MethodPost, Path: rel, Summary: "Update the " + res.Entity}, write,
		func(ctx context.Context, req page.Request, in *formInput) (page.View, error) {
			sent, err := rest.UpdateValues(in.RawBody, res.Schema.Fields, nil)
			if err == nil {
				if _, err = res.Update(ctx, uuid.Nil, rest.Writable(sent, res.Immutable)); err == nil {
					return page.View{}, httpx.SeeOther(at)
				}
			}
			return refusedForm(res, o, req, at, "screens.edit_item", "Edit %s", false, sent, err), nil
		})
}

// refusedForm redraws the submitted values, field errors and localized refusal.
// It does not replace a submitted revision with a new read.
func refusedForm(res httpx.Resource, o Options, req page.Request, action, titleKey, titleEnglish string, create bool, sent map[string]any, err error) page.View {
	o = localized(o, req)
	errs, detail := rest.FieldErrorsIn(err, res.Schema.Fields, words(o, req))
	detail, o.Refusal = refusedWrite(req.Locale, err, detail)
	return Form(res, o, action, o.Text(titleKey, titleEnglish, entryName(res, o)), sent, errs, detail, create)
}

// words is this request's formatter for the copy a package owns but does not
// speak: a message key and the English to show where no catalogue was composed.
// It is the shape rest.FieldErrorsIn asks for, and kit/richtext.Catalogues holds
// the sentences for a refused Markdown field.
func words(o Options, req page.Request) func(key, fallback string) string {
	loc := localized(o, req)
	return func(key, fallback string) string {
		if loc.Locale == nil {
			return fallback
		}
		return loc.Locale.Text(key, fallback)
	}
}

// localized is the shell's options with this request's language and the instant
// its renders measure relative values against. Options is a small value, so each
// render carries its own copy rather than the shell holding one language for
// everybody, and the request samples the instant once rather than the renderer
// asking a clock per value: two values on one page measured against two nows
// would be two requests dressed as one. A shell that holds a Clock names the
// instant itself; this fallback is for the shells that hold none.
func localized(o Options, req page.Request) Options {
	o.Locale = req.Locale
	if o.Rendered.IsZero() {
		o.Rendered = time.Now().UTC()
	}
	return o
}

// refusedWrite maps known problems to localized notice copy and tone.
// Actionable field prose is retained; internal subsystem errors stay private.
func refusedWrite(loc *page.Locale, err error, kept string) (string, *resource.Refusal) {
	status, detail := http.StatusInternalServerError, ""
	if p, ok := errors.AsType[*problem.Problem](err); ok {
		status, detail = p.Status, p.Detail
	}
	title, line, tone, ok := page.RefusalWords(loc, status, detail)
	if !ok {
		return kept, nil
	}
	return line, &resource.Refusal{Heading: title, Tone: tone}
}
