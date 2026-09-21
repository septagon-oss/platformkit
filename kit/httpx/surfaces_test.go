package httpx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The three surfaces: what an address is, what it answers, and what it may never
// do. kit/httpx/README.md carries the prefix table; the tests here are the
// reason it is true rather than merely written down.

// installationHost is where this fixture's installation is reached. Which host
// that is belongs to the deployment — kit/config reads it, kit/app passes it —
// and nothing in a module knows it. The installation's tenant is the operator's,
// and that is the second fact the control plane is gated on.
const installationHost = "platformkit.example"

// surfaces is this file's fixture: one installation, one host per tenant, and an
// authorizer that says yes to everything so that what a case observes is the
// surface's own decision and not a role's.
type surfaces struct {
	api    *httpx.API
	router http.Handler
	// who is the tenant the host resolved to, which a case sets before it asks.
	who tenancy.Tenant
	// asked counts the authorizer's answers, which is how a case says "nobody was
	// asked anything" rather than trusting a status code alone.
	asked int
}

func newSurfaces(t *testing.T) *surfaces {
	t.Helper()
	return newSurfacesWith(t, httpx.Options{})
}

func newSurfacesWith(t *testing.T, extra httpx.Options) *surfaces {
	t.Helper()
	admin, app := dbtest.Schema(t)
	_ = admin
	s := &surfaces{who: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}}
	opts := httpx.Options{
		PublicHost:   host,
		Installation: installationHost,
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
			if h == host || h == installationHost {
				return s.who, nil
			}
			return tenancy.Tenant{}, tenancy.ErrNoSuchHost
		}),
		Conn: app,
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			s.asked++
			return true, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log:          slog.New(slog.DiscardHandler),
		WriteLimiter: extra.WriteLimiter,
	}
	api, router := httpx.New(opts)
	s.api, s.router = api, router
	return s
}

// setWho changes what the hosts resolve to, and tells the kernel about it.
func (s *surfaces) setWho(t tenancy.Tenant) {
	s.who = t
	s.api.InvalidateHost(host)
	s.api.InvalidateHost(installationHost)
}

type loaderFunc func(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error)

func (f loaderFunc) ByHost(ctx context.Context, tx db.Tx[db.System], h string) (tenancy.Tenant, error) {
	return f(ctx, tx, h)
}

type authorizerFunc func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error)

func (f authorizerFunc) Allowed(ctx context.Context, t tenancy.Tenant, g tenancy.Grant) (bool, error) {
	return f(ctx, t, g)
}

// window is a limit.Limiter that lets a fixed number of events through, so a case
// names the limit instead of waiting out a real window.
type window struct {
	allow int
	seen  int
}

func (w *window) Allow(context.Context, string, int, time.Duration) (bool, time.Duration, error) {
	w.seen++
	if w.seen > w.allow {
		return false, time.Minute, nil
	}
	return true, 0, nil
}

func (w *window) Count(context.Context, string, time.Duration) (int, time.Duration, error) {
	return w.seen, 0, nil
}

func (w *window) Forget(context.Context, string) error { return nil }

func dial(t *testing.T, h http.Handler, method, authority, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+authority+path, nil)
	req.Header.Set("Accept", "application/json")
	// A session cookie is what makes a request somebody's: the fixture's identity
	// hook answers any request that presents one, which is the shape of the real
	// one. Without it every case below would be a test of the anonymous chain.
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestThePrefixTable is the composition in one table: a module names a resource,
// and the surface decides the address. A module writes neither prefix, and a
// relative path that repeats the prefix it was given is refused.
func TestThePrefixTable(t *testing.T) {
	s := newSurfaces(t)
	billing := s.api.Surfaces("billing")

	for _, g := range []struct{ what, got, want string }{
		{"the workspace's JSON", billing.App.Path("/subscriptions"), "/api/v1/billing/subscriptions"},
		{"the workspace's document", billing.App.PagePath("/subscriptions"), "/app/billing/subscriptions"},
		{"the public door", billing.Public.Path("/brochure"), "/api/v1/public/billing/brochure"},
		{"the control plane", billing.Ops.Path("/subscriptions"), "/api/v1/ops/billing/subscriptions"},
		{"the workspace namespace", billing.App.Prefix(), "/api/v1/billing"},
		{"the control-plane namespace", billing.Ops.Prefix(), "/api/v1/ops/billing"},
		// The kernel's own routes: no module composes them, because the address
		// belongs to the composition rather than to a capability.
		{"the composition's JSON", s.api.Surfaces("").App.Path("/resources"), "/api/v1/app/resources"},
		{"the composition's document", s.api.Surfaces("").App.PagePath(""), "/app"},
	} {
		if g.got != g.want {
			t.Errorf("%s = %q, want %q", g.what, g.got, g.want)
		}
	}

	// A module that names its own namespace, or anybody's prefix, in a path is
	// composing the same address twice — which is how /api/v1/tasks/tasks
	// happened once, and why the gate reads the composition rather than a regex.
	for _, rel := range []string{"/billing/subscriptions", "/api/v1/subscriptions", "/app/subscriptions", "/admin/subscriptions"} {
		s.api.Surfaces("billing").App.Path(rel)
	}
	err := s.api.ValidateDeclarations()
	if err == nil || !strings.Contains(err.Error(), "names a prefix") {
		t.Errorf("a relative path that names a prefix is not refused: %v", err)
	}
	// And the refusal is not an opinion about the *shape* of a good path: the
	// plain relative form composes silently.
	if err := newSurfaces(t).api.ValidateDeclarations(); err != nil {
		t.Errorf("an untouched API is refused: %v", err)
	}
}

