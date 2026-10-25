package screens

import (
	"context"
	"errors"
	"net/http"
	"strings"

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
		Page int    `query:"page" minimum:"1" default:"1"`
		Sort string `query:"sort"`
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
	// at is where the screens are: /app/<module>/<entity>, composed by the kernel
	// when the resource was registered. Every link is built from it. rel is the
	// same address before the prefix, which is the only form a router accepts —
	// the module wrote it, and it is the reason this shell never has to know
	// where the workspace is mounted.
	at, rel := res.Screen, res.Path
	if at == "" && !res.Singleton {
		// No address to link from, so nothing to mount. kit/rest composes this
		// address only when it mounts the list route beside it.
		return
	}
	id := "screen-" + res.Module + "-" + res.Entity + "-"
	read, write := res.ReadAuth(), res.WriteAuth()
	if res.Singleton {
		mountSingleton(router, s, o, res, at, rel, id, read, write)
		return
	}

	mountCollectionList(router, s, o, res, at, rel, id, read)
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

// mountCommands mounts one POST behind every command the resource declared, at the
// same path shape the API uses, with the command's own guard. All declared commands
// are mounted whatever any one caller may use — the guard is what distinguishes
// callers, not the route table — and each is refused if it collides with a screen
// verb.
//
// A command whose Run is nil is not mounted. There is nothing to perform, and a route
// that answers a form by doing nothing while reporting success is worse than no route:
// the person leaves the page believing the thing happened.
func mountCommands(router *httpx.Router, s page.Shell, o Options, res httpx.Resource, at, rel, id string) {
	for _, c := range res.Commands {
		for _, verb := range taken {
			if c.Verb == verb {
				panic("screens: " + res.Module + "." + res.Entity + " has a command called " + verb +
					", which is already a screen route of every entity; name a command after what it does")
			}
		}
		if c.Run == nil {
			continue
		}
		// The body is asked for only when the command has arguments. A form with
		// no fields posts an empty one, and a route that requires a body answers
		// 400 to a person who pressed the only button the command had — which is
		// what this code did before it was run: TestAnArgumentlessCommandIsAButton
		// AndNotAGuess is the failure, not a hypothetical.
		if c.Collection {
			collection := func(ctx context.Context, body []byte) (page.View, error) {
				if c.HasResult {
					return commandOutput(res, c, ctx, uuid.Nil, body, o, at, at)
				}
				if err := perform(c, ctx, uuid.Nil, body); err != nil {
					return page.View{}, err
				}
				return page.View{}, httpx.SeeOther(at)
			}
			if len(c.Fields) == 0 {
				page.Serve(router, s, page.Route{ID: id + c.Verb, Method: http.MethodPost,
					Path: strings.TrimSuffix(rel, "/") + "/" + c.Verb, Summary: c.Summary, MintsCredential: c.MintsCredential}, c.Auth,
					func(ctx context.Context, _ page.Request, _ *page.Empty) (page.View, error) {
						return collection(ctx, nil)
					})
			} else {
				page.Serve(router, s, page.Route{ID: id + c.Verb, Method: http.MethodPost,
					Path: strings.TrimSuffix(rel, "/") + "/" + c.Verb, Summary: c.Summary, MintsCredential: c.MintsCredential}, c.Auth,
					func(ctx context.Context, _ page.Request, in *formInput) (page.View, error) {
						return collection(ctx, in.RawBody)
					})
			}
			continue
		}
		item := func(ctx context.Context, rowID uuid.UUID, body []byte) (page.View, error) {
			if c.HasResult {
				return commandOutput(res, c, ctx, rowID, body, o, at, at+"/"+rowID.String())
			}
			if err := perform(c, ctx, rowID, body); err != nil {
				return page.View{}, err
			}
			return page.View{}, httpx.SeeOther(at + "/" + rowID.String())
		}
		if len(c.Fields) == 0 {
			page.Serve(router, s, page.Route{ID: id + c.Verb, Method: http.MethodPost,
				Path: rel + "/{id}/" + c.Verb, Summary: c.Summary, MintsCredential: c.MintsCredential}, c.Auth,
				func(ctx context.Context, _ page.Request, in *itemInput) (page.View, error) {
					return item(ctx, in.ID, nil)
				})
			continue
		}
		page.Serve(router, s, page.Route{ID: id + c.Verb, Method: http.MethodPost,
			Path: rel + "/{id}/" + c.Verb, Summary: c.Summary, MintsCredential: c.MintsCredential}, c.Auth,
			func(ctx context.Context, _ page.Request, in *itemFormInput) (page.View, error) {
				return item(ctx, in.ID, in.RawBody)
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

// mountSingleton mounts the screens a singleton has routes for: the record page at
// its path, and the form that writes it. No list, no new, no delete — not because
// they would look wrong but because rest.Singleton mounts no such route, so a page
// offering one is a page that sends a person to a refusal.
//
// The verbs are the collection's own verbs, so a screen of any resource is named the
// same way whatever shape it has. Get takes the nil id because rest.Singleton ignores
// it on purpose: there is one row, and a screen that asked for another would be
// asking about a tenant it cannot see.
func mountSingleton(router *httpx.Router, s page.Shell, o Options, res httpx.Resource, at, rel, id string, read, write httpx.Auth) {
	page.Serve(router, s, page.Route{ID: id + "read", Method: http.MethodGet, Path: rel, Summary: "The " + res.Entity + " record"}, read,
		func(ctx context.Context, req page.Request, _ *page.Empty) (page.View, error) {
			row, err := res.Get(ctx, uuid.Nil)
			if err != nil {
				return page.View{}, err
			}
			return detailView(res, ctx, localized(o, req), at, row, res.Writable(ctx))
		})

	if !writeAvailable(res, httpx.CRUDUpdate) {
		return
	}

	page.Serve(router, s, page.Route{ID: id + "edit", Method: http.MethodGet, Path: rel + "/edit", Summary: "The edit-" + res.Entity + " form"}, write,
		func(ctx context.Context, req page.Request, _ *page.Empty) (page.View, error) {
			row, err := res.Get(ctx, uuid.Nil)
			if err != nil {
				return page.View{}, err
			}
			o := localized(o, req)
			return Form(res, o, at, o.Text("screens.edit_item", "Edit %s", entryName(res, o)), row, nil, "", false), nil
		})

	page.Serve(router, s, page.Route{ID: id + "update", Method: http.MethodPost, Path: rel, Summary: "Update the " + res.Entity}, write,
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

// refusedForm is the one redraw every submit route answers a refused write with: the values
// the person typed, the field the write blamed if it blamed one, and the verdict behind the
// refusal in the words the refusal page uses. Three routes post a generated form — create,
// update, and a singleton's update — and one redraw is what keeps a settings page from
// printing a driver's error where the form beside it says "We couldn't save this."
//
// The heading is a message key rather than a sentence because a redraw is served in the
// language the request was answered in, and so is the entry's own noun beside it.
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

// localized is the shell's options with this request's language. Options is a
// small value, so each render carries its own copy rather than the shell
// holding one language for everybody.
func localized(o Options, req page.Request) Options {
	o.Locale = req.Locale
	return o
}

// refusedWrite is the verdict behind a refused write as the refusal page words it: the
// sentence its notice shows, replacing the code-prefixed line the write door hands back,
// with the heading and tone to show it under. A failure that page has no words for keeps
// the line its writer wrote: for a field the caller sent, or a row rule that named its
// reason, that line is the one thing the person can act on.
//
// An error carrying no problem at all is not such a line. kit/rest maps everything a
// caller may act on into a problem — a missing row, a field, a revision that moved — so
// what arrives unmapped is a failure of ours whose text is a driver's or a subsystem's,
// and it is worded as the server fault it is. The person keeps a sentence and the typed
// value; the text itself stays out of the page.
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
