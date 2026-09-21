package rest_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const host = "acme.test"

// Task is a module's entity, written the way a module writes one: a struct in a
// contracts/ package, an embedded crud.Base, a table name and nothing else. It
// is declared here as well as in kit/crud's own tests because the two packages
// are the two halves of the same idea and each has to be exercised without the
// other.
type Task struct {
	crud.Base
	Title    string     `json:"title" validate:"required" ui:"widget:text"`
	Status   string     `json:"status,omitempty" enum:"open,done" ui:"widget:select"`
	Priority int        `json:"priority,omitempty"`
	Done     bool       `json:"done,omitempty" ui:"hide:list"`
	Notes    string     `json:"notes,omitempty" gorm:"type:text"`
	DueAt    *time.Time `json:"dueAt,omitempty"`
	Secret   string     `json:"-" gorm:"-"`
	// A list field, gorm:"-" because the array codec is not what is under test
	// here; modules/user's Roles is the one that is really stored.
	Tags []string `json:"tags,omitempty" gorm:"-"`
}

func (Task) TableName() string { return "rest_tasks" }

// Validate is the optional check. An empty title is 422, not 500.
func (t *Task) Validate(context.Context) error {
	if strings.TrimSpace(t.Title) == "" {
		return errors.New("a task needs a title")
	}
	return nil
}

