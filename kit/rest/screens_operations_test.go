package rest_test

// screens_operations_test.go is the other half of a withheld verb. The routes
// are the truth about what a resource offers; the generated pages have to agree
// with it, because a page is what a person stands in front of, and a door behind
// which the router answers 405 is a refusal with a button on it.
//
// The caller in every case here holds the write permission. That is the point:
// the only reason a door is missing below is that the resource never mounted the
// route, and not anything about who is asking.

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// mountScreened mounts a Spec's generated screens over the Spec's own routes,
// for a caller who holds both permissions the Spec names.
func mountScreened(t *testing.T, s rest.Spec[*Task]) (http.Handler, *httpx.API, *sql.DB) {
	t.Helper()
	api, router, admin := mountAs(t, s, member{"task:read": true, "task:write": true})
	shell := page.Shell{Tag: "admin", Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node {
		return g.Group(body)
	}}
	for _, r := range api.Resources() {
		screens.Mount(api.Surfaces(r.Module).App, shell, screens.Options{Workspace: "/app"}, r)
	}
	return router, api, admin
}

// seed is a row the record page can be asked about, written through the owner
// connection so the case does not depend on a create the Spec may not mount.
func seed(t *testing.T, admin *sql.DB, title string) string {
	t.Helper()
	id := uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", id, acme.ID, title); err != nil {
		t.Fatal(err)
	}
	return id.String()
}

// TestTheScreensOfASpecOfferingListAndReadOfferNoWrite is the brief's first
// sentence beneath HTTP: the list has no New button, the record has no Edit link
// and no delete form, and the two pages the resource never mounted are addresses
// nothing answers at.
func TestTheScreensOfASpecOfferingListAndReadOfferNoWrite(t *testing.T) {
	only := spec
	only.Operations = []httpx.CRUD{rest.List, rest.Read}
	router, _, admin := mountScreened(t, only)
	row := seed(t, admin, "a row nobody may edit")

	code, body := call(t, router, http.MethodGet, "/app/tasks/task", "")
	if code != http.StatusOK {
		t.Fatalf("the list page = %d %s", code, body)
	}
	if strings.Contains(body, "/new") || strings.Contains(body, "New task") {
		t.Errorf("the list of a create-less resource offers New anyway: %s", around(body, "/new"))
	}

	code, body = call(t, router, http.MethodGet, "/app/tasks/task/"+row, "")
	if code != http.StatusOK {
		t.Fatalf("the record page = %d %s", code, body)
	}
	if strings.Contains(body, "/edit") || strings.Contains(body, "Edit") {
		t.Errorf("the record of an update-less resource offers Edit anyway: %s", around(body, "/edit"))
	}
	if strings.Contains(body, "/delete") {
		t.Errorf("the record of a delete-less resource carries a delete form: %s", around(body, "/delete"))
	}

	// And the pages are not mounted either: a link left over from another
	// resource's shell would lead a person to a refusal.
	for _, asked := range []struct{ method, path string }{
		{http.MethodGet, "/app/tasks/task/new"},
		{http.MethodGet, "/app/tasks/task/" + row + "/edit"},
		{http.MethodPost, "/app/tasks/task/" + row + "/delete"},
	} {
		if code, body := call(t, router, asked.method, asked.path, ""); code == http.StatusOK {
			t.Errorf("%s %s = 200, want the address of a page this resource never mounted: %s",
				asked.method, asked.path, around(body, "/app"))
		}
	}
}

// TestADeleteLessResourceStillOffersEditAndNew keeps the gating per verb: one
// boolean for three doors would answer the brief by hiding write screens no one
// asked to hide.
func TestADeleteLessResourceStillOffersEditAndNew(t *testing.T) {
	noDelete := spec
	noDelete.Operations = []httpx.CRUD{rest.List, rest.Read, rest.Create, rest.Update}
	router, _, admin := mountScreened(t, noDelete)
	row := seed(t, admin, "a row nobody will delete")

	_, list := call(t, router, http.MethodGet, "/app/tasks/task", "")
	if !strings.Contains(list, "/app/tasks/task/new") {
		t.Errorf("a resource that offers create draws no New button: %s", around(list, "/new"))
	}
	_, record := call(t, router, http.MethodGet, "/app/tasks/task/"+row, "")
	if !strings.Contains(record, "/edit") {
		t.Errorf("a resource that offers update draws no Edit link: %s", around(record, "/edit"))
	}
	if strings.Contains(record, "/delete") {
		t.Errorf("a resource that withholds delete still draws its delete form: %s", around(record, "/delete"))
	}
}

// TestAScreenlessResourcePublishesNoScreenAddress is the catalogue's half of the
// same rule: a resource with no collection page has no workspace address, and an
// entry that named one would send a shell to a 404 it was told to consider a
// screen.
func TestAScreenlessResourcePublishesNoScreenAddress(t *testing.T) {
	readOnly := spec
	readOnly.Operations = []httpx.CRUD{rest.Read}
	router, api, _ := mountScreened(t, readOnly)

	res := api.Resources()[0]
	if res.Screen != "" {
		t.Errorf("a resource with no list route claims the screen address %q", res.Screen)
	}
	if code, _ := call(t, router, http.MethodGet, "/app/tasks/task", ""); code != http.StatusNotFound {
		t.Errorf("GET the screen address of a screenless resource = %d, want 404", code)
	}
}
