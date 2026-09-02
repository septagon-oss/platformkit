package task_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

const (
	host = "acme.test"
	path = "/api/v1/task/tasks"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// caller is the two answers httpx.New needs, both of them yes.
type caller struct{}

func (caller) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return acme, nil
}
func (caller) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) { return true, nil }

// mounted is the module as main mounts it: the manifest's own Routes, on the
// real API, against a real Postgres. The tests below are about the Spec literal
// in module.go, so nothing here may build a Spec of its own.
func mounted(t *testing.T) (*httpx.API, chi.Router) {
	t.Helper()
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	task.Module(task.Deps{}).Routes(api)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return api, router
}

func call(t *testing.T, r http.Handler, method, at, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+at, strings.NewReader(body))
	// The session cookie is the one credential shape the kernel recognises, so
	// a test that wants its identity hook called presents one. The value is not
	// read: this file's hook answers without looking. See kit/httpx.credentialed.
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// TestThePatchRefusesTheFieldsTheLifecycleOwns. Four of a task's fields belong
// to a command: assigneeId and slaBreached, resolvedAt and resolution. A PATCH
// that could set assigneeId would make somebody responsible without moving the
// status and without task.assigned; one that could set slaBreached would forge
// the fact the SLA report counts. The generic update refuses them by name, and
// keeps changing everything else.
func TestThePatchRefusesTheFieldsTheLifecycleOwns(t *testing.T) {
	_, router := mounted(t)
	code, body := call(t, router, http.MethodPost, path, `{"title":"chiller"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := path + "/" + id(t, body)

	for _, tt := range []struct{ field, patch string }{
		{"assigneeId", `{"assigneeId":"` + uuid.NewString() + `"}`},
		{"slaBreached", `{"slaBreached":true}`},
		{"resolvedAt", `{"resolvedAt":"2020-01-01T00:00:00Z"}`},
		{"resolution", `{"resolution":"it fixed itself"}`},
	} {
		code, body := call(t, router, http.MethodPatch, at, tt.patch)
		if code != http.StatusUnprocessableEntity {
			t.Errorf("PATCH %s = %d %s, want 422: %s belongs to a command", tt.patch, code, body, tt.field)
		}
		if !strings.Contains(body, tt.field+" belongs to a route of its own") {
			t.Errorf("PATCH %s answered %s, which does not name the field it refused", tt.patch, body)
		}
	}

	if code, body := call(t, router, http.MethodPatch, at, `{"description":"the supply line"}`); code != http.StatusOK ||
		!strings.Contains(body, "the supply line") {
		t.Errorf("PATCH of the description = %d %s, want 200: no command owns it", code, body)
	}
}

// TestEveryEventTheRoutesDeclareIsInTheManifest is the boot gate kit/app runs,
// against this module alone. The create route publishes task.sla_breached
// through BreachOnArrival, which is a hook and not a handler, so the only thing
// that can say so is the Spec's HookEvents — and the manifest has to name it
// back, or the application refuses to start.
func TestEveryEventTheRoutesDeclareIsInTheManifest(t *testing.T) {
	api, _ := mounted(t)
	declared := api.Events()
	if !slices.Contains(declared, contracts.EventSLABreached) {
		t.Errorf("the routes declare %v; the create hook's event is not among them", declared)
	}
	for _, e := range declared {
		if !slices.Contains(contracts.Events, e) {
			t.Errorf("a route publishes %q and the manifest does not name it", e)
		}
	}
}

// id is the id of a created task.
func id(t *testing.T, body string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, `"id":"`)
	if !ok {
		t.Fatalf("no id in %s", body)
	}
	out, _, _ := strings.Cut(rest, `"`)
	return out
}