// TestTheControlPlaneIsNotFoundAtATenantHost is the control plane at the only
// layer that can answer for it: the same route, the same authorizer that says
// yes, the same caller. At the installation's host it is served; at a customer's
// host the answer is the one a never-mounted address gives, and nobody is asked
// anything.
func TestTheControlPlaneIsNotFoundAtATenantHost(t *testing.T) {
	s := newSurfaces(t)
	httpx.Register(s.api.Surfaces("billing").Ops, huma.Operation{
		OperationID: "reconcile", Method: http.MethodGet, Path: "/reconciliations",
	}, httpx.OperatorPermission("billing:reconcile"), ok)
	httpx.Register(s.api.Surfaces("billing").App, huma.Operation{
		OperationID: "read-subscription", Method: http.MethodGet, Path: "/subscriptions",
	}, httpx.Permission("billing:read"), ok)
	if err := s.api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	const at = "/api/v1/ops/billing/reconciliations"

	s.asked = 0
	customer := dial(t, s.router, http.MethodGet, host, at)
	if customer.Code != http.StatusNotFound {
		t.Errorf("the control plane at a tenant host = %d %s, want 404", customer.Code, customer.Body.String())
	}
	if s.asked != 0 {
		t.Errorf("a host that serves no control plane asked the authorizer %d times", s.asked)
	}

	// The same answer, byte for byte, that an address nobody mounted gives at the
	// same host: the 404 must not disclose that the surface exists. Only the
	// request's own identity differs, so that is what is taken out before the
	// comparison.
	never := dial(t, s.router, http.MethodGet, host, "/api/v1/ops/billing/never-mounted")
	if a, b := fold(customer), fold(never); a != b {
		t.Errorf("the control plane answers %s where a missing address answers %s", a, b)
	}

	// The installation's own host, and the installation's own tenant: served.
	// setWho also drops the host's resolution from the kernel's cache, which is
	// what a deployment does through the tenant module when a host is moved; a
	// test that changed the row underneath a cached answer would be testing the
	// cache and not the gate.
	s.setWho(tenancy.Tenant{ID: uuid.New(), Slug: "installation", Name: "Installation", Operator: true})
	if got := dial(t, s.router, http.MethodGet, installationHost, at); got.Code != http.StatusOK {
		t.Errorf("the control plane at the installation host = %d %s, want 200", got.Code, got.Body.String())
	}

	// The installation's host pointing at a tenant that is not the installation's
	// — one row of data written the wrong way round. There is no address there
	// either, and the authorizer is still never asked: the tenant flag decides it
	// before the roles table exists to answer.
	s.setWho(tenancy.Tenant{ID: uuid.New(), Slug: "wrong", Name: "Wrong"})
	s.asked = 0
	if got := dial(t, s.router, http.MethodGet, installationHost, at); got.Code != http.StatusNotFound {
		t.Errorf("the control plane for a tenant that is not the installation's = %d %s, want 404", got.Code, got.Body.String())
	}
	if s.asked != 0 {
		t.Errorf("a control-plane route refused on the tenant flag asked the authorizer %d times", s.asked)
	}

	// None of this reaches the workspace: the module's own door at a tenant host
	// is untouched by where the installation lives.
	if got := dial(t, s.router, http.MethodGet, host, "/api/v1/billing/subscriptions"); got.Code != http.StatusOK {
		t.Errorf("the workspace door at a tenant host = %d %s, want 200", got.Code, got.Body.String())
	}
}