const ddl = `
CREATE TABLE rest_tasks (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz,
	title text NOT NULL,
	status text NOT NULL DEFAULT 'open',
	priority int NOT NULL DEFAULT 0,
	done boolean NOT NULL DEFAULT false,
	notes text NOT NULL DEFAULT '',
	due_at timestamptz,
	UNIQUE (tenant_id, title)
);
ALTER TABLE rest_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE rest_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY rest_tasks_tenant ON rest_tasks
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// principal is who every request in this file is made by. It is fixed rather
// than fresh per request so that one case can ask whether the event a route
// published names them.
var principal = uuid.New()

// caller is the three answers httpx.New needs, all of them yes.
type caller struct{}

func (caller) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return acme, nil
}
func (caller) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) { return true, nil }

var spec = rest.Spec[*Task]{
	Module: "tasks", Entity: "task", Path: "/task",
	Read: "task:read", Write: "task:write", SoftDelete: true,
}

// mounted is the Spec behind the real API, with the real middleware chain: the
// tenant resolved from the host, the transaction opened lazily, the permission
// checked, and the response held until the commit.
func mounted(t *testing.T) (*httpx.API, chi.Router, *sql.DB) { return mount(t, spec) }

// mount is mounted for a Spec that is not the plain one, so a case can vary a
// field and still get the whole chain.
func mount(t *testing.T, s rest.Spec[*Task]) (*httpx.API, chi.Router, *sql.DB) {
	return mountAs(t, s, caller{})
}

// mountAs is mount for a caller who does not hold everything, which is what the
// resource closures' own authorization is tested with. The same value answers
// both of the kernel's questions when it can — as it does in the application,
// where one auth module is both the host resolver and the authorizer — so a case
// that needs the request to resolve to a different tenant (the operator's, for a
// control-plane route) brings its own resolver rather than mounting a second API.
func mountAs[T crud.Entity](t *testing.T, s rest.Spec[T], authorize httpx.Authorizer) (*httpx.API, chi.Router, *sql.DB) {
	t.Helper()
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	loader := httpx.TenantLoader(caller{})
	if l, ok := authorize.(httpx.TenantLoader); ok {
		loader = l
	}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: loader, Conn: app, Authorize: authorize,
		// The installation is reached at the same host the customer is, in this
		// harness, so a control-plane route is mounted and reachable and the
		// answer a case sees is the authorizer's rather than the host gate's.
		// TestTheControlPlaneIsNotFoundAtATenantHost in kit/httpx is where the
		// host gate itself is tested.
		Installation: host,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	s.Mount(api.Surfaces(s.Module))
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
	// The session cookie is the one credential shape the kernel recognises, so
	// a test that wants its identity hook called presents one. The value is not
	// read: this file's hook answers without looking. See kit/httpx.credentialed.
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
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

	code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"first","priority":2}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s, want 201", code, body)
	}
	first := id(t, body)

	// The server owns the id and the timestamps: what the caller sent for them
	// is discarded rather than honoured.
	code, body = call(t, router, http.MethodPost, "/api/v1/tasks/task",
		`{"title":"second","id":"00000000-0000-0000-0000-000000000001"}`)
	if code != http.StatusCreated || id(t, body) == "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("POST with a chosen id = %d %s", code, body)
	}

	if code, body = call(t, router, http.MethodGet, "/api/v1/tasks/task", ""); code != http.StatusOK ||
		!strings.Contains(body, `"total":2`) {
		t.Errorf("GET collection = %d %s, want 200 and two rows", code, body)
	}
	if code, body = call(t, router, http.MethodGet, "/api/v1/tasks/task?sort=-priority&limit=1", ""); code != http.StatusOK ||
		!strings.Contains(body, `"first"`) || strings.Contains(body, `"second"`) {
		t.Errorf("sorted page = %d %s", code, body)
	}
	if code, body = call(t, router, http.MethodGet, "/api/v1/tasks/task?filter=title:second", ""); code != http.StatusOK ||
		!strings.Contains(body, `"total":1`) || !strings.Contains(body, `"second"`) {
		t.Errorf("filtered page = %d %s", code, body)
	}

	if code, body = call(t, router, http.MethodGet, "/api/v1/tasks/task/"+first, ""); code != http.StatusOK ||
		!strings.Contains(body, `"first"`) {
		t.Errorf("GET item = %d %s", code, body)
	}
	if code, _ = call(t, router, http.MethodGet, "/api/v1/tasks/task/"+uuid.NewString(), ""); code != http.StatusNotFound {
		t.Errorf("GET a row nobody has = %d, want 404", code)
	}

	if code, body = call(t, router, http.MethodPatch, "/api/v1/tasks/task/"+first, `{"status":"done"}`); code != http.StatusOK ||
		!strings.Contains(body, `"done"`) {
		t.Errorf("PATCH = %d %s", code, body)
	}
	if code, _ = call(t, router, http.MethodDelete, "/api/v1/tasks/task/"+first, ""); code != http.StatusNoContent {
		t.Errorf("DELETE = %d, want 204", code)
	}
	if code, _ = call(t, router, http.MethodDelete, "/api/v1/tasks/task/"+first, ""); code != http.StatusNotFound {
		t.Errorf("DELETE twice = %d, want 404", code)
	}
}

// TestTheRoutesRefuseWithAProblem: every refusal is RFC 9457, and every status
// is the one that says what happened.
func TestTheRoutesRefuseWithAProblem(t *testing.T) {
	_, router, _ := mounted(t)
	if code, _ := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"only one"}`); code != http.StatusCreated {
		t.Fatalf("POST = %d", code)
	}
	for _, tt := range []struct {
		what   string
		method string
		path   string
		body   string
		want   int
	}{
		{"a title the entity refuses", http.MethodPost, "/api/v1/tasks/task", `{"title":"   "}`, http.StatusUnprocessableEntity},
		{"a title already taken", http.MethodPost, "/api/v1/tasks/task", `{"title":"only one"}`, http.StatusConflict},
		{"an edit to a title already taken", http.MethodPatch, "/api/v1/tasks/task/%s", `{"title":"only one"}`, http.StatusConflict},
		{"a field that does not exist", http.MethodPatch, "/api/v1/tasks/task/%s", `{"nonesuch":1}`, http.StatusUnprocessableEntity},
		{"a field the server owns", http.MethodPatch, "/api/v1/tasks/task/%s", `{"createdAt":"2020-01-01T00:00:00Z"}`, http.StatusUnprocessableEntity},
		{"a filter on nothing", http.MethodGet, "/api/v1/tasks/task?filter=nonesuch:1", "", http.StatusUnprocessableEntity},
		{"a sort on nothing", http.MethodGet, "/api/v1/tasks/task?sort=nonesuch", "", http.StatusUnprocessableEntity},
	} {
		t.Run(tt.what, func(t *testing.T) {
			// Each refusal needs its own row, because a statement Postgres
			// refuses ends the request's transaction with it.
			path := tt.path
			if strings.Contains(path, "%s") {
				_, created := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"`+tt.what+`"}`)
				path = strings.Replace(path, "%s", id(t, created), 1)
			}
			code, body := call(t, router, tt.method, path, tt.body)
			if code != tt.want {
				t.Errorf("%s = %d %s, want %d", tt.what, code, body, tt.want)
			}
			if ct := "problem"; !strings.Contains(body, ct) && !strings.Contains(body, `"status":`) {
				t.Errorf("%s answered %s, which is not a problem document", tt.what, body)
			}
			if tt.want == http.StatusConflict {
				var p struct{ Detail string }
				if err := json.Unmarshal([]byte(body), &p); err != nil || p.Detail != uniqueConflictDetail {
					t.Errorf("duplicate HTTP detail = %q, %v; want %q", p.Detail, err, uniqueConflictDetail)
				}
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
	// And every route says it can answer 503. A handler that cannot reach the
	// database answers with one, so a document that does not list it describes
	// a response a caller will nevertheless get.
	for _, op := range api.Recorded() {
		if !strings.HasPrefix(op.OperationID, "tasks-task-") {
			continue
		}
		if !slices.Contains(op.Errors, http.StatusServiceUnavailable) {
			t.Errorf("%s declares %v, which does not include the 503 it can answer", op.OperationID, op.Errors)
		}
	}
}

// TestBothWriteDoorsRefuseTheFieldsACommandOwns. An immutable field is not
// read-only: the server does not own it, one route does, and a write that could
// set it would make the change without the rule and without the event that
// route publishes. So both doors refuse it and the refusal names the field,
// which is how a caller finds the door.
//
// The create used to be the way past this. Immutable was consulted in merge()
// and merge() is the patch's, so POST with the field in the body stored
// whatever the caller said — content's author, a user's roles — silently, with
// the entity's own documentation saying an actor stamps it. See
// refuseImmutable.
func TestBothWriteDoorsRefuseTheFieldsACommandOwns(t *testing.T) {
	owned := spec
	owned.Immutable = []string{"status", "done"}
	_, router, _ := mount(t, owned)

	for _, tt := range []struct{ field, create string }{
		{"status", `{"title":"forged","status":"done"}`},
		{"done", `{"title":"forged","done":true}`},
		// null is a value a caller sent, not an absence.
		{"status", `{"title":"forged","status":null}`},
	} {
		code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", tt.create)
		if code != http.StatusUnprocessableEntity {
			t.Errorf("POST %s = %d %s, want 422", tt.create, code, body)
		}
		if !strings.Contains(body, tt.field+" belongs to a route of its own") {
			t.Errorf("POST %s answered %s, which does not say which field it refused", tt.create, body)
		}
	}
	if code, body := call(t, router, http.MethodGet, "/api/v1/tasks/task", ""); !strings.Contains(body, `"total":0`) {
		t.Errorf("a refused create stored a row anyway: %d %s", code, body)
	}

	code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"owned"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/v1/tasks/task/" + id(t, body)

	for _, tt := range []struct{ field, patch string }{
		{"status", `{"status":"done"}`},
		{"done", `{"done":true}`},
	} {
		code, body := call(t, router, http.MethodPatch, at, tt.patch)
		if code != http.StatusUnprocessableEntity {
			t.Errorf("PATCH %s = %d %s, want 422", tt.patch, code, body)
		}
		if !strings.Contains(body, tt.field+" belongs to a route of its own") {
			t.Errorf("PATCH %s answered %s, which does not say which field it refused", tt.patch, body)
		}
	}

	// A field nobody reserved is still a patch, which is what says the refusal
	// is about these fields and not about PATCH.
	if code, body := call(t, router, http.MethodPatch, at, `{"title":"renamed"}`); code != http.StatusOK ||
		!strings.Contains(body, `"renamed"`) {
		t.Errorf("PATCH of a field no command owns = %d %s, want 200", code, body)
	}
}

// TestACommandOwnedFieldRefusesUnderTheDecodersOwnFolding. The door has to ask
// the question the decoder asks, and the decoder's question is not about
// spelling: encoding/json binds a struct field when no key matches it exactly
// and some key folds onto it. refuseImmutable asked instead whether a map had
// that key, so it refused "status" and let "Status" through, and the row was
// stored with the field a command owns — on every generated write route of
// every module that declares Immutable. The door now folds the way the decoder
// folds, with strings.EqualFold, and names the declared field rather than the
// spelling it was sent: the field is what points at the route that owns it.
//
// status and notes are both text so that one body shape serves both names, and
// two names say the guard folds per declared name rather than once.
func TestACommandOwnedFieldRefusesUnderTheDecodersOwnFolding(t *testing.T) {
	owned := spec
	owned.Immutable = []string{"status", "notes"}
	_, router, _ := mount(t, owned)

	code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"owned"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/v1/tasks/task/" + id(t, body)

	capital := func(s string) string { return strings.ToUpper(s[:1]) + s[1:] }
	for _, sent := range []struct {
		field string
		keys  []string
	}{
		// The declared spelling, the two ASCII ones the decoder binds, and the
		// long-s spelling: "\u017ftatus" names status to encoding/json because the
		// decoder's comparison is Unicode simple case folding, not ASCII case.
		// strings.EqualFold is that same comparison — every BMP rune as a whole
		// key against twenty-six one-letter names, and every BMP rune in every
		// position of two real names, and the keys it calls equal and the keys
		// the decoder binds are the same set — so the door agrees with the
		// decoder about which key names which field, and about nothing else.
		{"status", []string{"status", "Status", "STATUS", "\u017ftatus"}},
		{"notes", []string{"notes", capital("notes"), strings.ToUpper("notes")}},
	} {
		for _, key := range sent.keys {
			field := sent.field
			// The patch door has the map's exact keys to go on, so the sentence
			// naming the declared field can only come from the bytes below it.
			if code, body := call(t, router, http.MethodPatch, at, `{"`+key+`":"done"}`); code != http.StatusUnprocessableEntity ||
				!strings.Contains(body, field+" belongs to a route of its own") {
				t.Errorf(`PATCH {"%s":…} = %d %s, want 422 naming %q`, key, code, body, field)
			}
			if code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"forged","`+key+`":"done"}`); code != http.StatusUnprocessableEntity ||
				!strings.Contains(body, field+" belongs to a route of its own") {
				t.Errorf(`POST {"%s":…} = %d %s, want 422 naming %q`, key, code, body, field)
			}
		}
	}

	// A refused write leaves nothing, the mutable field of the same body
	// included: the door refuses the write, it does not trim the field from it.
	if code, body := call(t, router, http.MethodGet, "/api/v1/tasks/task", ""); code != http.StatusOK ||
		!strings.Contains(body, `"total":1`) || strings.Contains(body, "forged") {
		t.Errorf("a refused write left a row: %d %s", code, body)
	}

	// What the fold does not reach. A mutable key still binds at the create, as
	// the decoder has always bound it, and still gets the unknown-field refusal
	// at the patch, whose body is a map and so never folds at all. Both are
	// today's behaviour, pinned so that no later edit for consistency moves one
	// without saying so here.
	if code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"Title":"Mutable"}`); code != http.StatusCreated ||
		!strings.Contains(body, `"title":"Mutable"`) {
		t.Errorf(`POST {"Title":"Mutable"} = %d %s, want 201 with the title bound as the decoder binds it`, code, body)
	}
	if code, body := call(t, router, http.MethodPatch, at, `{"Title":"renamed"}`); code != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "there is no field") || strings.Contains(body, "route of its own") {
		t.Errorf(`PATCH {"Title":"renamed"} = %d %s, want the unknown-field refusal it has always given`, code, body)
	}

	// And a body that is not an object stays the decoder's refusal, not this
	// one's: the guard reads the same bytes and asks one question, and a body it
	// cannot read names no field at all.
	for _, notAnObject := range []string{`[]`, `null`, `"a"`} {
		if _, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", notAnObject); !strings.Contains(body, "expected object") {
			t.Errorf("POST %s answered %s, want the decoder's own refusal", notAnObject, body)
		}
		if _, body := call(t, router, http.MethodPatch, at, notAnObject); !strings.Contains(body, "expected object") {
			t.Errorf("PATCH %s answered %s, want the decoder's own refusal", notAnObject, body)
		}
	}
}

// TestAPatchThatNamesNoColumnWritesNothingAndPublishesNothing. A PATCH whose
// body names no column changed nothing, so it writes nothing and says nothing:
// no UPDATE, no event, the row as it was read and updated_at where it stood.
// The route used to timestamp and publish whatever the body said, so the only
// body an entity whose every field a command owns accepts — {} — moved
// updated_at and filled the outbox on every call, and a consumer of
// <module>.<entity>.updated saw an update nobody made. The door beneath HTTP
// runs the same rule: a row that did not change is not something that happened.
func TestAPatchThatNamesNoColumnWritesNothingAndPublishesNothing(t *testing.T) {
	_, router, admin := mounted(t)
	code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"untouched"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	created, at := stampedAt(t, body), "/api/v1/tasks/task/"+id(t, body)
	stored := stampedRowAt(t, admin, at)

	if code, body := call(t, router, http.MethodPatch, at, `{}`); code != http.StatusOK {
		t.Fatalf("PATCH {} = %d %s", code, body)
	} else if got := stampedAt(t, body); got != created {
		t.Errorf("PATCH {} returned a row stamped %s, the create stamped it %s", got, created)
	}
	if got := stampedRowAt(t, admin, at); got != stored {
		t.Errorf("PATCH {} wrote the row: updated_at went %s to %s", stored, got)
	}
	if n := count(t, admin, spec.Event(rest.Updated)); n != 0 {
		t.Errorf("PATCH {} published %d %s events, want none", n, spec.Event(rest.Updated))
	}

	// The control, so the case above cannot pass because the route stopped
	// working: a body that does name a column still writes, still stamps and
	// still publishes.
	if code, body := call(t, router, http.MethodPatch, at, `{"title":"touched"}`); code != http.StatusOK {
		t.Fatalf(`PATCH {"title":"touched"} = %d %s`, code, body)
	}
	if got := stampedRowAt(t, admin, at); got == stored {
		t.Errorf("a PATCH that names a column left updated_at at %s", got)
	}
	if n := count(t, admin, spec.Event(rest.Updated)); n != 1 {
		t.Errorf("a PATCH that names a column published %d events, want the one", n)
	}
}

// stampedAt is the stamp a response says the row carries.
func stampedAt(t *testing.T, body string) string {
	t.Helper()
	var out struct {
		UpdatedAt string `json:"updatedAt"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.UpdatedAt == "" {
		t.Fatalf("no updatedAt in %s: %v", body, err)
	}
	return out.UpdatedAt
}

