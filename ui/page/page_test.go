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

// yes allows everything but the one permission it is told to refuse.
type yes struct{ refuse string }

func (y yes) Allowed(_ context.Context, _ tenancy.Tenant, g tenancy.Grant) (bool, error) {
	return g.Permission != y.refuse, nil
}

func TestNavigationHidesUnservedOperatorAndRefusedEntries(t *testing.T) {
	t.Parallel()
	entries := []module.NavEntry{
		{Label: "Tasks", Screen: "task/tasks", Permission: "task:read"},
		{Label: "Ghost", Screen: "ghost/ghosts", Permission: "ghost:read"},
		{Label: "Tenants", Screen: "tenant/tenants", Permission: "tenant:manage"},
		{Label: "Plans", Screen: "billing/plans", Permission: "billing:read"},
	}
	// A manifest names a screen relative to the workspace; the href a person
	// follows is the kernel's answer, and `served` is what the router recorded.
	served := []string{"/app/task/tasks", "/app/tenant/tenants", "/app/billing/plans"}
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