// fold is a response with its own request identity removed, so two answers can be
// compared for being the same answer.
func fold(w *httptest.ResponseRecorder) string {
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		return strings.TrimSpace(w.Body.String())
	}
	delete(doc, "instance")
	blob, err := json.Marshal(doc)
	if err != nil {
		return err.Error()
	}
	return w.Result().Status + " " + w.Header().Get("Content-Type") + " " + string(blob)
}

// TestARouteContradictingItsSurfaceIsRefusedAtMount is the mount gate: the
// contradictions a manifest can be written with, each named in the report that
// stops the process from listening.
func TestARouteContradictingItsSurfaceIsRefusedAtMount(t *testing.T) {
	page := func(context.Context, *struct{}) (*httpx.Page, error) {
		return &httpx.Page{Status: http.StatusOK, ContentType: httpx.HTMLContentType, Body: []byte("<!doctype html>")}, nil
	}
	for _, tc := range []struct {
		name    string
		mount   func(s *surfaces)
		inFault string
	}{
		{
			name: "a signed-in route on the public surface",
			mount: func(s *surfaces) {
				httpx.Register(s.api.Surfaces("billing").Public, huma.Operation{
					OperationID: "signed-in-on-public", Method: http.MethodGet, Path: "/mine",
				}, httpx.SignedIn(), ok)
			},
			inFault: "signed_in",
		},
		{
			name: "a permission on the public surface",
			mount: func(s *surfaces) {
				httpx.Register(s.api.Surfaces("billing").Public, huma.Operation{
					OperationID: "permission-on-public", Method: http.MethodGet, Path: "/manage",
				}, httpx.Permission("billing:manage"), ok)
			},
			inFault: "billing:manage",
		},
		{
			name: "an operator permission on the public surface",
			mount: func(s *surfaces) {
				httpx.Register(s.api.Surfaces("billing").Public, huma.Operation{
					OperationID: "operator-on-public", Method: http.MethodGet, Path: "/everybody",
				}, httpx.OperatorPermission("billing:reconcile"), ok)
			},
			inFault: "Ops",
		},
		{
			name: "a page on the control plane",
			mount: func(s *surfaces) {
				httpx.HTML(s.api.Surfaces("billing").Ops, huma.Operation{
					OperationID: "page-on-ops", Method: http.MethodGet, Path: "/reconciliations",
				}, httpx.OperatorPermission("billing:reconcile"), page)
			},
			inFault: "is a page on the Ops router",
		},
		{
			name: "the home of a surface that has none",
			mount: func(s *surfaces) {
				if _, took := s.api.Surfaces("billing").Ops.Home(); took {
					t.Error("the control plane handed out a home page")
				}
			},
			inFault: "has no home",
		},
		{
			name: "a page path that names the surface it is on",
			mount: func(s *surfaces) {
				httpx.HTML(s.api.Surfaces("billing").App, huma.Operation{
					OperationID: "page-naming-app", Method: http.MethodGet, Path: "/app/subscriptions",
				}, httpx.Permission("billing:read"), page)
			},
			inFault: "names a prefix",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSurfaces(t)
			tc.mount(s)
			err := s.api.ValidateDeclarations()
			if err == nil {
				t.Fatal("the contradiction passed the gate that exists to stop it")
			}
			if !strings.Contains(err.Error(), tc.inFault) {
				t.Errorf("the refusal does not name %q: %v", tc.inFault, err)
			}
		})
	}
}

// TestTheWorkspaceHomeHasOneOwner: the root of a surface is one address and can
// answer one way. Composition order decides who gets it, and the second claimant
// is told rather than silently overwritten.
func TestTheWorkspaceHomeHasOneOwner(t *testing.T) {
	s := newSurfaces(t)
	home, took := s.api.Surfaces("site").App.Home()
	if !took {
		t.Error("the first module in composition order did not get the workspace root")
	}
	if home.Prefix() != "/api/v1/site" {
		t.Errorf("a home router composes JSON at %q; the claim is about documents, not about who owns the API", home.Prefix())
	}
	if _, took := s.api.Surfaces("content").App.Home(); took {
		t.Error("two modules both got the workspace root")
	}
	if _, took := s.api.Surfaces("site").Public.Home(); !took {
		t.Error("the public surface has no root to claim, and a site has to answer its visitors at one")
	}
	httpx.HTML(home, huma.Operation{
		OperationID: "home", Method: http.MethodGet, Path: "/",
	}, httpx.Public(), func(context.Context, *struct{}) (*httpx.Page, error) {
		return &httpx.Page{Status: http.StatusOK, ContentType: httpx.HTMLContentType, Body: []byte("<!doctype html><h1>Home</h1>")}, nil
	})
	if err := s.api.ValidateDeclarations(); err != nil {
		t.Fatalf("the home page is refused: %v", err)
	}
	if got := dial(t, s.router, http.MethodGet, host, "/app"); !strings.Contains(got.Body.String(), "Home") {
		t.Errorf("the workspace root answered %d %s", got.Code, got.Body.String())
	}
}

