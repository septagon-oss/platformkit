package internal

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// catalogPath is where a program asks what this caller may reach.
const catalogPath = "/api/v1/admin/resources"

// catalogOutput is the resources document.
type catalogOutput struct {
	Body screens.Catalog
}

// mountCatalog is the shell for a program: the resources this caller may reach,
// each with the schema its screens are generated from and whether the caller
// may write it. A native shell reads it and generates the same list, detail
// and form the pages here generate, by the same rules — one seam, one set of
// permissions, no second answer to "what is in this application".
func mountCatalog(api *httpx.API, resources []httpx.Resource) {
	httpx.Register(api, huma.Operation{
		OperationID: "admin-resources", Method: http.MethodGet, Path: catalogPath,
		Summary: "The resources this caller may reach, with their schemas", Tags: []string{"admin"},
	}, httpx.SignedIn(), func(ctx context.Context, _ *page.Empty) (*catalogOutput, error) {
		return &catalogOutput{Body: screens.Describe(ctx, resources)}, nil
	})
}
