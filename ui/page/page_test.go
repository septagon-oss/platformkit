package page_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/page"
)

func render(t *testing.T, n g.Node) string {
	t.Helper()
	var b strings.Builder
	if err := n.Render(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func chrome() page.Chrome {
	return page.Chrome{
		Brand: "PlatformKit", Assets: "/admin/assets", Stylesheet: ui.Compose(design.Default()),
		Scripts: []string{"htmx.min.js", "theme.js"}, SignIn: "/admin/login",
		Attrs: map[string]string{"data-grain": "pke-grain"},
	}
}

func TestDocumentCarriesChromeRequestAndView(t *testing.T) {
	t.Parallel()
	c := chrome()
	r := page.Request{Tenant: tenancy.Tenant{Name: "Acme"}, Inline: []g.Node{h.Script(g.Attr("nonce", "n1"), g.Raw("1"))}}
	v := page.View{Title: "Tasks", Head: []g.Node{h.Link(h.Rel("stylesheet"), h.Href("/x.css"))}}
	out := render(t, page.Document(c, r, v, h.Main(g.Text("body"))))
	for _, want := range []string{
		`<html lang="en" data-signin="/admin/login" data-grain="pke-grain">`,
		`<title>Tasks · Acme</title>`,
		`href="/admin/assets/app.css?v=` + c.Stylesheet.Fingerprint + `"`,
		`<script nonce="n1">1</script>`,
		`<script src="/admin/assets/js/htmx.min.js" defer></script><script src="/admin/assets/js/theme.js" defer></script>`,
		`href="/x.css"`,
		`<body><main>body</main></body>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("document lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "data-theme") {
		t.Fatal("a chrome that pins no theme wrote data-theme")
	}
}

func TestDocumentPinsATheme(t *testing.T) {
	t.Parallel()
	c := chrome()
	c.Theme, c.SignIn = "dark", ""
	out := render(t, page.Document(c, page.Request{}, page.View{Title: "Shop"}, h.Main()))
	if !strings.Contains(out, `<html lang="en" data-theme="dark" data-grain="pke-grain">`) {
		t.Fatalf("html element is wrong:\n%s", out)
	}
	if !strings.Contains(out, `<title>Shop · PlatformKit</title>`) {
		t.Fatal("the brand is not the fallback title")
	}
	if strings.Contains(out, "data-signin") {
		t.Fatal("a chrome with no sign-in wrote data-signin")
	}
}

func TestDocumentOffersRecoveryWithoutSerializingInputs(t *testing.T) {
	t.Parallel()
	c := chrome()
	c.Scripts = append(c.Scripts, "htmx-config.js")
	r := page.Request{SignedIn: true, Principal: tenancy.Principal{UserID: uuid.MustParse("bf81ba02-7ae1-468e-a908-842736ba7246")}}
	out := render(t, page.Document(c, r, page.View{}, h.Main()))
	for _, want := range []string{
		`data-principal="bf81ba02-7ae1-468e-a908-842736ba7246"`,
		`id="pk-auth-anonymous" hidden`, `id="pk-auth-denied" hidden`, `id="pk-auth-changed" hidden`,
		`role="alert"`, "Sign-in required", "Permission denied", "Account changed",
		`href="/admin/login" target="_blank" rel="noopener noreferrer"`, "Sign in (opens a new tab)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recovery document lacks %q", want)
		}
	}
	for _, signin := range []string{"", "https://elsewhere.test/login", "//elsewhere.test/login", `/\elsewhere.test/login`} {
		c.SignIn = signin
		out = render(t, page.Document(c, page.Request{}, page.View{}, h.Main()))
		if strings.Contains(out, "data-principal") || strings.Contains(out, "Sign in (opens a new tab)") {
			t.Errorf("anonymous page or unsafe sign-in %q exposed recovery identity/link", signin)
		}
	}
}

func TestFaultKeepsTheStatusAndTheWayBack(t *testing.T) {
	t.Parallel()
	v := page.Fault(http.StatusNotFound, "no such task", "/admin", "Back to the dashboard")
	if v.Status != http.StatusNotFound || v.Title != "Not Found" {
		t.Fatalf("view is %+v", v)
	}
	out := render(t, g.Group(v.Body))
	for _, want := range []string{"no such task", `href="/admin"`, "Back to the dashboard"} {
		if !strings.Contains(out, want) {
			t.Fatalf("fault lacks %q", want)
		}
	}
	if !strings.Contains(render(t, g.Group(page.Fault(422, "", "/", "Home").Body)), "That did not work.") {
		t.Fatal("an empty detail has no fallback sentence")
	}
}

func TestBareIsANarrowColumn(t *testing.T) {
	t.Parallel()
	out := render(t, page.Bare([]g.Node{g.Text("card")}))
	if !strings.Contains(out, "card") || !strings.Contains(out, "max-w-sm") {
		t.Fatalf("bare frame:\n%s", out)
	}
}

// yes allows everything but the one permission it is told to refuse.
type yes struct{ refuse string }

func (y yes) Allowed(_ context.Context, _ tenancy.Tenant, g tenancy.Grant) (bool, error) {
	return g.Permission != y.refuse, nil
}

func TestNavigationHidesUnservedOperatorAndRefusedEntries(t *testing.T) {
	t.Parallel()
	entries := []module.NavEntry{
		{Label: "Tasks", Path: "/admin/task/tasks", Permission: "task:read"},
		{Label: "Ghost", Path: "/admin/ghost", Permission: "ghost:read"},
		{Label: "Tenants", Path: "/admin/tenant/tenants", Permission: "tenant:manage"},
		{Label: "Plans", Path: "/admin/billing/plans", Permission: "billing:read"},
	}
	served := []string{"/admin/task/tasks", "/admin/tenant/tenants", "/admin/billing/plans"}
	required := []tenancy.Grant{{Permission: "tenant:manage", Operator: true}}
	nav := page.NewNavigation(entries, served, required)

	if got := nav.Unserved(); len(got) != 1 || got[0].Label != "Ghost" {
		t.Fatalf("unserved = %+v", got)
	}
	customer := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	got := nav.Visible(context.Background(), customer, yes{refuse: "billing:read"})
	if len(got) != 1 || got[0].Label != "Tasks" {
		t.Fatalf("a customer sees %+v; want Tasks only", got)
	}
	operator := tenancy.Tenant{ID: uuid.New(), Slug: "op", Operator: true}
	got = nav.Visible(context.Background(), operator, yes{})
	if len(got) != 3 {
		t.Fatalf("the operator sees %+v; want Tasks, Tenants, Plans", got)
	}
	// No tenant resolved: served is the only filter, and the operator's entry
	// is still hidden because a tenant that is not the operator's is every
	// tenant that is not — including none.
	got = nav.Visible(context.Background(), tenancy.Tenant{}, yes{refuse: "task:read"})
	if len(got) != 2 {
		t.Fatalf("with no tenant %+v; want Tasks and Plans", got)
	}
}

func TestServedIsEveryRecordedGET(t *testing.T) {
	t.Parallel()
	ops := []*huma.Operation{
		{Method: http.MethodGet, Path: "/admin"},
		{Method: http.MethodPost, Path: "/admin/x"},
		{Method: http.MethodGet, Path: "/api/v1/task/tasks"},
	}
	if got := page.Served(ops); len(got) != 2 || got[0] != "/admin" || got[1] != "/api/v1/task/tasks" {
		t.Fatalf("served = %v", got)
	}
}

func TestAViewPinsItsOwnThemeAndDropsTheThemeScript(t *testing.T) {
	c := chrome()
	r := page.Request{Inline: []g.Node{h.Script(g.Raw("stored theme"))}}
	out := render(t, page.Document(c, r, page.View{Title: "Home", Theme: "dark"}, h.Main()))
	if !strings.Contains(out, `data-theme="dark"`) {
		t.Fatal("the view's theme was not pinned on the document")
	}
	if strings.Contains(out, "stored theme") {
		t.Fatal("a pinned document carried the visitor's theme script")
	}
	out = render(t, page.Document(c, r, page.View{Title: "Home"}, h.Main()))
	if !strings.Contains(out, "stored theme") {
		t.Fatal("an unpinned document dropped the theme script")
	}
}