// TestTheAliasRedirectsAndNeverServes is one release for the moved doors: the old
// address answers with a redirect and no body, the target is served by whatever
// the composition mounted there, and a second mount at the old address is refused.
func TestTheAliasRedirectsAndNeverServes(t *testing.T) {
	s := newSurfaces(t)
	for _, m := range []struct{ from, to string }{
		{"/admin", "/app"},
		{"/admin/tenants", "/app/tenants"},
		// The pages the admin module owns are not screens. The old root was both
		// the shell's address and that module's namespace, so the general row
		// alone would aim a bookmark of the sign-in page at a screen called login.
		{"/admin/login", "/app/admin/login"},
		{"/admin/health", "/app/admin/health"},
		{"/admin/assets/app.css", "/app/admin/assets/app.css"},
		{"/admin/_gallery", "/app/admin/_gallery"},
		{"/api/v1/admin/resources", "/api/v1/app/resources"},
		{"/api/v1/content/public", "/api/v1/public/content/contents"},
		{"/api/v1/file/public", "/api/v1/public/file/files"},
		{"/api/v1/site/settings/public", "/api/v1/public/site/settings"},
		{"/api/v1/auth/register", "/api/v1/public/auth/register"},
	} {
		got := dial(t, s.router, http.MethodGet, host, m.from)
		switch {
		case got.Code != http.StatusFound:
			t.Errorf("GET %s = %d, want the redirect one release gives", m.from, got.Code)
		case got.Header().Get("Location") != m.to:
			t.Errorf("GET %s redirects to %q, want %q", m.from, got.Header().Get("Location"), m.to)
		case got.Header().Get("Cache-Control") != "no-store":
			t.Errorf("GET %s says %q about caching; a redirect a browser remembers outlives the release that removes it",
				m.from, got.Header().Get("Cache-Control"))
		case got.Body.Len() != 0:
			t.Errorf("GET %s served %q; an alias is a redirect and not a second mount", m.from, got.Body.String())
		}
	}

	// A write keeps its method and its body rather than becoming a GET, and a
	// query survives: an old bookmark of a filtered list is still the filter.
	post := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/v1/auth/register?utm=campaign", strings.NewReader(`{}`))
	post.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, post)
	if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "/api/v1/public/auth/register?utm=campaign" {
		t.Errorf("POST to a moved door = %d to %q, want 307 with the method and the query kept", w.Code, w.Header().Get("Location"))
	}

	// The target is served by the route, not by the table: the catalog is the
	// composition's own, and the address the alias points at is where it answers.
	httpx.Register(s.api.Surfaces("").App, huma.Operation{
		OperationID: "catalog", Method: http.MethodGet, Path: "/resources",
	}, httpx.SignedIn(), ok)
	if got := dial(t, s.router, http.MethodGet, host, "/api/v1/app/resources"); got.Code != http.StatusOK {
		t.Errorf("the catalog's address = %d %s, want the route the composition mounted", got.Code, got.Body.String())
	}

	// Nothing answers at an address the table vouches for twice. A module that
	// mounts where something used to be is refused, because two answers at one
	// address is how a form ends up posting into a redirect.
	other := newSurfaces(t)
	httpx.Register(other.api.Surfaces("admin").App, huma.Operation{
		OperationID: "second-mount", Method: http.MethodGet, Path: "/resources",
	}, httpx.SignedIn(), ok)
	if err := other.api.ValidateDeclarations(); err == nil {
		t.Error("a route mounted at an address the migration table vouches for passed the gate")
	} else if !strings.Contains(err.Error(), "redirect") {
		t.Errorf("the refusal does not say the reason: %v", err)
	}
}

