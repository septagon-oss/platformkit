package screens

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// perPage is a screenful: the page the renderers link and the page the
// collection route serves by default are one number, which the adapter's test
// holds the two owners to.
const perPage = resource.PerPage

// The inputs. huma needs a type per shape, and there are four: a page of a
// list, one row, a form, and a form about one row.
type (
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

// Mount is the screens of one resource, each a renderer behind page.Serve.
// Everything about them comes from the resource — its own path, its two
// permissions, its schema, and the doors it has from which operations it
// actually mounted.
//
// router is the workspace router of the resource's own module (`s.App.ForModule("task")`
// for the task screens), so every address below is composed by the kernel from
// the module and the surface rather than spelled out by the shell that generated
// the page. A screen is always workspace work, whatever surface its API's reads
// answer on: a person stands in front of it, and the page declares the same
// permission the JSON route beside it does.
//
// A collection gets the seven: list, new, create, read, edit, update, delete. A
// singleton gets the two it has routes behind, because rest.Singleton mounts one
// read and one write on the resource's own path with no id in it — mounting the
// rest is what produced a settings page that listed one row, offered to create a
// second one, and linked an id of all zeros. See mountSingleton.
//
// Both declarations come off the resource: a screen must preserve the API's
// operator boundary for private reads as well as writes. A customer's wildcard
// must not open a screen whose corresponding API route refused it.
// See docs/adr/0008.
func Mount(router *httpx.Router, s page.Shell, o Options, res httpx.Resource) {
	// at is where the screens are: /app/<module>/<entity>, composed by the kernel
	// when the resource was registered. Every link is built from it. rel is the
	// same address before the prefix, which is the only form a router accepts —
	// the module wrote it, and it is the reason this shell never has to know
	// where the workspace is mounted.
	at, rel := res.Screen, res.Path
	id := "screen-" + res.Module + "-" + res.Entity + "-"
	read, write := res.ReadAuth(), res.WriteAuth()
	if res.Singleton {
		mountSingleton(router, s, o, res, at, rel, id, read, write)
		return
	}

	page.Serve(router, s, page.Route{ID: id + "list", Method: http.MethodGet, Path: rel, Summary: "The " + res.Entity + " list"}, read,
		func(ctx context.Context, req page.Request, in *listInput) (page.View, error) {
			pageNo := max(in.Page, 1)
			rows, total, err := res.List(ctx, crud.Query{Limit: perPage, Offset: (pageNo - 1) * perPage, Sort: in.Sort})
			if err != nil {
				return page.View{}, err
			}
			return listView(res, ctx, localized(o, req), at, rows, total, pageNo, in.Sort, res.Writable(ctx)), nil
		})

	page.Serve(router, s, page.Route{ID: id + "new", Method: http.MethodGet, Path: rel + "/new", Summary: "The new-" + res.Entity + " form"}, write,
		func(_ context.Context, req page.Request, _ *page.Empty) (page.View, error) {
			o := localized(o, req)
			return Form(res, o, at, o.Text("screens.new", "New %s", res.Entity), nil, nil, "", true), nil
		})

	page.Serve(router, s, page.Route{ID: id + "create", Method: http.MethodPost, Path: rel, Summary: "Create a " + res.Entity}, write,
		func(ctx context.Context, req page.Request, in *formInput) (page.View, error) {
			// Immutable is refused here rather than dropped: this form does not
			// render those fields at all, so a value for one did not come from
			// it. See rest.Values.
			sent, err := rest.Values(in.RawBody, res.Schema.Fields, res.Immutable)
			if err == nil {
				var row map[string]any
				if row, err = res.Create(ctx, sent); err == nil {
					return page.View{}, httpx.SeeOther(at + "/" + rest.Text(row["id"]))
				}
			}
			errs, detail := rest.FieldErrors(err, res.Schema.Fields)
			o := localized(o, req)
			return Form(res, o, at, o.Text("screens.new", "New %s", res.Entity), sent, errs, detail, true), nil
		})

	page.Serve(router, s, page.Route{ID: id + "read", Method: http.MethodGet, Path: rel + "/{id}", Summary: "One " + res.Entity}, read,
		func(ctx context.Context, req page.Request, in *itemInput) (page.View, error) {
			row, err := res.Get(ctx, in.ID)
			if err != nil {
				return page.View{}, err
			}
			return detailView(res, ctx, localized(o, req), at, row, res.Writable(ctx)), nil
		})

	page.Serve(router, s, page.Route{ID: id + "edit", Method: http.MethodGet, Path: rel + "/{id}/edit", Summary: "The edit-" + res.Entity + " form"}, write,
		func(ctx context.Context, req page.Request, in *itemInput) (page.View, error) {
			row, err := res.Get(ctx, in.ID)
			if err != nil {
				return page.View{}, err
			}
			o := localized(o, req)
			return Form(res, o, at+"/"+in.ID.String(), o.Text("screens.edit_item", "Edit %s", res.Entity), row, nil, "", false), nil
		})

	page.Serve(router, s, page.Route{ID: id + "update", Method: http.MethodPost, Path: rel + "/{id}", Summary: "Update a " + res.Entity}, write,
		func(ctx context.Context, req page.Request, in *itemFormInput) (page.View, error) {
			item := at + "/" + in.ID.String()
			sent, err := rest.UpdateValues(in.RawBody, res.Schema.Fields, nil)
			if err == nil {
				if _, err = res.Update(ctx, in.ID, rest.Writable(sent, res.Immutable)); err == nil {
					return page.View{}, httpx.SeeOther(item)
				}
			}
			errs, detail := rest.FieldErrors(err, res.Schema.Fields)
			o := localized(o, req)
			return Form(res, o, item, o.Text("screens.edit_item", "Edit %s", res.Entity), sent, errs, detail, false), nil
		})

	page.Serve(router, s, page.Route{ID: id + "delete", Method: http.MethodPost, Path: rel + "/{id}/delete", Summary: "Delete a " + res.Entity}, write,
		func(ctx context.Context, _ page.Request, in *itemInput) (page.View, error) {
			if err := res.Delete(ctx, in.ID); err != nil {
				return page.View{}, err
			}
			return page.View{}, httpx.SeeOther(at)
		})

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
				if err := perform(c, ctx, uuid.Nil, body); err != nil {
					return page.View{}, err
				}
				return page.View{}, httpx.SeeOther(at)
			}
			if len(c.Fields) == 0 {
				page.Serve(router, s, page.Route{ID: id + c.Verb, Method: http.MethodPost,
					Path: strings.TrimSuffix(rel, "/") + "/" + c.Verb, Summary: c.Summary}, c.Auth,
					func(ctx context.Context, _ page.Request, _ *page.Empty) (page.View, error) {
						return collection(ctx, nil)
					})
			} else {
				page.Serve(router, s, page.Route{ID: id + c.Verb, Method: http.MethodPost,
					Path: strings.TrimSuffix(rel, "/") + "/" + c.Verb, Summary: c.Summary}, c.Auth,
					func(ctx context.Context, _ page.Request, in *formInput) (page.View, error) {
						return collection(ctx, in.RawBody)
					})
			}
			continue
		}
		item := func(ctx context.Context, rowID uuid.UUID, body []byte) (page.View, error) {
			if err := perform(c, ctx, rowID, body); err != nil {
				return page.View{}, err
			}
			return page.View{}, httpx.SeeOther(at + "/" + rowID.String())
		}
		if len(c.Fields) == 0 {
			page.Serve(router, s, page.Route{ID: id + c.Verb, Method: http.MethodPost,
				Path: rel + "/{id}/" + c.Verb, Summary: c.Summary}, c.Auth,
				func(ctx context.Context, _ page.Request, in *itemInput) (page.View, error) {
					return item(ctx, in.ID, nil)
				})
			continue
		}
		page.Serve(router, s, page.Route{ID: id + c.Verb, Method: http.MethodPost,
			Path: rel + "/{id}/" + c.Verb, Summary: c.Summary}, c.Auth,
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
	values, err := rest.Values(body, c.Fields, nil)
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
			return detailView(res, ctx, localized(o, req), at, row, res.Writable(ctx)), nil
		})

	page.Serve(router, s, page.Route{ID: id + "edit", Method: http.MethodGet, Path: rel + "/edit", Summary: "The edit-" + res.Entity + " form"}, write,
		func(ctx context.Context, req page.Request, _ *page.Empty) (page.View, error) {
			row, err := res.Get(ctx, uuid.Nil)
			if err != nil {
				return page.View{}, err
			}
			o := localized(o, req)
			return Form(res, o, at, o.Text("screens.edit_item", "Edit %s", res.Entity), row, nil, "", false), nil
		})

	page.Serve(router, s, page.Route{ID: id + "update", Method: http.MethodPost, Path: rel, Summary: "Update the " + res.Entity}, write,
		func(ctx context.Context, req page.Request, in *formInput) (page.View, error) {
			sent, err := rest.UpdateValues(in.RawBody, res.Schema.Fields, nil)
			if err == nil {
				if _, err = res.Update(ctx, uuid.Nil, rest.Writable(sent, res.Immutable)); err == nil {
					return page.View{}, httpx.SeeOther(at)
				}
			}
			errs, detail := rest.FieldErrors(err, res.Schema.Fields)
			o := localized(o, req)
			return Form(res, o, at, o.Text("screens.edit_item", "Edit %s", res.Entity), sent, errs, detail, false), nil
		})
}

// localized is the shell's options with this request's language. Options is a
// small value, so each render carries its own copy rather than the shell
// holding one language for everybody.
func localized(o Options, req page.Request) Options {
	o.Locale = req.Locale
	return o
}
