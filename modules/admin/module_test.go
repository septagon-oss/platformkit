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

	"github.com/septagon-oss/platformkit/design"
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
	Title  string     `json:"title" validate:"required" doc:"What this note is about"`
	Body   string     `json:"body,omitempty" gorm:"type:text" ui:"hide:list"`
	Status string     `json:"status" enum:"open,done" ui:"widget:select" required:"false" default:"open"`
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
func mount(t *testing.T) chi.Router { return mountAs(t, caller{}) }

// mountAs is mount for a caller who holds less, which is what the guards on the
// generated screens and on the dashboard's cards are tested with.
func mountAs(t *testing.T, authorize httpx.Authorizer) chi.Router {
	t.Helper()
	adminDB, app := dbtest.Schema(t)
	if _, err := adminDB.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("create notes: %v", err)
	}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: app, Authorize: authorize,
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
	shell := admin.Module(admin.Deps{Modules: []module.Module{notes}, Authorize: authorize})
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
		"an enum renders a select":               `<select`,
		"a text column renders a textarea":       `<textarea`,
		"a bool renders a checkbox":              `type="checkbox"`,
		"a required field says so":               `required`,
		"an entity-picker says there is none":    "There is no picker for it yet",
		"a list field says how to write one":     "Comma separated",
		"the server owns the id and timestamps":  `name="title"`,
		"a field's own doc is the note under it": "What this note is about",
		"a declared default is preselected":      `<option value="open" selected>`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("%s: %q is not in the form", what, want)
		}
	}
	// A field a command of its own owns cannot have a value on a row that does
	// not exist yet, so the create form does not render it at all. See the edit
	// form below, where it is shown and read-only.
	if strings.Contains(form, `name="rank"`) || strings.Contains(form, "Changed by a command of its own") {
		t.Error("the create form offers a field only a command can set")
	}
	// A select whose field declares a default has no unchosen state to name.
	if strings.Contains(form, "Choose a status") {
		t.Error("a select with a default still offers a placeholder that cannot happen")
	}
	if strings.Contains(form, `name="createdAt"`) {
		t.Error("the form offers a field the server owns")
	}

	// The one door a command's field is otherwise writable through. The form
	// does not render it, so a value for it did not come from the form.
	code, refused, _ := call(t, router, http.MethodPost, "/admin/notes/notes", "title=Hand+rolled&rank=9")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("a create carrying an immutable field = %d, want 422", code)
	}
	if !strings.Contains(refused, "rank belongs to a route of its own") {
		t.Errorf("the refusal does not name the field: %s", refused[:min(len(refused), 400)])
	}
	if _, list, _ := call(t, router, http.MethodGet, "/admin/notes/notes", ""); strings.Contains(list, "Hand rolled") {
		t.Error("a create carrying an immutable field stored the row anyway")
	}

	// A refusal is a 422 that renders the form again with the message on it,
	// which is what htmx swaps in place and what a browser without it shows.
	code, refused, _ = call(t, router, http.MethodPost, "/admin/notes/notes", "title=&status=open")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("an empty title = %d, want 422", code)
	}
	if !strings.Contains(refused, "a note needs a title") && !strings.Contains(refused, "title") {
		t.Errorf("the refusal says nothing about the field: %s", refused)
	}

	code, body, location := call(t, router, http.MethodPost, "/admin/notes/notes",
		"title=First+note&status=done&pinned=on&body=Some+prose&tags=a,+b")
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
	// nothing in it gets. An enum reads as a person writes it and a boolean as
	// Yes or No, here and in a cell and in a select's options alike.
	for _, want := range []string{"First note", "Some prose", "Done", "Yes", "—"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the detail does not show %q", want)
		}
	}
	if strings.Contains(detail, ">done<") || strings.Contains(detail, ">true<") {
		t.Error("the detail shows an enum or a boolean as the wire spells it")
	}
	// The tab, the bookmark and the history entry say which row this is.
	if !strings.Contains(detail, "<title>First note · Acme</title>") {
		t.Error("the detail page is titled after the entity rather than the row")
	}

	// The edit form is where a command's field is shown, read-only, so a person
	// can see it and be told which door changes it.
	_, editForm, _ := call(t, router, http.MethodGet, location+"/edit", "")
	for what, want := range map[string]string{
		"an immutable field is shown": `name="rank"`,
		"and says who owns it":        "Changed by a command of its own",
		"an int renders a number":     `type="number"`,
	} {
		if !strings.Contains(editForm, want) {
			t.Errorf("%s: %q is not in the edit form", what, want)
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
	// A prefixed utility is one escaped colon inside the class name and one
	// unescaped colon before the pseudo-class, so the escaped ones are put
	// aside before the pseudo is stripped and put back afterwards.
	pseudo := regexp.MustCompile(`(?::+[a-z-]+(?:\(.*?\))?)+$`)
	ruled := func(sheets ...[]byte) map[string]bool {
		rules := map[string]bool{}
		for _, sheet := range sheets {
			for _, m := range regexp.MustCompile(`(?m)^\s*\.((?:[^{,\s]|\\.)+)`).FindAllStringSubmatch(string(sheet), -1) {
				name := pseudo.ReplaceAllString(strings.ReplaceAll(m[1], `\:`, "\x00"), "")
				rules[strings.ReplaceAll(strings.ReplaceAll(name, "\x00", ":"), `\`, "")] = true
			}
		}
		return rules
	}
	app := ruled(ui.Stylesheet(design.Default()))
	if len(app) < 100 {
		t.Fatalf("the stylesheet has %d rules; the parser above is wrong", len(app))
	}
	// The gallery is the one page that links a second sheet, so it is the one
	// page checked against both. Every other page is checked against app.css
	// alone, which is what makes the split honest: a shell class that moved
	// into gallery.css by mistake fails here.
	both := ruled(ui.Stylesheet(design.Default()), ui.GalleryStylesheet())
	for path, rules := range map[string]map[string]bool{
		"/admin": app, "/admin/login": app, "/admin/health": app,
		"/admin/notes/notes": app, "/admin/notes/notes/new": app,
		"/admin/_gallery": both,
	} {
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
	// And the second sheet is linked exactly where its classes are rendered.
	_, gallery, _ := call(t, router, http.MethodGet, "/admin/_gallery", "")
	if !strings.Contains(gallery, "/admin/assets/gallery.css?v="+ui.GalleryFingerprint()) {
		t.Error("the gallery does not link the sheet its own components need")
	}
	_, dashboard, _ := call(t, router, http.MethodGet, "/admin", "")
	if strings.Contains(dashboard, "gallery.css") {
		t.Error("an ordinary page downloads the gallery's stylesheet")
	}
}

// member is an Authorizer that answers yes to exactly the permissions it holds,
// which is what a role is.
type member map[string]bool

func (member) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return acme, nil
}

func (m member) Allowed(_ context.Context, _ tenancy.Tenant, want tenancy.Grant) (bool, error) {
	return m[want.Permission], nil
}

// TestTheDashboardCountsOnlyWhatTheCallerMayRead is the E4 review's critical
// finding on the page it was found on. The dashboard holds every resource and
// calls List on each, and a card saying "— Notes" for an entity a person may
// not look at still tells them it is there. The kernel refuses the list — see
// httpx.RegisterResource — and the card is not drawn at all.
func TestTheDashboardCountsOnlyWhatTheCallerMayRead(t *testing.T) {
	// Signed in, and holding nothing: the dashboard is a page about what this
	// caller may see, and this one may see none of it.
	code, body, _ := call(t, mountAs(t, member{}), http.MethodGet, "/admin", "")
	if code != http.StatusOK {
		t.Fatalf("the dashboard = %d %s", code, body)
	}
	// "In notes" is the card's own description, which nothing else on the page
	// renders — the sidebar's own filter is a separate claim, tested above.
	if strings.Contains(body, "In notes") {
		t.Error("the dashboard shows a card for an entity the caller may not read")
	}

	// The same page for a caller who may: the card is there, with the count its
	// own list route would report.
	_, body, _ = call(t, mountAs(t, member{"note:read": true}), http.MethodGet, "/admin", "")
	if !strings.Contains(body, "In notes") || !strings.Contains(body, "0 Notes") {
		t.Errorf("the dashboard hides a card the caller may read: %s", body)
	}
}

// TestASortHeaderIsALink is the E4 review's finding about sorting: the header
// was a button, so a column could only be reordered by a browser running
// JavaScript, and the order could not be linked to, bookmarked or opened in a
// tab. It is an anchor with the URL the server answers, and the hx-get beside
// it is an enhancement on top of that.
func TestASortHeaderIsALink(t *testing.T) {
	router := mount(t)
	_, body, _ := call(t, router, http.MethodGet, "/admin/notes/notes", "")
	head := body[strings.Index(body, "<thead"):strings.Index(body, "</thead>")]
	for _, want := range []string{
		`<a `, `href="/admin/notes/notes?sort=title"`, `href="/admin/notes/notes?sort=status"`,
	} {
		if !strings.Contains(head, want) {
			t.Errorf("the table head has no %s: %s", want, head)
		}
	}
	if strings.Contains(head, "<button") {
		t.Error("a server-sorted column is still a button")
	}
	// The same header, sorted, offers the other direction.
	_, body, _ = call(t, router, http.MethodGet, "/admin/notes/notes?sort=title", "")
	head = body[strings.Index(body, "<thead"):strings.Index(body, "</thead>")]
	if !strings.Contains(head, `href="/admin/notes/notes?sort=-title"`) || !strings.Contains(head, `aria-sort="ascending"`) {
		t.Errorf("a sorted column does not offer the other direction: %s", head)
	}
}

// TestAPageWithNoStoredChoiceFollowsTheOperatingSystem is the E4 review's
// theme finding. Every page but the sign-in one carried data-theme="light",
// which is the one value that defeats the dark rules: they are behind
// prefers-color-scheme, qualified by :root:not([data-theme]). The attribute is
// the person's own choice, written by the inline snippet from what they last
// picked, and there is nothing to write until they pick.
func TestAPageWithNoStoredChoiceFollowsTheOperatingSystem(t *testing.T) {
	router := mount(t)
	for _, path := range []string{"/admin", "/admin/login", "/admin/health", "/admin/notes/notes"} {
		_, body, _ := call(t, router, http.MethodGet, path, "")
		if strings.Contains(body, "data-theme=") {
			t.Errorf("%s pins a theme rather than letting the operating system say", path)
		}
		// The snippet that applies a stored choice is still there: without it
		// a chosen dark theme flashes white on every load.
		if !strings.Contains(body, `localStorage.getItem("platformkit-theme")`) {
			t.Errorf("%s lost the snippet that restores a chosen theme", path)
		}
	}
}

// visit is a request nobody made: no session cookie, so the kernel's identity
// hook is never asked and the caller is anonymous. accept is what a browser
// asks for and what a program does not.
func visit(t *testing.T, r http.Handler, path, accept string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
	req.Header.Set("Accept", accept)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Header().Get("Location")
}

// TestAnAnonymousBrowserIsSentToTheSignInForm is the E4 review's minor finding
// about the way in: following a bookmark into the shell while signed out
// answered a JSON document about one's own anonymity. It is a 303 to the form,
// carrying where the person was going; the JSON routes beside it are unchanged,
// because a program that gets a redirect where it expected a 403 has to guess.
func TestAnAnonymousBrowserIsSentToTheSignInForm(t *testing.T) {
	router := mount(t)
	const browser = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

	for path, want := range map[string]string{
		"/admin":                 "/admin/login?next=%2Fadmin",
		"/admin/notes/notes":     "/admin/login?next=%2Fadmin%2Fnotes%2Fnotes",
		"/admin/notes/notes/new": "/admin/login?next=%2Fadmin%2Fnotes%2Fnotes%2Fnew",
		"/admin/health":          "/admin/login?next=%2Fadmin%2Fhealth",
	} {
		code, location := visit(t, router, path, browser)
		if code != http.StatusSeeOther || location != want {
			t.Errorf("anonymous GET %s = %d to %q, want 303 to %q", path, code, location, want)
		}
	}
	// The form itself is public, and does not redirect to itself.
	if code, _ := visit(t, router, "/admin/login", browser); code != http.StatusOK {
		t.Errorf("the sign-in form = %d", code)
	}
	// A program asking for JSON keeps the problem document.
	if code, location := visit(t, router, "/api/v1/notes/notes", "application/json"); code != http.StatusForbidden || location != "" {
		t.Errorf("an anonymous API call = %d to %q, want 403", code, location)
	}
	// And so does a program asking for a page's URL without asking for a page.
	if code, location := visit(t, router, "/admin", "application/json"); code != http.StatusForbidden || location != "" {
		t.Errorf("an anonymous JSON call to a page = %d to %q, want 403", code, location)
	}
}

// TestANavEntryNoRouteServesIsABootWarning is the E4 review's minor finding
// about disabled links: a module that declares a nav entry and mounts nothing
// to answer it is a wiring mistake, and rendering it greyed out showed that
// mistake to every person using the application rather than to whoever could
// fix it.
func TestANavEntryNoRouteServesIsABootWarning(t *testing.T) {
	var log strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	// This caller holds every permission the composition defines, so the
	// permission filter cannot be what hides the entry: only the served one
	// can. See TestTheSidebarShowsWhatTheCallerMayReach for the other half.
	router := mountAs(t, member{"note:read": true, "note:write": true, "secret:read": true})
	if !strings.Contains(log.String(), "/admin/notes/secrets") {
		t.Errorf("boot said nothing about a nav entry no route serves: %s", log.String())
	}
	if strings.Contains(log.String(), "/admin/notes/notes") {
		t.Errorf("boot complained about a nav entry that is served: %s", log.String())
	}

	_, body, _ := call(t, router, http.MethodGet, "/admin", "")
	if strings.Contains(body, "/admin/notes/secrets") {
		t.Error("the sidebar renders a link no route answers")
	}
	if strings.Contains(body, `aria-disabled="true"`) {
		t.Error("the sidebar still renders a disabled entry")
	}
}