// TestAPublicSurfaceTakesNobodiesSessionAndSetsNobodiesCookie is the public chain:
// an anonymous visitor is anonymous even holding a session cookie; a public
// response that opens a session is a bug and is refused; and the caching that
// makes the face cheap does not leak onto the workspace.
func TestAPublicSurfaceTakesNobodiesSessionAndSetsNobodiesCookie(t *testing.T) {
	s := newSurfaces(t)
	sawPrincipal := false
	httpx.Register(s.api.Surfaces("content").Public, huma.Operation{
		OperationID: "brochure", Method: http.MethodGet, Path: "/brochure",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*body, error) {
		_, sawPrincipal = tenancy.PrincipalFrom(ctx)
		return &body{}, nil
	})
	httpx.Register(s.api.Surfaces("file").Public, huma.Operation{
		OperationID: "hand-out-a-cookie", Method: http.MethodGet, Path: "/cookie",
	}, httpx.Public(), func(_ context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{Body: func(h huma.Context) {
			h.SetHeader("Set-Cookie", "session=abc; HttpOnly")
			h.SetStatus(http.StatusOK)
			_, _ = h.BodyWriter().Write([]byte(`{}`))
		}}, nil
	})
	httpx.Register(s.api.Surfaces("billing").App, huma.Operation{
		OperationID: "read-subscription", Method: http.MethodGet, Path: "/subscriptions",
	}, httpx.Permission("billing:read"), ok)
	if err := s.api.ValidateDeclarations(); err != nil {
		t.Fatalf("the public routes do not declare themselves: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/public/content/brochure", nil)
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("the public door = %d %s", w.Code, w.Body.String())
	}
	if sawPrincipal {
		t.Error("the public surface recognised a caller; it reads no session at all")
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("a public response says %q about caching, want it cacheable", got)
	}
	if got := w.Header().Get("X-Robots-Tag"); got != "" {
		t.Errorf("the public face is told %q about indexing; the workspace is the thing nobody should index", got)
	}

	// A workspace response is nobody's to cache, and the generated shell's
	// address is nobody's to frame.
	workspace := dial(t, s.router, http.MethodGet, host, "/api/v1/billing/subscriptions")
	if workspace.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("a workspace response says %q about caching", workspace.Header().Get("Cache-Control"))
	}

	// A public response that tries to open a session is refused, and the cookie it
	// wrote does not reach the visitor.
	cookie := dial(t, s.router, http.MethodGet, host, "/api/v1/public/file/cookie")
	if cookie.Code != http.StatusInternalServerError || !strings.Contains(cookie.Body.String(), httpx.CodePublicSetsACookie) {
		t.Errorf("a public route that sets a cookie = %d %s, want 500 %s", cookie.Code, cookie.Body.String(), httpx.CodePublicSetsACookie)
	}
	if got := cookie.Header().Get("Set-Cookie"); got != "" {
		t.Errorf("the refused response still carried %q", got)
	}
}

// TestAnAnonymousWriteIsTheOnlyThingThePublicSurfaceLimits names why the public
// surface carries a rate limit at all: it is the one surface with no account to
// lock out. A workspace write has one, and is not counted here.
func TestAnAnonymousWriteIsTheOnlyThingThePublicSurfaceLimits(t *testing.T) {
	limiter := &window{allow: 2}
	s := newSurfacesWith(t, httpx.Options{WriteLimiter: limiter})
	ran := 0
	// The public write carries no session: there is none to carry.
	httpx.Register(s.api.Surfaces("auth").Public, huma.Operation{
		OperationID: "register", Method: http.MethodPost, Path: "/register",
	}, httpx.Public(), func(context.Context, *struct{}) (*body, error) {
		ran++
		return &body{}, nil
	})
	httpx.Register(s.api.Surfaces("billing").App, huma.Operation{
		OperationID: "write-subscription", Method: http.MethodPost, Path: "/subscriptions",
	}, httpx.Permission("billing:write"), func(context.Context, *struct{}) (*body, error) {
		ran++
		return &body{}, nil
	})
	if err := s.api.ValidateDeclarations(); err != nil {
		t.Fatalf("the routes do not declare themselves: %v", err)
	}
	write := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://"+host+path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)
		return w
	}
	var last *httptest.ResponseRecorder
	for range 3 {
		last = write("/api/v1/public/auth/register")
	}
	if ran != 2 {
		t.Errorf("the handler ran %d times against a limit of 2", ran)
	}
	if last.Code != http.StatusTooManyRequests || last.Header().Get("Retry-After") == "" {
		t.Errorf("the third anonymous write = %d, want 429 with a Retry-After", last.Code)
	}

	before := ran
	if got := write("/api/v1/billing/subscriptions"); got.Code == http.StatusTooManyRequests {
		t.Error("a workspace write was refused by the public surface's limit")
	}
	if ran != before+1 {
		t.Errorf("the workspace write reached its handler %d times; it is not the surface's to count", ran-before)
	}
}

