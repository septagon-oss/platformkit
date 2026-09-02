package crud_test

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const host = "acme.test"

// caller is the three answers httpx.New needs, all of them yes.
type caller struct{}

func (caller) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return acme, nil
}
func (caller) Allowed(context.Context, tenancy.Tenant, string) (bool, error) { return true, nil }

var spec = crud.Spec[*Task]{
	Module: "tasks", Entity: "task", Path: "/api/tasks",
	Read: "task:read", Write: "task:write", SoftDelete: true,
}

// mounted is the Spec behind the real API, with the real middleware chain: the
// tenant resolved from the host, the transaction opened lazily, the permission
// checked, and the response held until the commit.
func mounted(t *testing.T) (*httpx.API, chi.Router, *sql.DB) {
	t.Helper()
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: app, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (httpx.Principal, bool, error) {
			return httpx.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	spec.Mount(api)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return api, router, admin
}

func call(t *testing.T, r http.Handler, method, path, body string) (int, string) {
	t.Helper()
	var reader *strings.Reader = strings.NewReader(body)
	req := httptest.NewRequest(method, "http://"+host+path, reader)
	// The kernel asks the identity hook only about a request that presents
	// something to recognise, so a request that expects to be signed in carries
	// one. See kit/httpx.credentialed.
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func id(t *testing.T, body string) string {
	t.Helper()
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.ID == "" {
		t.Fatalf("no id in %s: %v", body, err)
	}
	return out.ID
}

// TestTheFiveRoutesAnswer walks every status the Spec can produce, through the
// whole chain, against a real Postgres.
func TestTheFiveRoutesAnswer(t *testing.T) {
	_, router, _ := mounted(t)

	code, body := call(t, router, http.MethodPost, "/api/tasks", `{"title":"first","priority":2}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s, want 201", code, body)
	}
	first := id(t, body)

	// The server owns the id and the timestamps: what the caller sent for them
	// is discarded rather than honoured.
	code, body = call(t, router, http.MethodPost, "/api/tasks",
		`{"title":"second","id":"00000000-0000-0000-0000-000000000001"}`)
	if code != http.StatusCreated || id(t, body) == "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("POST with a chosen id = %d %s", code, body)
	}

	if code, body = call(t, router, http.MethodGet, "/api/tasks", ""); code != http.StatusOK ||
		!strings.Contains(body, `"total":2`) {
		t.Errorf("GET collection = %d %s, want 200 and two rows", code, body)
	}
	if code, body = call(t, router, http.MethodGet, "/api/tasks?sort=-priority&limit=1", ""); code != http.StatusOK ||
		!strings.Contains(body, `"first"`) || strings.Contains(body, `"second"`) {
		t.Errorf("sorted page = %d %s", code, body)
	}
	if code, body = call(t, router, http.MethodGet, "/api/tasks?filter=title:second", ""); code != http.StatusOK ||
		!strings.Contains(body, `"total":1`) || !strings.Contains(body, `"second"`) {
		t.Errorf("filtered page = %d %s", code, body)
	}

	if code, body = call(t, router, http.MethodGet, "/api/tasks/"+first, ""); code != http.StatusOK ||
		!strings.Contains(body, `"first"`) {
		t.Errorf("GET item = %d %s", code, body)
	}
	if code, _ = call(t, router, http.MethodGet, "/api/tasks/"+uuid.NewString(), ""); code != http.StatusNotFound {
		t.Errorf("GET a row nobody has = %d, want 404", code)
	}

	if code, body = call(t, router, http.MethodPatch, "/api/tasks/"+first, `{"status":"done"}`); code != http.StatusOK ||
		!strings.Contains(body, `"done"`) {
		t.Errorf("PATCH = %d %s", code, body)
	}
	if code, _ = call(t, router, http.MethodDelete, "/api/tasks/"+first, ""); code != http.StatusNoContent {
		t.Errorf("DELETE = %d, want 204", code)
	}
	if code, _ = call(t, router, http.MethodDelete, "/api/tasks/"+first, ""); code != http.StatusNotFound {
		t.Errorf("DELETE twice = %d, want 404", code)
	}
}

// TestTheRoutesRefuseWithAProblem: every refusal is RFC 9457, and every status
// is the one that says what happened.
func TestTheRoutesRefuseWithAProblem(t *testing.T) {
	_, router, _ := mounted(t)
	if code, _ := call(t, router, http.MethodPost, "/api/tasks", `{"title":"only one"}`); code != http.StatusCreated {
		t.Fatalf("POST = %d", code)
	}
	for _, tt := range []struct {
		what   string
		method string
		path   string
		body   string
		want   int
	}{
		{"a title the entity refuses", http.MethodPost, "/api/tasks", `{"title":"   "}`, http.StatusUnprocessableEntity},
		{"a title already taken", http.MethodPost, "/api/tasks", `{"title":"only one"}`, http.StatusConflict},
		{"a field that does not exist", http.MethodPatch, "/api/tasks/%s", `{"nonesuch":1}`, http.StatusUnprocessableEntity},
		{"a field the server owns", http.MethodPatch, "/api/tasks/%s", `{"createdAt":"2020-01-01T00:00:00Z"}`, http.StatusUnprocessableEntity},
		{"a filter on nothing", http.MethodGet, "/api/tasks?filter=nonesuch:1", "", http.StatusUnprocessableEntity},
		{"a sort on nothing", http.MethodGet, "/api/tasks?sort=nonesuch", "", http.StatusUnprocessableEntity},
	} {
		t.Run(tt.what, func(t *testing.T) {
			// Each refusal needs its own row, because a statement Postgres
			// refuses ends the request's transaction with it.
			path := tt.path
			if strings.Contains(path, "%s") {
				_, created := call(t, router, http.MethodPost, "/api/tasks", `{"title":"`+tt.what+`"}`)
				path = strings.Replace(path, "%s", id(t, created), 1)
			}
			code, body := call(t, router, tt.method, path, tt.body)
			if code != tt.want {
				t.Errorf("%s = %d %s, want %d", tt.what, code, body, tt.want)
			}
			if ct := "problem"; !strings.Contains(body, ct) && !strings.Contains(body, `"status":`) {
				t.Errorf("%s answered %s, which is not a problem document", tt.what, body)
			}
		})
	}
}

// TestEveryRouteCarriesItsDeclaration reads the recording the boot gate reads:
// the reads are guarded by Read, the writes by Write, and the three writes say
// which event they will publish.
func TestEveryRouteCarriesItsDeclaration(t *testing.T) {
	api, _, _ := mounted(t)
	want := map[string]string{
		"tasks-task-list":   `{"kind":"permission","permission":"task:read"}`,
		"tasks-task-read":   `{"kind":"permission","permission":"task:read"}`,
		"tasks-task-create": `{"kind":"permission","permission":"task:write"}`,
		"tasks-task-update": `{"kind":"permission","permission":"task:write"}`,
		"tasks-task-delete": `{"kind":"permission","permission":"task:write"}`,
	}
	seen := map[string]bool{}
	for _, op := range api.Recorded() {
		declared, ok := want[op.OperationID]
		if !ok {
			continue
		}
		seen[op.OperationID] = true
		got, err := json.Marshal(op.Extensions[httpx.AuthExtension])
		if err != nil || string(got) != declared {
			t.Errorf("%s declares %s, want %s (%v)", op.OperationID, got, declared, err)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s was never registered", name)
		}
	}
	if got := strings.Join(api.Events(), " "); got != "tasks.task.created tasks.task.deleted tasks.task.updated" {
		t.Errorf("the routes publish %q", got)
	}
	if got := strings.Join(spec.Events(), " "); got != "tasks.task.created tasks.task.updated tasks.task.deleted" {
		t.Errorf("Spec.Events = %q", got)
	}
}

// TestAWriteAndItsEventCommitTogether: the outbox row is written by the same
// transaction as the row it describes, so one cannot outlive the other — and a
// write that was refused leaves neither.
func TestAWriteAndItsEventCommitTogether(t *testing.T) {
	_, router, admin := mounted(t)
	code, body := call(t, router, http.MethodPost, "/api/tasks", `{"title":"audited"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	created := id(t, body)
	if code, _ := call(t, router, http.MethodPatch, "/api/tasks/"+created, `{"status":"done"}`); code != http.StatusOK {
		t.Fatalf("PATCH = %d", code)
	}
	if code, _ := call(t, router, http.MethodDelete, "/api/tasks/"+created, ""); code != http.StatusNoContent {
		t.Fatalf("DELETE = %d", code)
	}
	if code, _ := call(t, router, http.MethodPost, "/api/tasks", `{"title":" "}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("the invalid POST = %d", code)
	}

	rows, err := admin.QueryContext(t.Context(),
		`SELECT name, tenant_id, payload->>'title' FROM platformkit_outbox ORDER BY created_at, id`)
	if err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name, tenant, title string
		if err := rows.Scan(&name, &tenant, &title); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if tenant != acme.ID.String() {
			t.Errorf("event %s belongs to %s, want the request's tenant", name, tenant)
		}
		got = append(got, name+" "+title)
	}
	want := "tasks.task.created audited tasks.task.updated audited tasks.task.deleted audited"
	if strings.Join(got, " ") != want {
		t.Errorf("the outbox holds %q, want %q", strings.Join(got, " "), want)
	}
}

// TestNoEventsMountsTheSameRoutesSilently, for the entity that is nobody's
// business but its own.
func TestNoEventsMountsTheSameRoutesSilently(t *testing.T) {
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	api, _ := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: app, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (httpx.Principal, bool, error) {
			return httpx.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	quiet := spec
	quiet.NoEvents = true
	quiet.Mount(api)
	if got := api.Events(); len(got) != 0 {
		t.Errorf("a silent Spec still declares %v", got)
	}
	if got := quiet.Events(); got != nil {
		t.Errorf("Spec.Events = %v, want none", got)
	}
	if len(api.Recorded()) < 5 {
		t.Errorf("%d operations, want the five routes", len(api.Recorded()))
	}
}

// TestARequestWithNoBodyIsARefusalAndNotAPanic. The entity is a pointer type
// and huma reads a pointer body as optional, so a POST with nothing in it used
// to reach the handler as a nil entity and panic on the first field the create
// stamped — one request took the process's goroutine down to a 500 and a stack.
func TestARequestWithNoBodyIsARefusalAndNotAPanic(t *testing.T) {
	_, router, _ := mounted(t)
	for _, body := range []string{"", "null"} {
		code, out := call(t, router, http.MethodPost, "/api/tasks", body)
		if code != http.StatusUnprocessableEntity && code != http.StatusBadRequest {
			t.Errorf("POST with body %q = %d %s, want a refusal", body, code, out)
		}
		if !strings.Contains(out, `"status":`) {
			t.Errorf("POST with body %q answered %s, which is not a problem document", body, out)
		}
	}
	// And the route still works, which is what says the refusal is the body's
	// and not the route's.
	if code, out := call(t, router, http.MethodPost, "/api/tasks", `{"title":"present"}`); code != http.StatusCreated {
		t.Errorf("POST with a body = %d %s, want 201", code, out)
	}
}

// TestTwoPatchesOfDifferentFieldsBothSurvive, through the routes: the update
// handler writes the columns the body named and no others.
func TestTwoPatchesOfDifferentFieldsBothSurvive(t *testing.T) {
	_, router, _ := mounted(t)
	code, body := call(t, router, http.MethodPost, "/api/tasks", `{"title":"shared","priority":1}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/tasks/" + id(t, body)

	if code, body = call(t, router, http.MethodPatch, at, `{"priority":99}`); code != http.StatusOK {
		t.Fatalf("the first PATCH = %d %s", code, body)
	}
	if code, body = call(t, router, http.MethodPatch, at, `{"status":"done"}`); code != http.StatusOK {
		t.Fatalf("the second PATCH = %d %s", code, body)
	}
	code, body = call(t, router, http.MethodGet, at, "")
	if code != http.StatusOK || !strings.Contains(body, `"priority":99`) || !strings.Contains(body, `"status":"done"`) {
		t.Errorf("after two patches the row is %s; both fields should be there", body)
	}
}
