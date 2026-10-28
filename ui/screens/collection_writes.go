package screens

import (
	"context"
	"net/http"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/page"
)

func mountCollectionNew(router *httpx.Router, s page.Shell, o Options, res httpx.Resource, at, rel, id string, write httpx.Auth) {
	if writeAvailable(res, httpx.CRUDCreate) {
		page.Serve(router, s, page.Route{View: screenRef(res, "create", ""), ID: id + "new", Method: http.MethodGet, Path: rel + "/new", Summary: "The new-" + res.Entity + " form"}, write,
			func(_ context.Context, req page.Request, _ *page.Empty) (page.View, error) {
				o := localized(o, req)
				return Form(res, o, at, o.Text("screens.new", "New %s", entryName(res, o)), nil, nil, "", true), nil
			})
	}
}

func mountCollectionCreate(router *httpx.Router, s page.Shell, o Options, res httpx.Resource, at, rel, id string, write httpx.Auth) {
	if writeAvailable(res, httpx.CRUDCreate) {
		page.Serve(router, s, page.Route{View: screenRef(res, "create", ""), ID: id + "create", Method: http.MethodPost, Path: rel, Summary: "Create a " + res.Entity}, write,
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
				return refusedForm(res, o, req, at, "screens.new", "New %s", true, sent, err), nil
			})
	}
}

func mountCollectionEdit(router *httpx.Router, s page.Shell, o Options, res httpx.Resource, at, rel, id string, write httpx.Auth) {
	if writeAvailable(res, httpx.CRUDUpdate) {
		page.Serve(router, s, page.Route{View: screenRef(res, "edit", ""), ID: id + "edit", Method: http.MethodGet, Path: rel + "/{id}/edit", Summary: "The edit-" + res.Entity + " form",
			Subject: subject(res, o, at, true)}, write,
			func(ctx context.Context, req page.Request, in *itemInput) (page.View, error) {
				row, err := res.Get(ctx, in.ID)
				if err != nil {
					return page.View{}, err
				}
				o := localized(o, req)
				return Form(res, o, at+"/"+in.ID.String(), o.Text("screens.edit_item", "Edit %s", entryName(res, o)), row, nil, "", false), nil
			})
	}
}

func mountCollectionUpdate(router *httpx.Router, s page.Shell, o Options, res httpx.Resource, at, rel, id string, write httpx.Auth) {
	if writeAvailable(res, httpx.CRUDUpdate) {
		page.Serve(router, s, page.Route{View: screenRef(res, "edit", ""), ID: id + "update", Method: http.MethodPost, Path: rel + "/{id}", Summary: "Update a " + res.Entity}, write,
			func(ctx context.Context, req page.Request, in *itemFormInput) (page.View, error) {
				item := at + "/" + in.ID.String()
				sent, err := rest.UpdateValues(in.RawBody, res.Schema.Fields, nil)
				if err == nil {
					if _, err = res.Update(ctx, in.ID, rest.Writable(sent, res.Immutable)); err == nil {
						return page.View{}, httpx.SeeOther(item)
					}
				}
				return refusedForm(res, o, req, item, "screens.edit_item", "Edit %s", false, sent, err), nil
			})
	}
}

func mountCollectionDelete(router *httpx.Router, s page.Shell, o Options, res httpx.Resource, at, rel, id string, write httpx.Auth) {
	if writeAvailable(res, httpx.CRUDDelete) {
		page.Serve(router, s, page.Route{Exception: "redirect", Reason: "Successful deletion returns to the collection.", ID: id + "delete", Method: http.MethodPost, Path: rel + "/{id}/delete", Summary: "Delete a " + res.Entity}, write,
			func(ctx context.Context, _ page.Request, in *itemInput) (page.View, error) {
				if err := res.Delete(ctx, in.ID); err != nil {
					return page.View{}, err
				}
				return page.View{}, httpx.SeeOther(at)
			})
	}
}
