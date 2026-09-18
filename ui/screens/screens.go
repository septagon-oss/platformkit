package screens

import (
	"context"
	"net/http"

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
// Everything about them comes from the resource — the path from its API path, the
// guards from its two permissions, the columns and the controls from its schema,
// and the doors it has from which operations it actually mounted.
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
func Mount(api *httpx.API, s page.Shell, o Options, r httpx.Resource) {
	at := Path(r, o)
	id := "screen-" + r.Module + "-" + r.Entity + "-"
	read, write := r.ReadAuth(), r.WriteAuth()
	if r.Singleton {
		mountSingleton(api, s, o, r, at, id, read, write)
		return
	}

	page.Serve(api, s, page.Route{ID: id + "list", Method: http.MethodGet, Path: at, Summary: "The " + r.Entity + " list"}, read,
		func(ctx context.Context, req page.Request, in *listInput) (page.View, error) {
			pageNo := max(in.Page, 1)
			rows, total, err := r.List(ctx, crud.Query{Limit: perPage, Offset: (pageNo - 1) * perPage, Sort: in.Sort})
			if err != nil {
				return page.View{}, err
			}
			return List(r, localized(o, req), rows, total, pageNo, in.Sort, r.Writable(ctx)), nil
		})

	page.Serve(api, s, page.Route{ID: id + "new", Method: http.MethodGet, Path: at + "/new", Summary: "The new-" + r.Entity + " form"}, write,
		func(_ context.Context, req page.Request, _ *page.Empty) (page.View, error) {
			o := localized(o, req)
			return Form(r, o, at, o.Text("screens.new", "New %s", r.Entity), nil, nil, "", true), nil
		})

	page.Serve(api, s, page.Route{ID: id + "create", Method: http.MethodPost, Path: at, Summary: "Create a " + r.Entity}, write,
		func(ctx context.Context, req page.Request, in *formInput) (page.View, error) {
			// Immutable is refused here rather than dropped: this form does not
			// render those fields at all, so a value for one did not come from
			// it. See rest.Values.
			sent, err := rest.Values(in.RawBody, r.Schema.Fields, r.Immutable)
			if err == nil {
				var row map[string]any
				if row, err = r.Create(ctx, sent); err == nil {
					return page.View{}, httpx.SeeOther(at + "/" + rest.Text(row["id"]))
				}
			}
			errs, detail := rest.FieldErrors(err, r.Schema.Fields)
			o := localized(o, req)
			return Form(r, o, at, o.Text("screens.new", "New %s", r.Entity), sent, errs, detail, true), nil
		})

	page.Serve(api, s, page.Route{ID: id + "read", Method: http.MethodGet, Path: at + "/{id}", Summary: "One " + r.Entity}, read,
		func(ctx context.Context, req page.Request, in *itemInput) (page.View, error) {
			row, err := r.Get(ctx, in.ID)
			if err != nil {
				return page.View{}, err
			}
			return Detail(r, localized(o, req), row, r.Writable(ctx)), nil
		})

	page.Serve(api, s, page.Route{ID: id + "edit", Method: http.MethodGet, Path: at + "/{id}/edit", Summary: "The edit-" + r.Entity + " form"}, write,
		func(ctx context.Context, req page.Request, in *itemInput) (page.View, error) {
			row, err := r.Get(ctx, in.ID)
			if err != nil {
				return page.View{}, err
			}
			o := localized(o, req)
			return Form(r, o, at+"/"+in.ID.String(), o.Text("screens.edit_item", "Edit %s", r.Entity), row, nil, "", false), nil
		})

	page.Serve(api, s, page.Route{ID: id + "update", Method: http.MethodPost, Path: at + "/{id}", Summary: "Update a " + r.Entity}, write,
		func(ctx context.Context, req page.Request, in *itemFormInput) (page.View, error) {
			item := at + "/" + in.ID.String()
			sent, err := rest.UpdateValues(in.RawBody, r.Schema.Fields, nil)
			if err == nil {
				if _, err = r.Update(ctx, in.ID, rest.Writable(sent, r.Immutable)); err == nil {
					return page.View{}, httpx.SeeOther(item)
				}
			}
			errs, detail := rest.FieldErrors(err, r.Schema.Fields)
			o := localized(o, req)
			return Form(r, o, item, o.Text("screens.edit_item", "Edit %s", r.Entity), sent, errs, detail, false), nil
		})

	page.Serve(api, s, page.Route{ID: id + "delete", Method: http.MethodPost, Path: at + "/{id}/delete", Summary: "Delete a " + r.Entity}, write,
		func(ctx context.Context, _ page.Request, in *itemInput) (page.View, error) {
			if err := r.Delete(ctx, in.ID); err != nil {
				return page.View{}, err
			}
			return page.View{}, httpx.SeeOther(at)
		})
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
func mountSingleton(api *httpx.API, s page.Shell, o Options, r httpx.Resource, at, id string, read, write httpx.Auth) {
	page.Serve(api, s, page.Route{ID: id + "read", Method: http.MethodGet, Path: at, Summary: "The " + r.Entity + " record"}, read,
		func(ctx context.Context, req page.Request, _ *page.Empty) (page.View, error) {
			row, err := r.Get(ctx, uuid.Nil)
			if err != nil {
				return page.View{}, err
			}
			return Detail(r, localized(o, req), row, r.Writable(ctx)), nil
		})

	page.Serve(api, s, page.Route{ID: id + "edit", Method: http.MethodGet, Path: at + "/edit", Summary: "The edit-" + r.Entity + " form"}, write,
		func(ctx context.Context, req page.Request, _ *page.Empty) (page.View, error) {
			row, err := r.Get(ctx, uuid.Nil)
			if err != nil {
				return page.View{}, err
			}
			o := localized(o, req)
			return Form(r, o, at, o.Text("screens.edit_item", "Edit %s", r.Entity), row, nil, "", false), nil
		})

	page.Serve(api, s, page.Route{ID: id + "update", Method: http.MethodPost, Path: at, Summary: "Update the " + r.Entity}, write,
		func(ctx context.Context, req page.Request, in *formInput) (page.View, error) {
			sent, err := rest.UpdateValues(in.RawBody, r.Schema.Fields, nil)
			if err == nil {
				if _, err = r.Update(ctx, uuid.Nil, rest.Writable(sent, r.Immutable)); err == nil {
					return page.View{}, httpx.SeeOther(at)
				}
			}
			errs, detail := rest.FieldErrors(err, r.Schema.Fields)
			o := localized(o, req)
			return Form(r, o, at, o.Text("screens.edit_item", "Edit %s", r.Entity), sent, errs, detail, false), nil
		})
}

// localized is the shell's options with this request's language. Options is a
// small value, so each render carries its own copy rather than the shell
// holding one language for everybody.
func localized(o Options, req page.Request) Options {
	o.Locale = req.Locale
	return o
}
