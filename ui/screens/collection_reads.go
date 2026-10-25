package screens

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/page"
)

func mountCollectionList(router *httpx.Router, s page.Shell, o Options, res httpx.Resource, at, rel, id string, read httpx.Auth) {
	if res.Offers(httpx.CRUDList) {
		page.Serve(router, s, page.Route{ID: id + "list", Method: http.MethodGet, Path: rel, Summary: "The " + res.Entity + " list",
			Subject: subject(res, o, at, false)}, read,
			func(ctx context.Context, req page.Request, in *listInput) (page.View, error) {
				pageNo := max(in.Page, 1)
				rows, total, err := res.List(ctx, crud.Query{Limit: perPage, Offset: (pageNo - 1) * perPage, Sort: in.Sort})
				if err != nil {
					return page.View{}, err
				}
				return listView(res, ctx, localized(o, req), at, rows, total, pageNo, in.Sort, res.Writable(ctx)), nil
			})
	}
}

func mountCollectionRead(router *httpx.Router, s page.Shell, o Options, res httpx.Resource, at, rel, id string, read httpx.Auth) {
	if res.Offers(httpx.CRUDRead) {
		page.Serve(router, s, page.Route{ID: id + "read", Method: http.MethodGet, Path: rel + "/{id}", Summary: "One " + res.Entity,
			Subject: subject(res, o, at, true)}, read,
			func(ctx context.Context, req page.Request, in *readInput) (page.View, error) {
				if in.ID == "new" {
					return page.View{}, huma.Error404NotFound("No form is available")
				}
				rowID, err := uuid.Parse(in.ID)
				if err != nil {
					return page.View{}, huma.Error422UnprocessableEntity("Invalid record ID")
				}
				row, err := res.Get(ctx, rowID)
				if err != nil {
					return page.View{}, err
				}
				return detailView(res, ctx, localized(o, req), at, row, res.Writable(ctx))
			})
	}
}