// stampedRowAt is the stamp the row carries in the database, which is the only
// answer to "did this write reach it": an empty object returned unchanged can
// come from a row nobody touched or from a row that was and was answered from
// the snapshot.
func stampedRowAt(t *testing.T, admin *sql.DB, at string) string {
	t.Helper()
	var got string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT updated_at::text FROM rest_tasks WHERE id = $1`, at[strings.LastIndex(at, "/")+1:]).Scan(&got); err != nil {
		t.Fatalf("read the row's stamp: %v", err)
	}
	return got
}

// TestAHooksEventsAreDeclaredWhereTheGateLooks. A hook publishes from inside
// the write's transaction and nothing reads a hook, so a Spec has to say what
// its hooks emit. HookEvents puts that on the write operations, which is the
// recording kit/app's boot gate reads.
//
// The gate compares what the routes declare with what the manifests declare —
// never with what a handler does — so this closes one direction: an event
// declared and unnamed by any manifest fails startup, and an event a hook
// publishes and nobody declared is invisible. Both halves are below.
func TestAHooksEventsAreDeclaredWhereTheGateLooks(t *testing.T) {
	const hooked = "tasks.task.escalated"
	hook := func(ctx context.Context, tx db.Tx[db.Tenant], e *Task) error {
		return events.Publish(ctx, tx, hooked, e)
	}

	undeclared := spec
	undeclared.AfterCreate = hook
	quiet, router, admin := mount(t, undeclared)
	if slices.Contains(quiet.Events(), hooked) {
		t.Errorf("an undeclared hook event reached the recording as %v", quiet.Events())
	}
	// And it is published all the same: the hook runs, the row is written, and
	// no gate anywhere saw it coming. That is the hole HookEvents closes.
	if code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"escalated"}`); code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	if got := count(t, admin, hooked); got != 1 {
		t.Fatalf("the hook published %d events, want the one nothing declared", got)
	}

	declared := undeclared
	declared.HookEvents = []string{hooked}
	api, _, _ := mount(t, declared)
	if !slices.Contains(api.Events(), hooked) {
		t.Fatalf("the routes declare %v, which does not include the hook's event", api.Events())
	}
	// kit/app's gate, which is this comparison and nothing else.
	if got := missing(api.Events(), spec.Events()); len(got) != 1 || got[0] != hooked {
		t.Errorf("a manifest naming only the Spec's own events leaves %v undeclared, want just the hook's", got)
	}
	if got := missing(api.Events(), append(spec.Events(), hooked)); len(got) != 0 {
		t.Errorf("a manifest naming every event still leaves %v undeclared", got)
	}
}

