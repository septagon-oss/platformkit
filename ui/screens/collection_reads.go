package screens

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/page"
)

func mountCollectionRead(router *httpx.Router, s page.Shell, o Options, res httpx.Resource, at, rel, id string, read httpx.Auth) {
	if res.Offers(httpx.CRUDRead) {
		page.Serve(router, s, page.Route{View: screenRef(res, "detail", ""), ID: id + "read", Method: http.MethodGet, Path: rel + "/{id}", Summary: "One " + res.Entity,
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