// TestAWorkspaceDoorThatAnswersAnonymouslyIsListed is why the removed allowlist
// is not a hole: the routes that answer nobody on the workspace are read back off
// the declarations they carry, and boot prints them.
func TestAWorkspaceDoorThatAnswersAnonymouslyIsListed(t *testing.T) {
	s := newSurfaces(t)
	httpx.Register(s.api.Surfaces("auth").App, huma.Operation{
		OperationID: "sign-in", Method: http.MethodPost, Path: "/login",
	}, httpx.Public(), ok)
	httpx.Register(s.api.Surfaces("auth").App, huma.Operation{
		OperationID: "sign-out", Method: http.MethodPost, Path: "/logout",
	}, httpx.Permission("auth:session"), ok)
	httpx.Register(s.api.Surfaces("content").Public, huma.Operation{
		OperationID: "brochure", Method: http.MethodGet, Path: "/brochure",
	}, httpx.Public(), ok)
	if err := s.api.ValidateDeclarations(); err != nil {
		t.Fatalf("the doors do not declare themselves: %v", err)
	}

	doors := s.api.AnonymousDoors()
	if len(doors) != 1 || doors[0] != "POST "+s.api.Surfaces("auth").App.Path("/login") {
		t.Errorf("the workspace admits %v, want the one route that declares it answers nobody", doors)
	}
}

// TestEveryMountedRouteStatesItsSurface is the invariant the document carries: a
// route and its surface cannot disagree, because the router that mounted it is
// what named the surface. It is asserted on the recording rather than on the
// address, which is the same reason the OpenAPI extension is written from the
// router: a reader of the document should not have to re-derive the answer.
func TestEveryMountedRouteStatesItsSurface(t *testing.T) {
	s := newSurfaces(t)
	httpx.Register(s.api.Surfaces("billing").App, huma.Operation{
		OperationID: "read-subscription", Method: http.MethodGet, Path: "/subscriptions",
	}, httpx.Permission("billing:read"), ok)
	httpx.Register(s.api.Surfaces("content").Public, huma.Operation{
		OperationID: "brochure", Method: http.MethodGet, Path: "/brochure",
	}, httpx.Public(), ok)
	httpx.Register(s.api.Surfaces("billing").Ops, huma.Operation{
		OperationID: "reconcile", Method: http.MethodGet, Path: "/reconciliations",
	}, httpx.OperatorPermission("billing:reconcile"), ok)
	httpx.HTML(s.api.Surfaces("billing").App, huma.Operation{
		OperationID: "subscription-page", Method: http.MethodGet, Path: "/subscriptions",
	}, httpx.Permission("billing:read"), func(context.Context, *struct{}) (*httpx.Page, error) {
		return &httpx.Page{Status: http.StatusOK, ContentType: httpx.HTMLContentType, Body: []byte("<!doctype html>")}, nil
	})
	if err := s.api.ValidateDeclarations(); err != nil {
		t.Fatalf("the routes do not declare themselves: %v", err)
	}

	stamped := map[string]string{}
	for _, op := range s.api.Recorded() {
		value, ok := op.Extensions[httpx.SurfaceExtension]
		if !ok {
			t.Errorf("%s %s carries no %s", op.Method, op.Path, httpx.SurfaceExtension)
			continue
		}
		stamped[op.Method+" "+op.Path] = fmt.Sprint(value)
	}
	for _, m := range s.api.Mounted() {
		// The catalogue and the generated document are mounted by whoever composes
		// them, and a page shares its address with the value route beneath it, so
		// the comparison is one-way: whatever was mounted must be stamped, and
		// stamped the same way.
		at := m.Method + " " + m.Path
		if got, seen := stamped[at]; !seen {
			t.Errorf("%s is mounted and recorded nowhere", at)
		} else if got != string(m.Surface) {
			t.Errorf("%s is mounted on the %s surface and says %q in the document", at, m.Surface, got)
		}
	}
	if len(stamped) < 4 {
		t.Errorf("the recording holds %d stamped operations, want the three surfaces and a page", len(stamped))
	}
}

