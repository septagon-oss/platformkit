package rest_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Settings is a singleton entity: one row per tenant, with the same crud.Base
// every other entity has. It reuses the tasks table, because what is under test
// is the routes and not the storage — a singleton is a Load and a Save the
// module supplies, and this file supplies the simplest pair there is.
type Settings = Task

// settings is the singleton under test: a read, a PUT, and a public face that
// is smaller than the row.
func singleton(save bool, public bool) rest.Singleton[*Settings] {
	s := rest.Singleton[*Settings]{
		Module: "site", Entity: "settings", Path: "/api/v1/site/settings",
		Read:  "task:read",
		Event: "site.settings_updated",
		Load: func(_ context.Context, tx db.Tx[db.Tenant]) (*Settings, error) {
			var out Settings
			if err := tx.DB().Where("deleted_at IS NULL").Take(&out).Error; err == nil {
				return &out, nil
			}
			// A tenant that has configured nothing still has settings, which is
			// the answer this kind of singleton gives and billing's gives the
			// other way round.
			return &Settings{Title: "unnamed"}, nil
		},
	}
	if save {
		s.Write = "task:write"
		s.Save = func(ctx context.Context, tx db.Tx[db.Tenant], in *Settings) (*Settings, error) {
			current, err := s.Load(ctx, tx)
			if err != nil {
				return nil, err
			}
			if current.ID == uuid.Nil {
				in.ID = uuid.Nil
				return in, crud.Create(ctx, tx, in)
			}
			current.Title = in.Title
			return current, crud.Update(ctx, tx, current, "title", "updated_at")
		}
	}
	if public {
		s.Public = true
		s.Face = func(in *Settings) any { return map[string]string{"title": in.Title} }
	}
	return s
}

func mountSingleton(t *testing.T, s rest.Singleton[*Settings]) (*httpx.API, chi.Router) {
	t.Helper()
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: app, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	s.Mount(api)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return api, router
}

// TestASingletonReadsWritesAndShowsAFace is the whole shape: the read a tenant
// with nothing configured gets, the PUT, the public door, and the routes that
// are not mounted because a singleton has no id.
func TestASingletonReadsWritesAndShowsAFace(t *testing.T) {
	api, router := mountSingleton(t, singleton(true, true))
	const at = "/api/v1/site/settings"

	code, body := call(t, router, http.MethodGet, at, "")
	if code != http.StatusOK || !strings.Contains(body, `"title":"unnamed"`) {
		t.Fatalf("the read before anything = %d %s", code, body)
	}
	if code, body = call(t, router, http.MethodPut, at, `{"title":"Acme"}`); code != http.StatusOK {
		t.Fatalf("PUT = %d %s", code, body)
	}
	if code, body = call(t, router, http.MethodGet, at, ""); !strings.Contains(body, `"title":"Acme"`) {
		t.Errorf("the read after the PUT = %d %s", code, body)
	}

	// The public door serves the face and not the row.
	req := httptest.NewRequest(http.MethodGet, "http://"+host+at+"/public", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	switch {
	case w.Code != http.StatusOK:
		t.Errorf("the public door = %d %s", w.Code, w.Body.String())
	case !strings.Contains(w.Body.String(), `"title":"Acme"`):
		t.Errorf("the public face is %s", w.Body.String())
	case strings.Contains(w.Body.String(), "priority") || strings.Contains(w.Body.String(), "createdAt"):
		t.Errorf("the public door served the whole row: %s", w.Body.String())
	}

	// There is no item path, no create and no delete: a tenant has one, and it
	// is neither made nor removed.
	for _, gone := range []struct{ method, path string }{
		{http.MethodPost, at},
		{http.MethodDelete, at},
		{http.MethodGet, at + "/" + principal.String()},
	} {
		if code, _ := call(t, router, gone.method, gone.path, "{}"); code != http.StatusMethodNotAllowed && code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want no such route", gone.method, gone.path, code)
		}
	}

	// And the resource the admin generator reads, so a settings form is
	// derived rather than written.
	var found bool
	for _, r := range api.Resources() {
		if r.Module == "site" && r.Entity == "settings" {
			found = true
			if r.Write != "task:write" || r.Path != at {
				t.Errorf("the resource is %+v", r)
			}
		}
	}
	if !found {
		t.Error("no resource was registered, so there is no generated screen")
	}
}

// TestAReadOnlySingletonHasNoWriteAndNoScreen. billing's subscription is one: it
// is moved by its own commands, and a PUT would be a customer writing its own
// period.
func TestAReadOnlySingletonHasNoWriteAndNoScreen(t *testing.T) {
	api, router := mountSingleton(t, singleton(false, false))
	const at = "/api/v1/site/settings"

	if code, body := call(t, router, http.MethodGet, at, ""); code != http.StatusOK {
		t.Fatalf("the read = %d %s", code, body)
	}
	if code, _ := call(t, router, http.MethodPut, at, `{"title":"Acme"}`); code != http.StatusMethodNotAllowed && code != http.StatusNotFound {
		t.Errorf("a PUT on a read-only singleton = %d, want no such route", code)
	}
	if code, _ := call(t, router, http.MethodGet, at+"/public", ""); code != http.StatusMethodNotAllowed && code != http.StatusNotFound {
		t.Errorf("a public door nobody asked for = %d, want no such route", code)
	}
	for _, r := range api.Resources() {
		if r.Module == "site" {
			t.Error("a singleton with no write registered a resource, so the generator would mount forms nobody can use")
		}
	}
}

// TestASingletonRefusesAWiringMistake: the four states that could only produce
// broken routes fail where they are written.
func TestASingletonRefusesAWiringMistake(t *testing.T) {
	for what, s := range map[string]rest.Singleton[*Settings]{
		"no Load":          {Module: "site", Entity: "settings", Path: "/api/v1/site/settings", Read: "task:read"},
		"a path with no /": {Module: "site", Entity: "settings", Path: "api/v1", Read: "task:read", Load: singleton(false, false).Load},
		"public with no Face": {Module: "site", Entity: "settings", Path: "/api/v1/site/settings", Read: "task:read",
			Load: singleton(false, false).Load, Public: true},
		"a write with no Save": {Module: "site", Entity: "settings", Path: "/api/v1/site/settings", Read: "task:read",
			Load: singleton(false, false).Load, Write: "task:write"},
	} {
		t.Run(what, func(t *testing.T) {
			defer func() {
				// The message has to be the check's own: mounting on a nil API
				// panics whatever the Singleton says, so a test that only
				// asserted "it panicked" would pass with no check at all.
				got, ok := recover().(string)
				if !ok || !strings.HasPrefix(got, "rest: Singleton for ") {
					t.Errorf("%s mounted with %v; want the wiring refusal", what, got)
				}
			}()
			s.Mount(nil)
		})
	}
}
