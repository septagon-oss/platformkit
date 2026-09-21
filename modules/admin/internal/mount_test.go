package internal

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/page"
)

// allow is an Authorizer that answers one way for everybody.
type allow bool

func (a allow) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return bool(a), nil
}

type noTenants struct{}

func (noTenants) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return tenancy.Tenant{}, tenancy.ErrNoSuchHost
}

func render(t *testing.T, node g.Node) string {
	t.Helper()
	var b strings.Builder
	if err := node.Render(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestEveryHandWrittenPageDeclaresTheAuthorizationItsDataNeeds is the shell's
// own boundary, read from the kernel's recording rather than from a request:
// the sign-in form is public, the dashboard and health need a session, the
// tenant switcher is the operator's, and every gallery route — index, preview,
// export and each Storybook file — needs gallery:read before it selects anything.
func TestEveryHandWrittenPageDeclaresTheAuthorizationItsDataNeeds(t *testing.T) {
	_, conn := dbtest.Schema(t)
	api, _ := httpx.New(httpx.Options{
		PublicHost: "admin.test", Tenants: noTenants{}, Conn: conn, Authorize: allow(true),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	Mount(api, Shell{Authorize: allow(true), Theme: design.Default()})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("a page has no declaration: %v", err)
	}
	want := map[string]httpx.Auth{
		loginPath:                    httpx.Public(),
		adminRoot:                    httpx.SignedIn(),
		healthPath:                   httpx.SignedIn(),
		catalogPath:                  httpx.SignedIn(),
		tenantsPath:                  httpx.OperatorPermission(tenantcontracts.PermissionTenantManage),
		galleryPath:                  httpx.Permission("gallery:read"),
		galleryPath + "/preview":     httpx.Permission("gallery:read"),
		galleryPath + "/export":      httpx.Permission("gallery:read"),
		galleryPath + "/storybook/*": httpx.Permission("gallery:read"),
	}
	seen := map[string]bool{}
	for _, op := range api.Recorded() {
		expected, ok := want[op.Path]
		if !ok {
			continue
		}
		seen[op.Path] = true
		if got, _ := op.Extensions[httpx.AuthExtension].(httpx.Auth); got != expected {
			t.Errorf("%s %s declares %+v, want %+v", op.Method, op.Path, got, expected)
		}
	}
	for path := range want {
		if !seen[path] {
			t.Errorf("%s was not mounted", path)
		}
	}
}

// TestTheSidebarOffersTheGalleryOnlyToACallerWhoMayOpenIt: the link and the
// route agree because the frame asks the same authorizer and the same
// selector the route does, so a caller who would be refused sees no link.
func TestTheSidebarOffersTheGalleryOnlyToACallerWhoMayOpenIt(t *testing.T) {
	t.Parallel()
	book := func(context.Context) (export.Storybook, error) {
		return export.Storybook{Theme: design.Default()}, nil
	}
	denied := func(context.Context) (export.Storybook, error) {
		return export.Storybook{}, problem.New(http.StatusForbidden, "no storybook")
	}
	signedIn := page.Request{SignedIn: true, Principal: tenancy.Principal{UserID: uuid.New()}, Path: adminRoot}
	nav := page.NewNavigation(nil, nil, nil)
	for _, tc := range []struct {
		name      string
		authorize httpx.Authorizer
		storybook func(context.Context) (export.Storybook, error)
		r         page.Request
		offered   bool
	}{
		{"allowed with a composition", allow(true), book, signedIn, true},
		{"permission refused", allow(false), book, signedIn, false},
		{"no composition for this tenant", allow(true), denied, signedIn, false},
		{"anonymous", allow(true), book, page.Request{Path: loginPath}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := render(t, frame(nav, tc.authorize, tc.storybook)(context.Background(), tc.r, nil))
			if got := strings.Contains(body, `href="`+galleryPath+`"`); got != tc.offered {
				t.Fatalf("gallery link offered = %v, want %v:\n%s", got, tc.offered, body)
			}
			if !strings.Contains(body, `href="`+healthPath+`"`) || !strings.Contains(body, "data-confirm-accept") {
				t.Fatal("the frame lost its health entry or its confirm dialog")
			}
		})
	}
}

// TestGalleryExampleResolvesOnlyPublishedExamplesAndValidProps: the selector
// is the composition, never the query string, and a property patch is typed.
func TestGalleryExampleResolvesOnlyPublishedExamplesAndValidProps(t *testing.T) {
	t.Parallel()
	book := export.Storybook{Theme: design.Default(), Examples: []examples.Example{
		examples.ExampleOf(examples.ExampleInfo{ID: "product/button", ComponentID: "product.button", Group: "Actions", Name: "Button"},
			components.ButtonProps{Label: "Buy"}, components.Button),
		examples.ExampleOf(examples.ExampleInfo{ID: "product/badge", ComponentID: "product.badge", Group: "Status", Name: "Badge"},
			components.BadgeProps{Label: "New"}, components.Badge),
	}}
	status := func(err error) int {
		var p *problem.Problem
		if errors.As(err, &p) {
			return p.Status
		}
		return 0
	}
	if _, err := galleryExample(book, &galleryInput{Example: "pk-ui.component.button/primary"}); status(err) != http.StatusNotFound {
		t.Fatalf("an example outside the composition = %v, want 404", err)
	}
	first, err := galleryExample(book, &galleryInput{Group: "Status"})
	if err != nil || first.ID != "product/badge" {
		t.Fatalf("the first example of a group = %q, %v", first.ID, err)
	}
	edited, err := galleryExample(book, &galleryInput{Example: "product/button", Props: `{"label":"Buy now"}`})
	if err != nil || !strings.Contains(render(t, edited.Node), "Buy now") {
		t.Fatalf("a typed patch was not applied: %v", err)
	}
	if _, err := galleryExample(book, &galleryInput{Example: "product/button", Props: `{"onclick":"alert(1)"}`}); status(err) != http.StatusUnprocessableEntity {
		t.Fatalf("an unknown property = %v, want 422", err)
	}
	if _, err := galleryExample(book, &galleryInput{Example: "product/button", Props: `{"label":`}); status(err) != http.StatusUnprocessableEntity {
		t.Fatalf("malformed JSON = %v, want 422", err)
	}
}

// TestThePreviewDocumentIsSandboxedAndCarriesItsOwnStylesheet: the direct URL
// must be as isolated as the framed one, and the palette the person chose is
// pinned on the document rather than left to their stored preference.
func TestThePreviewDocumentIsSandboxedAndCarriesItsOwnStylesheet(t *testing.T) {
	t.Parallel()
	example := examples.ExampleOf(examples.ExampleInfo{ID: "product/button", ComponentID: "product.button", Name: "Button"},
		components.ButtonProps{Label: "Buy"}, components.Button)
	out, err := galleryPreview(export.Storybook{Theme: design.Default()}, example, "dark")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.ContentSecurityPolicy, "sandbox allow-scripts;") || !strings.Contains(out.ContentSecurityPolicy, "form-action 'none'") {
		t.Errorf("preview policy = %q", out.ContentSecurityPolicy)
	}
	if out.FrameOptions != "SAMEORIGIN" || out.CacheControl != "no-store" || out.ContentType != httpx.HTMLContentType {
		t.Errorf("preview headers = %+v", out)
	}
	body := string(out.Body)
	for _, want := range []string{"<!doctype html>", `<html lang="en" data-theme="dark">`, "--pk-color-surface-canvas:", ">Buy<", assetPrefix + "/js/gallery-preview.js"} {
		if !strings.Contains(body, want) {
			t.Errorf("preview lacks %q", want)
		}
	}
	system, _ := galleryPreview(export.Storybook{Theme: design.Default()}, example, "system")
	if strings.Contains(string(system.Body), `<html lang="en" data-theme=`) {
		t.Error("a system preview pinned a theme")
	}
}

// TestTheHealthPageNamesEachCheckAndItsState is what somebody looking at a
// broken deployment reads: the probe's checks, one row each, with the state
// as a badge rather than a colour alone.
func TestTheHealthPageNamesEachCheckAndItsState(t *testing.T) {
	t.Parallel()
	body := render(t, g.Group(healthPage([]result{{name: "database", err: nil}, {name: "cache", err: errors.New("refused")}}).Body))
	for _, want := range []string{">database<", ">cache<", ">ok<", ">failing<", `data-tone="success"`, `data-tone="danger"`} {
		if !strings.Contains(body, want) {
			t.Errorf("health page lacks %q:\n%s", want, body)
		}
	}
	if !strings.Contains(render(t, g.Group(healthPage(nil).Body)), "Health") {
		t.Error("an unreachable database renders no health page at all")
	}
}

// TestTheSignInPageSpeaksTheRequestLanguage: the composed catalog's words,
// and the authored English when none is composed.
func TestTheSignInPageSpeaksTheRequestLanguage(t *testing.T) {
	t.Parallel()
	english := render(t, g.Group(login(context.Background(), nil).Body))
	if !strings.Contains(english, ">Sign in<") || !strings.Contains(english, `data-next="`+adminRoot+`"`) {
		t.Fatalf("english sign-in:\n%s", english)
	}
	pt := &page.Locale{Language: "pt-PT", Formatter: words{"admin.login.title": "Iniciar sessão", "admin.login.password": "Palavra-passe"}}
	view := login(context.Background(), pt)
	body := render(t, g.Group(view.Body))
	if view.Title != "Iniciar sessão" || !strings.Contains(body, "Palavra-passe") || !strings.Contains(body, ">Email<") {
		t.Fatalf("localized sign-in lost a translation or its fallback:\n%s", body)
	}
	if raw, _ := json.Marshal(view.Title); string(raw) != `"Iniciar sessão"` {
		t.Error("the title is not the catalog's text")
	}
}

type words map[string]string

func (w words) Text(key, fallback string, _ ...any) string {
	if text, ok := w[key]; ok {
		return text
	}
	return fallback
}

// TestTheSignInFormIsNotOfferedAtATenantlessHost is the other half of the same
// trap. The page reads no tenant, so it is Public and renders happily at an
// address nobody serves a site at — and the form it renders posts to a route
// that cannot answer there. Somebody who opens the deployment by IP meets a
// working sign-in page that cannot sign anybody in, which reads as an
// application with a broken password rather than as a mistyped address.
//
// Every other page at such a host is a 404; this one has to agree.
func TestTheSignInFormIsNotOfferedAtATenantlessHost(t *testing.T) {
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: "admin.test", Tenants: noTenants{}, Conn: conn, Authorize: allow(true),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	Mount(api, Shell{Authorize: allow(true), Theme: design.Default()})

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080"+loginPath, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("%s at a host that serves no site = %d, want 404", loginPath, w.Code)
	}
	if strings.Contains(w.Body.String(), `type="password"`) {
		t.Error("a form that no route can accept was rendered; the page offered a sign-in there is none to have")
	}
}