// TestAFileTreeIsRecordedAndDressedByItsSurface is the half of `Static` the
// prefix table cannot say. A tree is mounted outside the operation chain — a
// stylesheet opens no transaction and asks nothing of nobody — but it is mounted
// *inside* the surface's chain, so the answer about caching and indexing is that
// surface's answer, and the mount table lists the tree beside the routes of the
// namespace it stands in. A composition's audit of what it serves is incomplete
// if the files it serves are not in it.
func TestAFileTreeIsRecordedAndDressedByItsSurface(t *testing.T) {
	s := newSurfaces(t)
	tree := fstest.MapFS{"app.css": &fstest.MapFile{Data: []byte(":root{--a:1}")}}
	s.api.Surfaces("admin").App.Static("/assets", tree)
	s.api.Surfaces("web").Public.Static("/sheets", tree)
	if err := s.api.ValidateDeclarations(); err != nil {
		t.Fatalf("the trees do not describe themselves: %v", err)
	}

	workspace := dial(t, s.router, http.MethodGet, host, "/app/admin/assets/app.css")
	if workspace.Code != http.StatusOK || !strings.Contains(workspace.Body.String(), "--a") {
		t.Fatalf("the workspace's stylesheet = %d %q", workspace.Code, workspace.Body.String())
	}
	if got := workspace.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("a file of the workspace says %q about caching, want nobody to keep it", got)
	}
	if got := workspace.Header().Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Errorf("a file of the workspace says %q about indexing", got)
	}

	// The public tree is the other surface's answer: a visitor's stylesheet is
	// nobody's own, and a face that could not be cached is a face served from
	// this process forever.
	visitor := httptest.NewRecorder()
	s.router.ServeHTTP(visitor, httptest.NewRequest(http.MethodGet, "http://"+host+"/web/sheets/app.css", nil))
	if visitor.Code != http.StatusOK {
		t.Fatalf("the public stylesheet = %d", visitor.Code)
	}
	if got := visitor.Header().Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("a file of the public face says %q about caching, want it cacheable", got)
	}
	if got := visitor.Header().Get("X-Robots-Tag"); got != "" {
		t.Errorf("a file of the public face is told %q about indexing", got)
	}

	want := map[string]httpx.Surface{"GET /app/admin/assets/*": httpx.SurfaceApp, "GET /web/sheets/*": httpx.SurfacePublic}
	for _, m := range s.api.Mounted() {
		if at, listed := want[m.Method+" "+m.Path]; listed {
			if m.Surface != at {
				t.Errorf("%s is mounted on the %s surface and described as %s", m.Method+" "+m.Path, at, m.Surface)
			}
			delete(want, m.Method+" "+m.Path)
		}
	}
	if len(want) > 0 {
		t.Errorf("the mount table does not list the file trees %v", want)
	}
}

// perKey is a limiter that counts each key apart and lets each one ask once,
// which is the shape a case needs to say "this tenant's second write was
// refused" about one customer without refusing the other customer's first.
type perKey struct {
	seen map[string]int
	keys []string
}

func (p *perKey) Allow(_ context.Context, key string, _ int, _ time.Duration) (bool, time.Duration, error) {
	p.keys = append(p.keys, key)
	if p.seen == nil {
		p.seen = map[string]int{}
	}
	p.seen[key]++
	if p.seen[key] > 1 {
		return false, time.Minute, nil
	}
	return true, 0, nil
}

func (p *perKey) Count(context.Context, string, time.Duration) (int, time.Duration, error) {
	return 0, 0, nil
}
func (p *perKey) Forget(context.Context, string) error { return nil }

