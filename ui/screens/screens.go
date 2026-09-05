package screens

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/page"
)

// perPage is a screenful. The API's own default is the same number, so a page
// of a list and a page of the collection route are the same page.
const perPage = crud.DefaultLimit

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

// Mount is the seven operations of one resource, each a renderer behind
// page.Serve. Everything about them comes from the resource — the path from its
// API path, the guards from its two permissions, the columns and the controls
// from its schema.
//
// The write declaration comes off the resource rather than being rebuilt here:
// a resource whose rows are the operator's — the price list — is written at
// the operator's host and nowhere else, and a screen that declared the bare
// permission would be a form a customer's wildcard could use after the API
// refused it. See docs/adr/0008.
func Mount(api *httpx.API, s page.Shell, o Options, r httpx.Resource) {
	at := Path(r, o)
	id := "screen-" + r.Module + "-" + r.Entity + "-"
	read, write := httpx.Permission(r.Read), r.WriteAuth()

	page.Serve(api, s, page.Route{ID: id + "list", Method: http.MethodGet, Path: at, Summary: "The " + r.Entity + " list"}, read,
		func(ctx context.Context, _ page.Request, in *listInput) (page.View, error) {
			pageNo := max(in.Page, 1)
			rows, total, err := r.List(ctx, crud.Query{Limit: perPage, Offset: (pageNo - 1) * perPage, Sort: in.Sort})
			if err != nil {
				return page.View{}, err
			}
			return List(r, o, rows, total, pageNo, in.Sort, r.Writable(ctx)), nil
		})

	page.Serve(api, s, page.Route{ID: id + "new", Method: http.MethodGet, Path: at + "/new", Summary: "The new-" + r.Entity + " form"}, write,
		func(context.Context, page.Request, *page.Empty) (page.View, error) {
			return Form(r, o, at, "New "+r.Entity, nil, nil, "", true), nil
		})

	page.Serve(api, s, page.Route{ID: id + "create", Method: http.MethodPost, Path: at, Summary: "Create a " + r.Entity}, write,
		func(ctx context.Context, _ page.Request, in *formInput) (page.View, error) {
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
			return Form(r, o, at, "New "+r.Entity, sent, errs, detail, true), nil
		})

	page.Serve(api, s, page.Route{ID: id + "read", Method: http.MethodGet, Path: at + "/{id}", Summary: "One " + r.Entity}, read,
		func(ctx context.Context, _ page.Request, in *itemInput) (page.View, error) {
			row, err := r.Get(ctx, in.ID)
			if err != nil {
				return page.View{}, err
			}
			return Detail(r, o, row, r.Writable(ctx)), nil
		})

	page.Serve(api, s, page.Route{ID: id + "edit", Method: http.MethodGet, Path: at + "/{id}/edit", Summary: "The edit-" + r.Entity + " form"}, write,
		func(ctx context.Context, _ page.Request, in *itemInput) (page.View, error) {
			row, err := r.Get(ctx, in.ID)
			if err != nil {
				return page.View{}, err
			}
			return Form(r, o, at+"/"+in.ID.String(), "Edit "+r.Entity, row, nil, "", false), nil
		})

	page.Serve(api, s, page.Route{ID: id + "update", Method: http.MethodPost, Path: at + "/{id}", Summary: "Update a " + r.Entity}, write,
		func(ctx context.Context, _ page.Request, in *itemFormInput) (page.View, error) {
			item := at + "/" + in.ID.String()
			sent, err := rest.Values(in.RawBody, r.Schema.Fields, nil)
			if err == nil {
				if _, err = r.Update(ctx, in.ID, rest.Writable(sent, r.Immutable)); err == nil {
					return page.View{}, httpx.SeeOther(item)
				}
			}
			errs, detail := rest.FieldErrors(err, r.Schema.Fields)
			return Form(r, o, item, "Edit "+r.Entity, sent, errs, detail, false), nil
		})

	page.Serve(api, s, page.Route{ID: id + "delete", Method: http.MethodPost, Path: at + "/{id}/delete", Summary: "Delete a " + r.Entity}, write,
		func(ctx context.Context, _ page.Request, in *itemInput) (page.View, error) {
			if err := r.Delete(ctx, in.ID); err != nil {
				return page.View{}, err
			}
			return page.View{}, httpx.SeeOther(at)
		})
}