// missing is kit/app's validateEvents: every event the routes say they publish
// that no manifest declared. It is spelled here because the gate is in kit/app
// and this is the recording it reads.
func missing(routes, manifest []string) []string {
	var out []string
	for _, e := range routes {
		if !slices.Contains(manifest, e) {
			out = append(out, e)
		}
	}
	return out
}

// count is how many outbox rows carry this event name.
func count(t *testing.T, admin *sql.DB, name string) int {
	t.Helper()
	var n int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE name = $1`, name).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", name, err)
	}
	return n
}

// TestAWriteAndItsEventCommitTogether: the outbox row is written by the same
// transaction as the row it describes, so one cannot outlive the other — and a
// write that was refused leaves neither.
func TestAWriteAndItsEventCommitTogether(t *testing.T) {
	_, router, admin := mounted(t)
	code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"audited"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	created := id(t, body)
	if code, _ := call(t, router, http.MethodPatch, "/api/v1/tasks/task/"+created, `{"status":"done"}`); code != http.StatusOK {
		t.Fatalf("PATCH = %d", code)
	}
	if code, _ := call(t, router, http.MethodDelete, "/api/v1/tasks/task/"+created, ""); code != http.StatusNoContent {
		t.Fatalf("DELETE = %d", code)
	}
	if code, _ := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":" "}`); code != http.StatusUnprocessableEntity {
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

// TestARequestWithNoBodyIsARefusalAndNotAPanic. The entity is a pointer type
// and huma reads a pointer body as optional, so a POST with nothing in it used
// to reach the handler as a nil entity and panic on the first field the create
// stamped — one request took the process's goroutine down to a 500 and a stack.
func TestARequestWithNoBodyIsARefusalAndNotAPanic(t *testing.T) {
	_, router, _ := mounted(t)
	for _, body := range []string{"", "null"} {
		code, out := call(t, router, http.MethodPost, "/api/v1/tasks/task", body)
		if code != http.StatusUnprocessableEntity && code != http.StatusBadRequest {
			t.Errorf("POST with body %q = %d %s, want a refusal", body, code, out)
		}
		if !strings.Contains(out, `"status":`) {
			t.Errorf("POST with body %q answered %s, which is not a problem document", body, out)
		}
	}
	// And the route still works, which is what says the refusal is the body's
	// and not the route's.
	if code, out := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"present"}`); code != http.StatusCreated {
		t.Errorf("POST with a body = %d %s, want 201", code, out)
	}
}

// TestTwoPatchesOfDifferentFieldsBothSurvive, through the routes: the update
// handler writes the columns the body named and no others.
func TestTwoPatchesOfDifferentFieldsBothSurvive(t *testing.T) {
	_, router, _ := mounted(t)
	code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"shared","priority":1}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/v1/tasks/task/" + id(t, body)

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

// TestSpecRefusesToMountNonsense: a Spec that could only produce broken routes
// fails at the mount site, like every other wiring mistake in this kernel.
func TestSpecRefusesToMountNonsense(t *testing.T) {
	for name, spec := range map[string]rest.Spec[*Task]{
		"no path":        {Module: "tasks", Entity: "task", Path: "api", Read: "task:read", Write: "task:write"},
		"no event name":  {Module: "Tasks", Entity: "task", Path: "/api", Read: "task:read", Write: "task:write"},
		"bad permission": {Module: "tasks", Entity: "task", Path: "/api", Read: "read", Write: "task:write"},
		"immutable nothing": {Module: "tasks", Entity: "task", Path: "/api", Read: "task:read", Write: "task:write",
			Immutable: []string{"nonesuch"}},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("Mount accepted it")
				}
			}()
			spec.Mount(httpx.Surfaces{})
		})
	}
}

// Prefs is a second entity, here only to carry a widget no screen can draw.
type Prefs struct {
	crud.Base
	Theme string `json:"theme,omitempty" ui:"widget:colour"`
}

func (Prefs) TableName() string { return "rest_prefs" }

// TestAWidgetNoScreenCanDrawIsRefusedAtMount.
//
// A `ui:"widget:…"` name the renderer does not know used to draw a plain text
// input and say nothing, which is how `widget:file` ended up with a file
// component in the library and no form that could ask for it. kit/entity owns
// the vocabulary because ui/forms owns the renderers and cannot be imported
// from a kernel package below the presentation layer, so the refusal happens at
// the mount site, where every other wiring mistake is already made loud.
func TestAWidgetNoScreenCanDrawIsRefusedAtMount(t *testing.T) {
	// The message is the assertion. Mount on an API with no router behind it
	// panics for its own reasons, so a bare recover() here would be satisfied by
	// the wrong accident — which is exactly how a test like this goes green while
	// the rule it names goes unenforced.
	var recovered any
	defer func() {
		recovered = recover()
		if recovered == nil {
			t.Error("Mount accepted an entity whose widget renders nothing")
			return
		}
		message, _ := recovered.(string)
		if !strings.Contains(message, `widget "colour"`) || !strings.Contains(message, "no screen") {
			t.Errorf("Mount refused for another reason: %v", recovered)
		}
	}()
	rest.Spec[*Prefs]{Module: "prefs", Entity: "pref", Path: "/preferences",
		Read: "pref:read", Write: "pref:write"}.Mount(httpx.Surfaces{})
}

// TestASchemaCarriesAListAndNothingSortsOnIt. A list renders — a user's roles
// is the case that made the type necessary — and it is not something a query
// compares against: "tags = ?" is not a question about any of the values in the
// column, so the two routes that take a field name refuse it rather than
// producing SQL nobody meant.
func TestASchemaCarriesAListAndNothingSortsOnIt(t *testing.T) {
	recorded, router, _ := mounted(t)

	var tags crud.Field
	for _, f := range spec.Schema().Fields {
		if f.Name == "tags" {
			tags = f
		}
	}
	if tags.Type != crud.TypeList || tags.Elem != crud.TypeString {
		t.Errorf("the schema says tags is %+v, want a list of strings", tags)
	}

	for _, q := range []string{"?sort=tags", "?filter=tags:red"} {
		if code, body := call(t, router, http.MethodGet, "/api/v1/tasks/task"+q, ""); code != http.StatusUnprocessableEntity {
			t.Errorf("GET /api/v1/tasks/task%s = %d %s, want 422", q, code, body)
		}
	}

	// And the list route does not offer it: a document that named a field every
	// request for it is refused on would be describing a 422.
	for _, op := range recorded.Recorded() {
		if op.OperationID == "tasks-task-list" && strings.Contains(op.Description, "tags") {
			t.Errorf("the list route offers to sort by a list: %s", op.Description)
		}
	}
}

// TestAnEventNamesTheCallerWhoCausedIt.
//
// The actor is on the envelope and not in any payload, so it is the same
// question about every event whatever module wrote it, and no module passes it
// along: kit/httpx puts the recognised caller on the request context and
// kit/events.Publish reads it there. What a route publishes therefore names the
// person who called it without the route, the Spec or the entity knowing that
// anybody was asked.
func TestAnEventNamesTheCallerWhoCausedIt(t *testing.T) {
	_, router, admin := mounted(t)

	if code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"first"}`); code != http.StatusCreated {
		t.Fatalf("POST = %d %s, want 201", code, body)
	}
	var actor *uuid.UUID
	err := admin.QueryRowContext(t.Context(),
		`SELECT actor FROM platformkit_outbox WHERE name = $1`, "tasks.task.created").Scan(&actor)
	if err != nil {
		t.Fatalf("read the actor: %v", err)
	}
	if actor == nil || *actor != principal {
		t.Errorf("the created event names %v, want the caller %s", actor, principal)
	}
}
