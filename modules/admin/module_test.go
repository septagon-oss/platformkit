package admin_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/ui"
)

const host = "acme.test"

// Note is a module's entity, declared here rather than imported so that this
// test exercises the generator and not one particular module's tags: every
// widget the screens can produce is on this struct.
type Note struct {
	crud.Base
	Title  string     `json:"title" validate:"required"`
	Body   string     `json:"body,omitempty" gorm:"type:text" ui:"hide:list"`
	Status string     `json:"status" enum:"open,done" ui:"widget:select" required:"false"`
	Rank   int        `json:"rank,omitempty"`
	Pinned bool       `json:"pinned" required:"false"`
	Owner  *uuid.UUID `json:"owner,omitempty" ui:"widget:entity-picker"`
	Tags   []string   `json:"tags,omitempty" gorm:"-"`
}

func (Note) TableName() string { return "admin_notes" }

// Validate is the entity's own check, so the generator has a refusal to render.
func (n *Note) Validate(context.Context) error {
	if strings.TrimSpace(n.Title) == "" {
		return errors.New("a note needs a title")
	}
	return nil
}

const ddl = `
CREATE TABLE admin_notes (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz,
	title text NOT NULL,
	body text NOT NULL DEFAULT '',
	status text NOT NULL DEFAULT 'open',
	rank int NOT NULL DEFAULT 0,
	pinned boolean NOT NULL DEFAULT false,
	owner uuid
);
ALTER TABLE admin_notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE admin_notes FORCE ROW LEVEL SECURITY;
CREATE POLICY admin_notes_tenant ON admin_notes
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// caller resolves the host and answers every permission but one, so the nav
// filter has something to hide.
type caller struct{}

func (caller) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return acme, nil
}

func (caller) Allowed(_ context.Context, _ tenancy.Tenant, g tenancy.Grant) (bool, error) {
	return g.Permission != "secret:read", nil
}

var spec = rest.Spec[*Note]{
	Module: "notes", Entity: "note", Path: "/api/v1/notes/notes",
	Read: "note:read", Write: "note:write", SoftDelete: true,
	Immutable: []string{"rank"},
}

// mount composes a two-module application — the entity, and the shell after it
// — behind the real kernel: the tenant resolved from the host, the transaction
// opened lazily, the permission checked, the response held until the commit.
func mount(t *testing.T) chi.Router {
	t.Helper()
	adminDB, app := dbtest.Schema(t)
	if _, err := adminDB.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("create notes: %v", err)
	}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: app, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New(), Roles: []string{"admin"}}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	notes := module.Module{
		Name:        "notes",
		Permissions: []module.Permission{{Key: "note:read"}, {Key: "note:write"}, {Key: "secret:read"}},
		Events:      spec.Events(),
		Nav: []module.NavEntry{
			{Label: "Notes", Path: "/admin/notes/notes", Permission: "note:read"},
			{Label: "Secrets", Path: "/admin/notes/secrets", Permission: "secret:read"},
		},
		Routes: func(api *httpx.API) { spec.Mount(api) },
	}
	shell := admin.Module(admin.Deps{Modules: []module.Module{notes}, Authorize: caller{}})
	if err := module.Validate([]module.Module{notes, shell}); err != nil {
		t.Fatalf("the composition is invalid: %v", err)
	}
	notes.Routes(api)
	shell.Routes(api)
	// The gate every route in this application passes, the shell's included.
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("a screen does not declare its authorization: %v", err)
	}
	return router
}

func call(t *testing.T, r http.Handler, method, path, body string) (int, string, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	// The session cookie is the one credential shape the kernel recognises, so
	// a test that wants its identity hook called presents one. The value is not
	// read: the hook above answers without looking.
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String(), w.Header().Get("Location")
}

// TestTheScreensAreGeneratedFromTheSchema is stage E4's claim, checked against
// an entity this package has never heard of: seven screens, and every control
// on them decided by a struct tag.
func TestTheScreensAreGeneratedFromTheSchema(t *testing.T) {
	router := mount(t)

	code, body, _ := call(t, router, http.MethodGet, "/admin/notes/notes", "")
	if code != http.StatusOK {
		t.Fatalf("the list = %d %s", code, body)
	}
	for _, want := range []string{">Title<", ">Status<", ">Rank<", "No notes yet."} {
		if !strings.Contains(body, want) {
			t.Errorf("the list has no %s", want)
		}
	}
	if strings.Contains(body, ">Body<") {
		t.Error(`the list shows a field tagged ui:"...;hide:list"`)
	}

	code, form, _ := call(t, router, http.MethodGet, "/admin/notes/notes/new", "")
	if code != http.StatusOK {
		t.Fatalf("the new form = %d %s", code, form)
	}
	for what, want := range map[string]string{
		"an enum renders a select":              `<select`,
		"a text column renders a textarea":      `<textarea`,
		"a bool renders a checkbox":             `type="checkbox"`,
		"an int renders a number":               `type="number"`,
		"a required field says so":              `required`,
		"an entity-picker says there is none":   "There is no picker for it yet",
		"an immutable field is read-only":       "Changed by a command of its own",
		"a list field says how to write one":    "Comma separated",
		"the server owns the id and timestamps": `name="title"`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("%s: %q is not in the form", what, want)
		}
	}
	if strings.Contains(form, `name="createdAt"`) {
		t.Error("the form offers a field the server owns")
	}

	// A refusal is a 422 that renders the form again with the message on it,
	// which is what htmx swaps in place and what a browser without it shows.
	code, refused, _ := call(t, router, http.MethodPost, "/admin/notes/notes", "title=&status=open")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("an empty title = %d, want 422", code)
	}
	if !strings.Contains(refused, "a note needs a title") && !strings.Contains(refused, "title") {
		t.Errorf("the refusal says nothing about the field: %s", refused)
	}

	code, body, location := call(t, router, http.MethodPost, "/admin/notes/notes",
		"title=First+note&status=done&rank=3&pinned=on&body=Some+prose&tags=a,+b")
	if code != http.StatusSeeOther {
		t.Fatalf("a create = %d %s", code, body)
	}
	id := strings.TrimPrefix(location, "/admin/notes/notes/")
	if _, err := uuid.Parse(id); err != nil {
		t.Fatalf("a create redirected to %q", location)
	}

	code, detail, _ := call(t, router, http.MethodGet, location, "")
	if code != http.StatusOK {
		t.Fatalf("the detail = %d %s", code, detail)
	}
	// Tags is gorm:"-" on this entity, so it is in the schema and not in the
	// table: the form renders it, and the detail shows the dash a column with
	// nothing in it gets.
	for _, want := range []string{"First note", "Some prose", "done", "true", "—"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the detail does not show %q", want)
		}
	}

	// Rank is Immutable, so the form posts it back and the screen drops it
	// rather than earning the 422 the API would answer with.
	code, body, location = call(t, router, http.MethodPost, location, "title=Renamed&status=open&rank=9")
	if code != http.StatusSeeOther {
		t.Fatalf("an update = %d %s", code, body)
	}
	_, detail, _ = call(t, router, http.MethodGet, location, "")
	if !strings.Contains(detail, "Renamed") || strings.Contains(detail, ">9<") {
		t.Errorf("the update did not land, or it wrote a field a command owns: %s", detail)
	}

	code, _, location = call(t, router, http.MethodPost, "/admin/notes/notes/"+id+"/delete", "")
	if code != http.StatusSeeOther || location != "/admin/notes/notes" {
		t.Fatalf("a delete = %d to %q", code, location)
	}
	if code, _, _ = call(t, router, http.MethodGet, "/admin/notes/notes/"+id, ""); code != http.StatusNotFound {
		t.Errorf("a deleted row still has a screen: %d", code)
	}
}

// TestTheSidebarShowsWhatTheCallerMayReach is the other half of the guard: the
// pages are protected by the kernel, and the navigation asks the same
// authorizer, so a link that is shown is a link that works.
func TestTheSidebarShowsWhatTheCallerMayReach(t *testing.T) {
	router := mount(t)
	_, body, _ := call(t, router, http.MethodGet, "/admin", "")
	if !strings.Contains(body, ">Notes<") {
		t.Error("the sidebar hides a screen the caller may read")
	}
	if strings.Contains(body, ">Secrets<") {
		t.Error("the sidebar offers a screen the caller may not read")
	}
	if !strings.Contains(body, "Dashboard") || !strings.Contains(body, "Health") {
		t.Error("the sidebar lost the shell's own pages")
	}
}

// TestEveryClassTheShellRendersHasARule closes the loop the whole UI stack
// rests on, from the far end: ui/components proves its declarations resolve,
// and this proves the shell declares nothing else. A page styled with a class
// the stylesheet does not carry renders as unstyled HTML, and nothing else
// would notice.
func TestEveryClassTheShellRendersHasARule(t *testing.T) {
	router := mount(t)
	stylesheet := string(ui.Stylesheet())
	// A prefixed utility is one escaped colon inside the class name and one
	// unescaped colon before the pseudo-class, so the escaped ones are put
	// aside before the pseudo is stripped and put back afterwards.
	rules := map[string]bool{}
	pseudo := regexp.MustCompile(`(?::+[a-z-]+(?:\(.*?\))?)+$`)
	for _, m := range regexp.MustCompile(`(?m)^\s*\.((?:[^{,\s]|\\.)+)`).FindAllStringSubmatch(stylesheet, -1) {
		name := pseudo.ReplaceAllString(strings.ReplaceAll(m[1], `\:`, "\x00"), "")
		rules[strings.ReplaceAll(strings.ReplaceAll(name, "\x00", ":"), `\`, "")] = true
	}
	if len(rules) < 100 {
		t.Fatalf("the stylesheet has %d rules; the parser above is wrong", len(rules))
	}
	for _, path := range []string{"/admin", "/admin/login", "/admin/health", "/admin/_gallery",
		"/admin/notes/notes", "/admin/notes/notes/new"} {
		_, body, _ := call(t, router, http.MethodGet, path, "")
		var missing []string
		for _, m := range regexp.MustCompile(`class="([^"]*)"`).FindAllStringSubmatch(body, -1) {
			for _, class := range strings.Fields(m[1]) {
				if !rules[class] {
					missing = append(missing, class)
				}
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s renders %d classes with no rule: %v", path, len(missing), missing[:min(len(missing), 10)])
		}
	}
}
