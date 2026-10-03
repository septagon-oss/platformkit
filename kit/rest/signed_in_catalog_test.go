package rest_test

// The case the delivery never wrote:
// TestASignedInResourceIsInItsCallersCatalogueAtAll. The reason Readable moved
// onto the declaration is that Describe
// drops what it answers no to, so the claim that matters is about the document,
// not about the method. This asks the document, over a mounted Spec, a real
// Postgres and a caller who holds no grant at all.

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestASignedInResourceReachesItsCallersCatalogueWithItsVerbs(t *testing.T) {
	shared := spec
	shared.Operations = []httpx.CRUD{rest.List, rest.Read}
	shared.Read, shared.ReadAuth = "", httpx.SignedIn()
	api, router, _ := mountAs(t, shared, member{})
	var out screens.Catalog
	httpx.Register(api.Surfaces("tasks").App, probeOperation("probe", "/probe"), httpx.SignedIn(),
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			out = screens.Describe(ctx, api.Resources())
			return nil, nil
		})
	if code, body := call(t, router, http.MethodPost, api.Surfaces("tasks").App.Path("/probe"), ""); code != http.StatusNoContent {
		t.Fatalf("the probe = %d %s", code, body)
	}
	if len(out.Resources) != 1 {
		t.Fatalf("the catalogue of a member holding no grant is %+v, want the one resource httpx.SignedIn() admits them to", out.Resources)
	}
	e := out.Resources[0]
	if !slices.Equal(e.Operations, []string{"list", "read"}) {
		t.Errorf("the entry publishes operations %v, want the two verbs the Spec mounted", e.Operations)
	}
	if e.Writable {
		t.Errorf("the entry says writable=true to a caller who holds no grant at all")
	}
}
