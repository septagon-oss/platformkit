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
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

const (
	host = "acme.test"
	at   = "/api/v1/user/users"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// administering is this file's role system. The real answer is the auth
// module's table — auth.AdministeringRoles — and this schema is the user
// module's own migrations, which own users and nothing else; the floor takes a
// function for exactly that reason.
func administering(context.Context, db.Tx[db.Tenant]) ([]string, error) {
	return []string{"owner"}, nil
}

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

func (everything) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return true, nil
}

func mount(t *testing.T) chi.Router {
	t.Helper()
	_, conn := dbtest.Schema(t, user.Migrations)
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: everything{}, Conn: conn, Authorize: everything{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	_, m := user.Module(user.Deps{Administration: &usercontracts.AdministrationFunc{Ask: administering}})
	m.Routes(surfacesOf(api))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return router
}

func call(t *testing.T, r http.Handler, method, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// The session cookie is the one credential shape the kernel recognises, so
	// a test that wants its identity hook called presents one. The value is not
	// read: this file's hook answers without looking. See kit/httpx.credentialed.
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
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
// are shut, and they are shut for two different reasons worth knowing apart:
// the create and the two patches are all refused by Spec.Immutable, which names
// the field so the caller is told which door to use. The create used to be the
// module's own AfterCreate hook, which is now a second line of defence behind
// the kernel's: kit/rest refuses an Immutable field at every door that reads a
// body, for every module at once. See rest.foldedName. The folded block below
// is that rule's module-level conformance case, run at this module's generated
// routes through this file's own mount and call — the pattern every module
// route test in this repository uses, because the kernel exports no route-test
// helper — and it asks the kernel's rule, not the module's own hook.
//
// `roles` used to be refused by kit/crud's schema having no list type, which
// meant it rendered nowhere either. The schema has one now, so the refusal is
// deliberate and the field is real: the case below is the one that would have
// let a bulk update of a profile grant somebody the admin role.
func TestALifecycleChangeHasExactlyOneDoor(t *testing.T) {
	router := mount(t)

	for _, status := range []string{"pending", "unverified"} {
		code, _ := call(t, router, http.MethodPost, at, `{"email":"`+status+`@acme.test","status":"`+status+`"}`)
		if code != http.StatusUnprocessableEntity {
			t.Fatalf("generic create bypassed %s registration: %d", status, code)
		}
	}
	code, body := call(t, router, http.MethodPost, at, `{"email":"ada@acme.test","roles":["admin"]}`)
	if code != http.StatusUnprocessableEntity {
		t.Errorf("creating a user with roles = %d %s, want 422", code, body)
	}
	if !strings.Contains(body, "roles belongs to a route of its own") {
		t.Errorf("the refusal does not say where roles are granted: %s", body)
	}

	code, body = call(t, router, http.MethodPost, at, `{"email":"ada@acme.test","displayName":"Ada"}`)
	if code != http.StatusCreated {
		t.Fatalf("creating a user = %d %s, want 201", code, body)
	}
	id := field(t, body, "id")

	code, body = call(t, router, http.MethodPatch, at+"/"+id, `{"roles":["admin"]}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "route of its own") {
		t.Errorf("patching roles = %d %s, want 422 naming the door", code, body)
	}

	// And the patch route still works, which is what says the two refusals are
	// about those two fields and not about PATCH.
	code, body = call(t, router, http.MethodPatch, at+"/"+id, `{"displayName":"Ada Lovelace"}`)
	if code != http.StatusOK || !strings.Contains(body, "Ada Lovelace") {
		t.Errorf("patching displayName = %d %s, want 200", code, body)
	}

	// handle is refused by name for the same reason roles is: it publishes
	// user.handle_set and the trail has to say who held the name before. A handle
	// a caller could PATCH beside a display name is a handle that changed hands
	// while nobody was told.
	code, body = call(t, router, http.MethodPatch, at+"/"+id, `{"handle":"ada"}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "route of its own") {
		t.Errorf("patching handle = %d %s, want 422 naming the door", code, body)
	}

	// status is refused by name, because Deactivate owns it and publishes
	// user.deactivated: a caller who could patch it would deactivate somebody
	// and tell nobody.
	code, body = call(t, router, http.MethodPatch, at+"/"+id, `{"status":"inactive"}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "route of its own") {
		t.Errorf("patching status = %d %s, want 422 naming the door", code, body)
	}

	// The same rule under the decoder's own folding: "ROLES" binds into Roles
	// exactly as "roles" does, so the door in front of it has to name the
	// declared field, and a refused create stores no part of its body.
	for _, sent := range []struct{ body, field string }{
		{`{"email":"folded@acme.test","ROLES":["admin"]}`, "roles"},
		{`{"email":"folded2@acme.test","Handle":"ada"}`, "handle"},
	} {
		code, body := call(t, router, http.MethodPost, at, sent.body)
		if code != http.StatusUnprocessableEntity ||
			!strings.Contains(body, sent.field+" belongs to a route of its own") {
			t.Errorf("%s = %d %s, want 422 naming the declared field %s", sent.body, code, body, sent.field)
		}
	}
	if code, body := call(t, router, http.MethodGet, at, ""); code != http.StatusOK ||
		strings.Contains(body, "folded") {
		t.Errorf("a create the door refused left a row: %d %s", code, body)
	}
	// And the patch door names the field it refused, not the spelling it was
	// sent, so the caller is pointed at Deactivate rather than at a field that
	// does not exist.
	if code, body := call(t, router, http.MethodPatch, at+"/"+id, `{"STATUS":"inactive"}`); code != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "status belongs to a route of its own") {
		t.Errorf(`patching STATUS = %d %s, want 422 naming status`, code, body)
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

// TestTheLastAdministratorCannotBeRemovedThroughAnyDoor.
//
// Three writes on the generated user screen reached the same state, and all
// three answered 2xx: setting the sole administrator's roles to none,
// deactivating them, and deleting them. After any of them nobody in the tenant
// could change a role again — the roles screen, the roles route and the users
// route all answer 403 — and the installation's operator cannot repair a
// customer's tenant, because a session does not cross into one. In the
// operator's own tenant the same three clicks take the control plane with them.
//
// The refusal is 422 and names the person and the role, because it is a rule
// about the request and not an outage. The PATCH route is not in this list: its
// two fields are Immutable and TestALifecycleChangeHasExactlyOneDoor covers
// them, which is why the door count here is three and not five.
func TestTheLastAdministratorCannotBeRemovedThroughAnyDoor(t *testing.T) {
	router := mount(t)
	ada := administrator(t, router, "ada@acme.test")

	doors := []struct {
		name, method, path, body string
	}{
		{"roles", http.MethodPost, at + "/" + ada + "/roles", `{"roles":[]}`},
		{"deactivate", http.MethodPost, at + "/" + ada + "/deactivate", ``},
		{"delete", http.MethodDelete, at + "/" + ada, ``},
	}
	for _, door := range doors {
		code, body := call(t, router, door.method, door.path, door.body)
		if code != http.StatusUnprocessableEntity {
			t.Errorf("%s on the last administrator = %d %s, want 422", door.name, code, body)
			continue
		}
		if !strings.Contains(body, "ada@acme.test") || !strings.Contains(body, "owner") {
			t.Errorf("%s refused without naming the person or the role: %s", door.name, body)
		}
	}

	// Every refusal rolled its whole transaction back, so ada is where she
	// was: still active, still holding the role, still readable.
	code, body := call(t, router, http.MethodGet, at+"/"+ada, ``)
	if code != http.StatusOK || !strings.Contains(body, `"owner"`) || !strings.Contains(body, `"active"`) {
		t.Fatalf("after three refusals ada is %d %s", code, body)
	}

	// And the floor is the last one leaving rather than a headcount: a second
	// administrator makes every one of those three writes ordinary again.
	grace := administrator(t, router, "grace@acme.test")
	if code, body = call(t, router, http.MethodPost, at+"/"+ada+"/roles", `{"roles":[]}`); code != http.StatusOK {
		t.Errorf("standing down while grace administers = %d %s, want 200", code, body)
	}
	if code, body = call(t, router, http.MethodDelete, at+"/"+grace, ``); code != http.StatusUnprocessableEntity {
		t.Errorf("deleting grace, now the last administrator = %d %s, want 422", code, body)
	}
}

// administrator invites somebody and grants them the role this file's
// Administration treats as able to change a role again, then returns their id.
func administrator(t *testing.T, router chi.Router, email string) string {
	t.Helper()
	code, body := call(t, router, http.MethodPost, at, `{"email":"`+email+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("creating %s = %d %s", email, code, body)
	}
	id := field(t, body, "id")
	if code, body = call(t, router, http.MethodPost, at+"/"+id+"/roles", `{"roles":["owner"]}`); code != http.StatusOK {
		t.Fatalf("granting owner to %s = %d %s", email, code, body)
	}
	// Active, so the case is about the floor and not about a lifecycle state:
	// a password is what makes an invited person somebody who can sign in.
	if code, body = call(t, router, http.MethodPost, at+"/"+id+"/set-password",
		`{"password":"correct horse battery staple"}`); code != http.StatusOK {
		t.Fatalf("setting %s's password = %d %s", email, code, body)
	}
	return id
}

// TestAModuleWithNoRoleSystemIsRefusedAtComposition.
//
// Deps.Administration is required, and this is what "required" means: a
// product that composes this module without it stops at boot with a message
// naming the field, rather than serving an application whose floor silently
// permits every lockout. file.Deps.Storage and notification.Deps.Mailer are the
// precedent and the reason the failure is a panic and not a returned error —
// composition happens in main, before there is anywhere to report to.
func TestAModuleWithNoRoleSystemIsRefusedAtComposition(t *testing.T) {
	defer func() {
		reason, ok := recover().(string)
		if !ok || !strings.Contains(reason, "Deps.Administration") {
			t.Fatalf("composing without an Administration panicked with %v, want the field named", reason)
		}
	}()
	user.Module(user.Deps{})
	t.Fatal("composing without an Administration was allowed")
}

// surfacesOf is the module's view of the kernel: the three routers, named the
// way a composition names them at mount. The test keeps the *httpx.API
// separately, because validating the composition is the composition's job and
// holding a *Router would be holding one door of three.
func surfacesOf(a *httpx.API) httpx.Surfaces { return a.Surfaces("user") }
