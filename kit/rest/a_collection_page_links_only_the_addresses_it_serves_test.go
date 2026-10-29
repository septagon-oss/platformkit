package rest_test

// A collection page links its rows into a record wherever a record route exists, and
// a resource that withholds read mounts none. The refusal of that declaration used
// to live at the mount site; now that the page is legal, the promise is the page's
// own: every address under the list's own path that a served collection page hands a
// reader is an address something answers. One row link into a 404 is a screen that
// does not work, and the renderer, not the router, is the one that said so.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestEveryAddressUnderItsOwnPathOnACollectionPageIsServed(t *testing.T) {
	for _, offered := range []struct {
		name   string
		offers []httpx.CRUD
	}{
		{"a list with no record page", []httpx.CRUD{rest.List}},
		{"a list with its record page", []httpx.CRUD{rest.List, rest.Read}},
		{"a list that also mounts its create form", []httpx.CRUD{rest.List, rest.Read, rest.Create}},
	} {
		offers := offered.offers
		t.Run(offered.name, func(t *testing.T) {
			rowID := uuid.New()
			listed := spec
			listed.Operations = offers
			api, router, admin := mountAs(t, listed, member{"task:read": true, "task:write": true})
			if _, err := admin.ExecContext(t.Context(),
				"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", rowID, acme.ID, "a row on a list"); err != nil {
				t.Fatal(err)
			}
			res := api.Resources()[0]
			screens.Mount(api.Surfaces(res.Module).App, listShell(), screens.Options{Workspace: "/app"}, res)
			code, body := call(t, router, http.MethodGet, res.Screen, "")
			if code != http.StatusOK {
				t.Fatalf("GET %s = %d %s, want the collection page", res.Screen, code, body)
			}
			asked := 0
			for _, at := range strings.Split(body, `href="`) {
				target := strings.SplitN(at, `"`, 2)[0]
				if !strings.HasPrefix(target, res.Screen+"/") {
					continue
				}
				asked++
				if got, page := call(t, router, http.MethodGet, target, ""); got == http.StatusNotFound {
					t.Errorf("the collection page of a resource offering %v links %s, which answers 404", offers, target)
					t.Logf("the page: %s", page)
				}
			}
			// The case is only worth anything if it looked at a link: a page that
			// links nothing off its own path would pass by saying nothing.
			if len(offers) > 1 && asked == 0 {
				t.Errorf("a resource offering %v links no address under its own path %s", offers, res.Screen)
			}
		})
	}
}
