package user_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user"
)

const (
	host = "acme.test"
	at   = "/api/v1/user/users"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// everything is the tenant loader and the authorizer for this file: one host,
// and a caller who holds every permission. What is under test is what the
// module refuses somebody who is allowed to do everything else.
type everything struct{}

func (everything) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return acme, nil
}

func (everything) Allowed(context.Context, tenancy.Tenant, string) (bool, error) { return true, nil }

func mount(t *testing.T) chi.Router {
	t.Helper()
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: everything{}, Conn: conn, Authorize: everything{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (httpx.Principal, bool, error) {
			return httpx.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	_, m := user.Module(user.Deps{})
	m.Routes(api)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return router
}

func call(t *testing.T, r http.Handler, method, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test") // see kit/httpx.credentialed
	// A cross-site write is refused by the kernel when a session cookie is
	// present; these requests carry a bearer credential instead.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	out, _ := io.ReadAll(w.Body)
	return w.Code, string(out)
}

// TestALifecycleChangeHasExactlyOneDoor.
//
// A grant and a deactivation are the two changes to a user that an audit has to
// be able to find, so each has one route and each publishes. The generic doors
// are shut, and they are shut for three different reasons worth knowing apart:
// the create is refused by the module's own AfterCreate hook, `status` by
// Spec.Immutable, and `roles` by kit/crud's schema — which covers a closed set
// of field types that a slice is not in, so the field does not exist as far as
// a patch is concerned. The last of those is the right answer arrived at for
// the wrong reason, and it is the one to revisit when the schema learns lists.
func TestALifecycleChangeHasExactlyOneDoor(t *testing.T) {
	router := mount(t)

	code, body := call(t, router, http.MethodPost, at, `{"email":"ada@acme.test","roles":["admin"]}`)
	if code != http.StatusUnprocessableEntity {
		t.Errorf("creating a user with roles = %d %s, want 422", code, body)
	}
	if !strings.Contains(body, "/roles") {
		t.Errorf("the refusal does not say where roles are granted: %s", body)
	}

	code, body = call(t, router, http.MethodPost, at, `{"email":"ada@acme.test","displayName":"Ada"}`)
	if code != http.StatusCreated {
		t.Fatalf("creating a user = %d %s, want 201", code, body)
	}
	id := field(t, body, "id")

	code, body = call(t, router, http.MethodPatch, at+"/"+id, `{"roles":["admin"]}`)
	if code != http.StatusUnprocessableEntity {
		t.Errorf("patching roles = %d %s, want 422", code, body)
	}

	// status is refused by name, because Deactivate owns it and publishes
	// user.deactivated: a caller who could patch it would deactivate somebody
	// and tell nobody.
	code, body = call(t, router, http.MethodPatch, at+"/"+id, `{"status":"inactive"}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "command of its own") {
		t.Errorf("patching status = %d %s, want 422 naming the door", code, body)
	}

	code, body = call(t, router, http.MethodPost, at+"/"+id+"/roles", `{"roles":["admin"]}`)
	if code != http.StatusOK || !strings.Contains(body, `"admin"`) {
		t.Errorf("granting a role = %d %s, want 200 and the role", code, body)
	}
}

// TestThePasswordIsInNoResponseAndNoRequest: password_hash is json:"-", so it
// is in no body, in no schema and in no generated screen.
func TestThePasswordIsInNoResponseAndNoRequest(t *testing.T) {
	router := mount(t)
	code, body := call(t, router, http.MethodPost, at, `{"email":"ada@acme.test"}`)
	if code != http.StatusCreated {
		t.Fatalf("creating a user = %d %s", code, body)
	}
	id := field(t, body, "id")
	if strings.Contains(strings.ToLower(body), "password") {
		t.Errorf("the create response mentions a password: %s", body)
	}

	code, body = call(t, router, http.MethodPost, at+"/"+id+"/set-password", `{"password":"correct horse battery staple"}`)
	if code != http.StatusOK {
		t.Fatalf("setting a password = %d %s, want 200", code, body)
	}
	if strings.Contains(strings.ToLower(body), "password") || strings.Contains(body, "argon2") {
		t.Errorf("the response carries the hash: %s", body)
	}
	if !strings.Contains(body, `"active"`) {
		t.Errorf("setting a password left the user %s, want active", body)
	}

	// And a patch cannot reach it either: the field has no json name, so the
	// schema does not know it.
	if code, body = call(t, router, http.MethodPatch, at+"/"+id, `{"passwordHash":"x"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("patching the hash = %d %s, want 422", code, body)
	}
}

func field(t *testing.T, body, name string) string {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("read %s from %s: %v", name, body, err)
	}
	s, _ := out[name].(string)
	if s == "" {
		t.Fatalf("no %s in %s", name, body)
	}
	return s
}