// TestTwoTenantsBehindOneAddressAreCountedApart is the promise the public write
// limit's key is there to keep: 192.0.2.1 is an address, not a customer, and the
// office that shares it — an egress, a VPN, anything that rewrites the peer
// address — is more than one tenant's office. One tenant's second write is
// refused, and the other tenant's first is not.
func TestTwoTenantsBehindOneAddressAreCountedApart(t *testing.T) {
	counted := &perKey{}
	s := newSurfacesWith(t, httpx.Options{WriteLimiter: counted})
	httpx.Register(s.api.Surfaces("auth").Public, huma.Operation{
		OperationID: "register", Method: http.MethodPost, Path: "/register",
	}, httpx.Public(), func(context.Context, *struct{}) (*body, error) { return &body{}, nil })
	if err := s.api.ValidateDeclarations(); err != nil {
		t.Fatalf("the route does not declare itself: %v", err)
	}
	write := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/v1/public/auth/register", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)
		return w
	}

	first := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	other := tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	s.setWho(first)
	if got := write(); got.Code != http.StatusOK {
		t.Fatalf("the first write of the first tenant = %d %s", got.Code, got.Body.String())
	}
	s.setWho(other)
	if got := write(); got.Code != http.StatusOK {
		t.Fatalf("the first write of the second tenant = %d %s; the two are counted apart or this is one counter", got.Code, got.Body.String())
	}
	s.setWho(first)
	if got := write(); got.Code != http.StatusTooManyRequests {
		t.Errorf("the first tenant's second write = %d, want the refusal that is its own: %s", got.Code, got.Body.String())
	}
	if len(counted.keys) != 3 {
		t.Fatalf("the limit was asked %d times, want three", len(counted.keys))
	}
	for i, key := range counted.keys[:2] {
		want := []string{"acme", "globex"}[i]
		if !strings.Contains(key, want+" ") {
			t.Errorf("write %d was counted under %q, which does not name %s", i+1, key, want)
		}
	}
	if counted.keys[0] == counted.keys[1] {
		t.Errorf("both tenants were counted under one key %q", counted.keys[0])
	}
	if !strings.Contains(counted.keys[2], counted.keys[0]) {
		t.Errorf("the retry of the first tenant counted under %q, which is not the key it started on", counted.keys[2])
	}
}

// TestTheCookiePromiseIsThePublicSurfacesOwn is both halves of the promise one
// case cannot say alone. The public surface remembers nobody, at any size of
// body: a small one is discarded with the 500 that names the fault, and one that
// streams — an export, a file, a rendered document, which is what the surface
// exists for — is answered with its bytes and without the header, because the
// promise was about the visitor and not about the response code. The workspace
// side is the other half: a session cookie is what the workspace is made of, and
// a guard that reached past its surface would sign everybody out.
func TestTheCookiePromiseIsThePublicSurfacesOwn(t *testing.T) {
	s := newSurfaces(t)
	mint := func(h huma.Context) {
		h.SetHeader("Set-Cookie", "session=somebody; HttpOnly")
		h.SetStatus(http.StatusOK)
		_, _ = h.BodyWriter().Write([]byte("x"))
	}
	httpx.Register(s.api.Surfaces("file").Public, huma.Operation{
		OperationID: "small", Method: http.MethodGet, Path: "/small",
	}, httpx.Public(), func(_ context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{Body: mint}, nil
	})
	httpx.Register(s.api.Surfaces("file").Public, huma.Operation{
		OperationID: "big", Method: http.MethodGet, Path: "/big",
	}, httpx.Public(), func(_ context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{Body: func(h huma.Context) {
			h.SetHeader("Set-Cookie", "session=somebody; HttpOnly")
			h.SetStatus(http.StatusOK)
			_, _ = h.BodyWriter().Write([]byte(strings.Repeat("x", 3<<20)))
		}}, nil
	})
	httpx.Register(s.api.Surfaces("auth").App, huma.Operation{
		OperationID: "sign-in", Method: http.MethodGet, Path: "/login",
	}, httpx.SignedIn(), func(_ context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{Body: mint}, nil
	})
	if err := s.api.ValidateDeclarations(); err != nil {
		t.Fatalf("the routes do not declare themselves: %v", err)
	}

	small := dial(t, s.router, http.MethodGet, host, "/api/v1/public/file/small")
	if small.Code != http.StatusInternalServerError || !strings.Contains(small.Body.String(), httpx.CodePublicSetsACookie) {
		t.Errorf("a public route that mints a cookie = %d %s, want 500 %s", small.Code, small.Body.String(), httpx.CodePublicSetsACookie)
	}
	if got := small.Header().Get("Set-Cookie"); got != "" {
		t.Errorf("the discarded answer still carried %q", got)
	}

	big := dial(t, s.router, http.MethodGet, host, "/api/v1/public/file/big")
	if big.Body.Len() < 2<<20 {
		t.Fatalf("the streaming route answered %d bytes; the case is about one past the buffer", big.Body.Len())
	}
	if got := big.Header().Get("Set-Cookie"); got != "" {
		t.Errorf("a streamed public answer carried %q", got)
	}

	workspace := dial(t, s.router, http.MethodGet, host, "/api/v1/auth/login")
	if got := workspace.Header().Get("Set-Cookie"); !strings.HasPrefix(got, "session=") {
		t.Errorf("a workspace answer carried %q; the surface that hands out sessions still hands out sessions", got)
	}
}
