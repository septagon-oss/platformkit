package rest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// screens.go mounts a shell's screens over a mounted resource, which is what an
// admin application does. The singleton under test is the one this repository
// really ships — site's settings — reproduced from a running app rather than
// imagined:
//
//	GET  /app/site/settings                    200   a list, sortable on every field
//	       href="/app/site/settings/new"              a door no route serves
//	       href="/app/site/settings/0000…0000"        a row id invented from nothing
//	       a Delete form
//	GET  /api/v1/site/settings/0000…0000         404   the API never heard of that id
//	POST /api/v1/site/settings                   405   the API will not create one
//
// kit/rest/singleton.go answers that by making Create and Delete *refuse* rather
// than be nil, because the generator calls all five — which is the right call for
// a closure and the wrong one for a door. Refusing a click is not the same as not
// offering it, and httpx.Resource.Singleton exists precisely so a shell knows:
// "a shell that did not know would render a collection whose list is one row,
// offering doors — New, Delete — that no route serves." The native catalog reads
// that flag (ui/screens/catalog.go). The web screens did not.
func mountSingletonScreens(t *testing.T, s rest.Singleton[*Settings]) chi.Router {
	t.Helper()
	api, router := mountSingleton(t, s)
	shell := page.Shell{Tag: "admin", Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node {
		return g.Group(body)
	}}
	for _, r := range api.Resources() {
		screens.Mount(api.Surfaces(r.Module).App, shell, screens.Options{Workspace: "/app"}, r)
	}
	return router
}

// around is a failure message's worth of markup rather than a whole document:
// these pages are 12 KB of class attributes, and a test that prints all of them
// hides the one anchor that is wrong.
func around(body, needle string) string {
	i := strings.Index(body, needle)
	if i < 0 {
		return body[:min(len(body), 400)]
	}
	lo, hi := max(0, i-260), min(len(body), i+260)
	return body[lo:hi]
}

func postForm(t *testing.T, r http.Handler, path, body string) (int, string, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://"+host+path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String(), w.Header().Get("Location")
}

// TestTheScreensOfASingletonOfferOnlyWhatItsRoutesServe is the reproduction.
func TestTheScreensOfASingletonOfferOnlyWhatItsRoutesServe(t *testing.T) {
	router := mountSingletonScreens(t, singleton(true, false))
	const at = "/app/site/settings"

	code, body := call(t, router, http.MethodGet, at, "")
	if code != http.StatusOK {
		t.Fatalf("the record page = %d", code)
	}
	// A door is an href or a form action, not a bare string: a row's own id may
	// legitimately appear as one of its values, and that is the row's data rather
	// than the screen inventing an address. What the screen may not do is build a
	// path out of an id no route was ever mounted behind.
	for _, absent := range []string{
		`href="` + at + `/new`, // no create route exists
		`/delete`,              // no delete route exists
		`="` + at + `/00000000-0000-0000-0000-000000000000`, // no id in a singleton's path at all
		"?sort=", // one row does not need a sortable list
	} {
		if strings.Contains(body, absent) {
			t.Errorf("the record page offers %q, which no route serves: %s", absent, around(body, absent))
		}
	}
	if !strings.Contains(body, "unnamed") {
		t.Errorf("the record page does not show the row there is: %s", body)
	}
	if !strings.Contains(body, `href="`+at+`/edit"`) {
		t.Errorf("a caller who may write is not offered the one write there is: %s", around(body, "</h1>"))
	}

	// And the doors the collection used to mount are not mounted as routes
	// either, so a bookmark or a guess gets "no such route" rather than a form
	// that would refuse the write it offers.
	for _, gone := range []struct{ method, path string }{
		{http.MethodGet, at + "/new"},
		{http.MethodGet, at + "/00000000-0000-0000-0000-000000000000"},
		{http.MethodGet, at + "/00000000-0000-0000-0000-000000000000/edit"},
		{http.MethodPost, at + "/00000000-0000-0000-0000-000000000000/delete"},
	} {
		if code, _ := call(t, router, gone.method, gone.path, "{}"); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want no such route", gone.method, gone.path, code)
		}
	}
}

// TestASingletonEditsItselfWithNoIDAnywhere is the door that does exist, and the
// reason the id is left out: there is one row, and a screen that put an id in the
// path would be asking about a tenant the caller cannot see.
func TestASingletonEditsItselfWithNoIDAnywhere(t *testing.T) {
	router := mountSingletonScreens(t, singleton(true, false))
	const at = "/app/site/settings"

	code, body := call(t, router, http.MethodGet, at+"/edit", "")
	if code != http.StatusOK {
		t.Fatalf("the edit screen = %d", code)
	}
	if !strings.Contains(body, `action="`+at+`"`) {
		t.Errorf("the form does not post to the one path that serves a write: %s", body)
	}

	code, body, location := postForm(t, router, at, url.Values{"title": {"Acme"}}.Encode())
	if code != http.StatusSeeOther {
		t.Fatalf("saving settings = %d %s", code, body)
	}
	if location != at {
		t.Errorf("after a save the person is taken to %q, want the record page %q", location, at)
	}
	if _, body := call(t, router, http.MethodGet, at, ""); !strings.Contains(body, "Acme") {
		t.Errorf("the saved value is not on the record page: %s", body)
	}
}

// A singleton nobody may write registers no resource at all, so no screen is
// mounted — TestAReadOnlySingletonHasNoWriteAndNoScreen in singleton_test.go owns
// that case, and it is not repeated here. What that leaves unguarded is the
// writable singleton whose *caller* may not write: their page must not offer the
// edit link. ui/resource owns that decision as a plain boolean, and its unit test
// (ui/resource/singleton_test.go) holds it without a database.

// TestACollectionsScreensKeepEveryDoorTheyHad is the half that lets this change go
// near every other screen in the repository: a resource that really is a
// collection keeps its New, its item links, its Edit and its delete form. It
// asserts the doors are present rather than that nothing threw, because a screen
// that quietly lost its only write door would pass a test that only looked for
// errors.
func TestACollectionsScreensKeepEveryDoorTheyHad(t *testing.T) {
	api, router, admin := mounted(t)
	rowID := uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", rowID, acme.ID, "keep me"); err != nil {
		t.Fatal(err)
	}
	shell := page.Shell{Tag: "admin", Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node {
		return g.Group(body)
	}}
	screens.Mount(api.Surfaces("tasks").App, shell, screens.Options{Workspace: "/app"}, api.Resources()[0])
	const at = "/app/tasks/task"

	_, list := call(t, router, http.MethodGet, at, "")
	if !strings.Contains(list, at+"/new") {
		t.Errorf("a collection lost its New: %s", list)
	}
	if !strings.Contains(list, at+"/") {
		t.Errorf("a collection lost the way into a row: %s", list)
	}

	code, item := call(t, router, http.MethodGet, at+"/"+rowID.String(), "")
	if code != http.StatusOK {
		t.Fatalf("a row's page = %d", code)
	}
	if !strings.Contains(item, at+"/"+rowID.String()+"/edit") {
		t.Errorf("a row lost its Edit: %s", item)
	}
	if !strings.Contains(item, at+"/"+rowID.String()+"/delete") {
		t.Errorf("a row lost its delete form: %s", item)
	}
}
