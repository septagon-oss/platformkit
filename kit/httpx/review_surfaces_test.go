package httpx_test

// Reviewer's own cases for T-0024 (three surfaces by path), written to falsify
// what the change asserts about itself in kit/httpx/README.md, in aliases.go, in
// docs/adr/0017 and in the commit body. Each case asserts behaviour the change
// promises and has a branch that passes once the promise is kept; none asserts
// an impossibility. Where a case's reachability matters it is proved with what
// correct behaviour prints (a status, a header the kernel always sets, the body
// of the 404 a never-mounted address gives), never with the defect's own output.
//
// Reviewer: a fresh pi session, 2026-09-21. REVIEW.md in the state directory
// carries the reproduction of each and the assertion it falsifies.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// askHeadered is a request with a session, so an answer is the gate's and not
// the anonymous refusal, and returns the whole response. The caller is
// recognised and authorized, so a 404 can only be the gate's answer.
func askHeadered(t *testing.T, h http.Handler, authority, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+authority+path, nil)
	req.Header.Set("Accept", "application/json")
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// sameRefusal is a refusal's own account, with the one field that differs per
// request — the instance URN, which carries the request id — taken out.
var requestURN = regexp.MustCompile(`"instance":"urn:request:[^"]*"`)

func sameRefusal(rec *httptest.ResponseRecorder) string {
	return requestURN.ReplaceAllString(rec.Body.String(), `"instance":"urn:request:id"`)
}

func operatorFixture(t *testing.T) http.Handler { return operatorFixtureWithFault(t, nil) }

func operatorFixtureWithFault(t *testing.T, fault httpx.Fault) http.Handler {
	t.Helper()
	operator := tenancy.Tenant{ID: uuid.New(), Slug: "installation", Name: "Installation", Operator: true}
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost:   host,
		Installation: installationHost,
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
			if h == host || h == installationHost {
				return operator, nil
			}
			return tenancy.Tenant{}, tenancy.ErrNoSuchHost
		}),
		Conn:      app,
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) { return true, nil }),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log:   slog.New(slog.DiscardHandler),
		Fault: fault,
	})
	httpx.Register(api.Surfaces("tenant").Ops, huma.Operation{
		OperationID: "reviewer-ops-list", Method: http.MethodGet, Path: "/tenants",
	}, httpx.OperatorPermission("tenant:manage"), ok)
	httpx.Register(api.Surfaces("tenant").App, huma.Operation{
		OperationID: "reviewer-app-permission", Method: http.MethodGet, Path: "/things",
	}, httpx.Permission("task:read"), ok)
	httpx.Register(api.Surfaces("tenant").App, huma.Operation{
		OperationID: "reviewer-app-signed-in", Method: http.MethodGet, Path: "/door",
	}, httpx.SignedIn(), ok)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the routes do not declare themselves: %v", err)
	}
	return router
}

// TestTheControlPlaneRefusalIsTheSameAnswerAsAnAddressNobodyMounted.
//
// "Anywhere else it answers exactly what an address nobody mounted answers, down
// to the bytes, so the surface discloses nothing" — kit/httpx/README.md, and the
// same sentence in docs/adr/0017 and in CHANGELOG.md. `notHere`, the refusal the
// *authorization* middleware gives on the control-plane surface, is written
// through huma for exactly this reason ("which is also what makes the answer
// byte-identical to the one a never-mounted address gets, Fault page included").
//
// The other gate — the one that answers /ops at a host that is not the
// installation's — is not. It runs inside the `surfaces` middleware, which is
// mounted on the root router *before* the middleware that puts the security
// headers and the caching decision on a response (kit/httpx/httpx.go:
// `root.Use(a.requestID, a.surfaces, a.headers)`), and it answers with a.fail and
// returns without ever calling next. So a browser that asks a customer's host for
// the control plane is served an HTML refusal page with no Content-Security-Policy,
// no X-Frame-Options, no X-Content-Type-Options and no Cache-Control, while the
// same browser asking for an address nobody mounted at the same host gets all
// four. The difference is the disclosure the README says does not exist, and the
// uncached HTML page is a hard-reload refusal.
//
// The passing branch: answer the host gate after the headers middleware has
// wrapped the writer (or state the four headers on this one response), which is
// what "the same answer, down to the bytes" requires.
func TestTheControlPlaneRefusalIsTheSameAnswerAsAnAddressNobodyMounted(t *testing.T) {
	h := operatorFixture(t)

	const controlPlane = "/api/v1/ops/tenant/tenants"
	const nowhere = "/nothing-is-mounted-at-this-address"

	// Reachability first, and it does not depend on the refusal being correct:
	// the control-plane route is mounted (the installation host serves it — see
	// the case below), and the never-mounted address gives the answer this case
	// compares against.
	ops := askHeadered(t, h, installationHost, controlPlane)
	if ops.Code != http.StatusOK {
		t.Fatalf("the control plane at the installation host = %d; the case asks about an address that is served there", ops.Code)
	}

	base := askHeadered(t, h, host, nowhere)
	gate := askHeadered(t, h, host, controlPlane)

	if base.Code != http.StatusNotFound {
		t.Fatalf("a never-mounted address at a customer host = %d, want the 404 this case compares against", base.Code)
	}
	if gate.Code != base.Code {
		t.Fatalf("the control plane at a customer host = %d, an address nobody mounted = %d; the README promises one answer", gate.Code, base.Code)
	}
	if a, b := sameRefusal(gate), sameRefusal(base); a != b {
		t.Errorf("the two refusals are not the same answer:\n control plane: %s\n never mounted: %s", a, b)
	}
	// Every header the ordinary refusal of this host carries. headers.go puts
	// them on every response, so the comparison is not a list this case invented.
	for name, want := range base.Header() {
		if name == "X-Request-Id" {
			continue // one per request, on both
		}
		if got := gate.Header().Values(name); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("the control-plane refusal at a customer host answers %s=%v where every other refusal of this host answers %s=%v; "+
				"a caller that can tell the two apart has been told a control plane exists, which README.md says it cannot do",
				name, got, name, want)
		}
	}

	// The same two addresses, asked as a browser navigation, against the refusal
	// renderer a real composition registers. This is where the missing headers are
	// not a fingerprint but a document: the page a person is shown at a customer's
	// host has no Content-Security-Policy and no frame protection, while every other
	// refusal page of that host has both.
	t.Run("the document a browser is shown", func(t *testing.T) {
		html := operatorFixtureWithFault(t, func(w http.ResponseWriter, _ *http.Request, p *problem.Problem) bool {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<h1>" + p.Title + "</h1>"))
			return true
		})
		ask := func(path string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
			req.Header.Set("Accept", "text/html")
			req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
			rec := httptest.NewRecorder()
			html.ServeHTTP(rec, req)
			return rec
		}
		if page := ask(nowhere); !strings.Contains(page.Header().Get("Content-Security-Policy"), "default-src") {
			t.Fatalf("the ordinary refusal page of this host carries no policy; the reference is broken: %v", page.Header())
		}
		if refused := ask(controlPlane); refused.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("the control-plane refusal page a browser is shown at a customer's host carries no Content-Security-Policy; "+
				"every other refusal page of that host does, and the frame protection is missing with it (%q)",
				refused.Header().Get("X-Frame-Options"))
		}
	})
}

// TestTheControlPlaneIsServedOnlyAtTheInstallationHostInEverySpellingOfIt is a
// pin, not a finding: it holds today and it is the case a later change to
// HostOnly or to servesOps would break silently. The control plane's entire
// defence is one comparison of two normalised addresses, so every spelling that
// *is* the installation host must be served and every one that only looks like it
// must not be.
func TestTheControlPlaneIsServedOnlyAtTheInstallationHostInEverySpellingOfIt(t *testing.T) {
	h := operatorFixture(t)
	const at = "/api/v1/ops/tenant/tenants"

	for _, authority := range []string{installationHost, installationHost + ":8443", installationHost + ".", strings.ToUpper(installationHost)} {
		if got := askHeadered(t, h, authority, at); got.Code != http.StatusOK {
			t.Errorf("the control plane at %q = %d, want it served: the gate may not be stricter than the host it was given", authority, got.Code)
		}
	}
	for _, authority := range []string{"x" + installationHost, installationHost + ".example", "not" + installationHost + ".example", host, "acme." + installationHost} {
		if got := askHeadered(t, h, authority, at); got.Code != http.StatusNotFound {
			t.Errorf("the control plane at %q = %d, want the 404 an unmounted address gives", authority, got.Code)
		}
	}
	// The path's own spellings, at a customer host. Whatever the router does with
	// each of them afterwards, none of them may serve an operator route there.
	for _, path := range []string{
		"//api/v1/ops/tenant/tenants",
		"/API/v1/ops/tenant/tenants",
		"/api/v1/ops//tenant/tenants",
		"/api/v1/public/../ops/tenant/tenants",
		"/app/../api/v1/ops/tenant/tenants",
		"/api/v1/%6fps/tenant/tenants",
		"/api/v1/ops/tenant/tenants/",
	} {
		if got := askHeadered(t, h, host, path); got.Code != http.StatusNotFound {
			t.Errorf("%s at a customer host = %d, want 404: an address that only looks like the control plane must not serve it", path, got.Code)
		}
	}
}

// TestTheWorkspaceRefusesAnAnonymousCallerUnderEveryDeclarationButTheOneThatAdmitsIt
// is a pin over the App chain's central sentence — "deny by default, every route
// needs a principal unless allowlisted" — across *both* declarations that need
// one. A tenant middleware that admits an anonymous caller to a route declaring
// Public() is the allowlist; the same middleware must not be able to admit one to
// a route declaring SignedIn(), because that is the half of the table the public
// surface's own tests do not cover.
func TestTheWorkspaceRefusesAnAnonymousCallerUnderEveryDeclarationButTheOneThatAdmitsIt(t *testing.T) {
	h := operatorFixture(t)

	anonymous := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for _, at := range []string{"/api/v1/tenant/things", "/api/v1/tenant/door"} {
		got := anonymous(at)
		if got.Code != http.StatusForbidden {
			t.Errorf("the workspace route %s with no session = %d, want the refusal the surface gives by default: %s",
				at, got.Code, strings.TrimSpace(got.Body.String()))
		}
		if !strings.Contains(got.Body.String(), httpx.CodeAnonymous) {
			t.Errorf("the workspace route %s refused without naming AUTH_ANONYMOUS: %s", at, strings.TrimSpace(got.Body.String()))
		}
	}
	// And with a session both answer, so the refusals above are the principal's
	// absence and not a route that was never mounted.
	for _, at := range []string{"/api/v1/tenant/things", "/api/v1/tenant/door"} {
		if got := askHeadered(t, h, host, at); got.Code != http.StatusOK {
			t.Errorf("the workspace route %s with a session = %d, want it served: %s", at, got.Code, strings.TrimSpace(got.Body.String()))
		}
	}
}

// TestAStaticTreeIsNeverMountedOutsideTheChainOfTheSurfaceItWasGiven.
//
// The control plane serves no documents, and the kernel refuses that three ways:
// a page mounted on r.Ops is refused at mount, Router.PagePath refuses to compose
// one, and r.Ops.Home refuses to claim a root. Router.Static is a fourth mount
// door and is the one that is not refused: it composes through the page prefix
// (which is empty for Ops), records neither a mount nor a refusal, and is mounted
// straight onto the root mux — so `r.Ops.Static("/exports", tree)` publishes the
// installation's own files at /exports/*, anonymously, at every customer's host,
// on the public chain (Cache-Control: public, max-age=60), in a composition whose
// ValidateDeclarations is clean and whose Mounted() does not list them.
//
// The same door reaches one step further down the table. `r.App.Static("/", tree)`
// composes the *workspace root* and mounts "/app/*" on the outer mux, ahead of
// every middleware and of the API's own mount: a module that hands the workspace's
// root prefix to a file server keeps it, and the gate that exists to stop a route
// mounting outside its chain never sees the line.
//
// The passing branch for both is the noteRefusal PagePath already writes for the
// same contradiction: a composition that is refused never listens, so the
// assertion is the refusal.
func TestAStaticTreeIsNeverMountedOutsideTheChainOfTheSurfaceItWasGiven(t *testing.T) {
	tree := fstest.MapFS{"reconciliation.txt": &fstest.MapFile{Data: []byte("the installation's own ledger")}}

	t.Run("control plane", func(t *testing.T) {
		s := newSurfaces(t)
		s.api.Surfaces("billing").Ops.Static("/exports", tree)
		err := s.api.ValidateDeclarations()
		if err == nil {
			// Reachability, proved without the refusal: ask the customer's host,
			// anonymously, for the file, and report what came back.
			rec := httptest.NewRecorder()
			s.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+host+"/exports/reconciliation.txt", nil))
			t.Errorf("a file tree mounted on the control-plane router passed the gate that refuses a page there; "+
				"a customer's host answered it anonymously: %d %q cache=%q",
				rec.Code, strings.TrimSpace(rec.Body.String()), rec.Header().Get("Cache-Control"))
			return
		}
		if !strings.Contains(err.Error(), "Ops") {
			t.Errorf("the gate refused the tree for a reason that does not name the surface: %v", err)
		}
	})

	t.Run("workspace root", func(t *testing.T) {
		s := newSurfaces(t)
		app := s.api.Surfaces("billing").App
		httpx.Register(app, huma.Operation{
			OperationID: "reviewer-app-page", Method: http.MethodGet, Path: "/plans",
		}, httpx.Permission("task:read"), ok)
		app.Static("/", tree)
		err := s.api.ValidateDeclarations()
		if err == nil {
			rec := httptest.NewRecorder()
			s.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+host+"/app/billing/plans", nil))
			t.Errorf("a module took the workspace root prefix with Static and the gate said nothing; the address a "+
				"route of that same module answers at now answers %d: the file server is mounted ahead of the chain",
				rec.Code)
			return
		}
		if !strings.Contains(err.Error(), "Static") && !strings.Contains(err.Error(), "root") {
			t.Errorf("the gate refused the tree for a reason that names neither the door nor the root: %v", err)
		}
	})
}

// countedWrites records every key the public write limit is counted under, and
// nothing else about it.
type countedWrites struct{ keys []string }

func (c *countedWrites) Allow(_ context.Context, key string, _ int, _ time.Duration) (bool, time.Duration, error) {
	c.keys = append(c.keys, key)
	return true, 0, nil
}
func (c *countedWrites) Count(context.Context, string, time.Duration) (int, time.Duration, error) {
	return 0, 0, nil
}
func (c *countedWrites) Forget(context.Context, string) error { return nil }

// TestThePublicWriteLimitCountsTheTenantItSaysItCounts.
//
// publicWrites is the Public surface's only rate limit, and its own comment says
// what it counts: "the writes with no account to lock out … counted by tenant and
// address. … The key is both, so two customers never share a counter and one
// customer's office does not exhaust another's." kit/httpx/README.md repeats the
// promise ("it is the one surface with no account to lock out").
//
// The key holds no tenant. publicWrites is the first middleware in the huma chain
// (`a.api.UseMiddleware(a.publicWrites, a.tenant, …)`, and the comment there says
// the order is "the point"), so the request has not resolved a host yet,
// `tenancy.FromContext` answers nothing, and every public write from one egress
// address — a customer's own office, a VPN, anything behind a proxy that rewrites
// RemoteAddr — is counted against every other tenant's anonymous writes.
//
// The passing branch keeps both stated reasons for the order: run the count after
// the tenant middleware, which opens no transaction (it resolves through the host
// cache on a system token), and still ahead of the transaction middleware, so a
// refused write is still not a transaction that rolled back an attempt nobody made.
func TestThePublicWriteLimitCountsTheTenantItSaysItCounts(t *testing.T) {
	counted := &countedWrites{}
	s := newSurfacesWith(t, httpx.Options{WriteLimiter: counted})
	httpx.Register(s.api.Surfaces("auth").Public, huma.Operation{
		OperationID: "reviewer-register", Method: http.MethodPost, Path: "/register",
	}, httpx.Public(), ok)
	if err := s.api.ValidateDeclarations(); err != nil {
		t.Fatalf("the route does not declare itself: %v", err)
	}

	// Reachability, with none of the limit's own answer in it: this is the host the
	// fixture resolves to the tenant "acme", and the request reached the counter at
	// all — otherwise there is no key to read and the case says nothing.
	s.router.ServeHTTP(httptest.NewRecorder(), func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/v1/public/auth/register", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		return r
	}())
	if len(counted.keys) != 1 {
		t.Fatalf("an anonymous public write was counted %d times; this case asks about the one key it counts under", len(counted.keys))
	}
	if key := counted.keys[0]; !strings.Contains(key, "acme") {
		t.Errorf("the public write limit counts %q; the tenant the request's own host resolved to is not in the key, "+
			"so the promise that two customers never share a counter is false for every pair that shares an address", key)
	}
}

// TestThePublicSurfaceSetsNoCookieHoweverTheBodyIsWritten.
//
// "Public … sets no cookie (a handler that mints one is a 500 named
// PUBLIC_SETS_A_COOKIE, with the body discarded)" — kit/httpx/README.md, repeated
// in the commit body and in CHANGELOG.md. The guard can only keep that promise
// while the response is still held: it runs after the handler, and a route that
// has already written past `maxBuffer` (2 MiB) has had its headers sent by the
// buffer's own `Write`, so `reset` reports false and the guard is a log line. The
// cookie reaches the visitor, over it a `Cache-Control: public, max-age=60`, and
// whatever is between this process and the next visitor holds the pair.
//
// The passing branch is to withhold Set-Cookie at the writer on the public surface
// — which is the promise — rather than to detect the breach after the bytes went.
func TestThePublicSurfaceSetsNoCookieHoweverTheBodyIsWritten(t *testing.T) {
	s := newSurfaces(t)
	httpx.Register(s.api.Surfaces("content").Public, huma.Operation{
		OperationID: "reviewer-stream", Method: http.MethodGet, Path: "/big",
	}, httpx.Public(), func(_ context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{Body: func(h huma.Context) {
			h.SetHeader("Set-Cookie", "session=somebody; HttpOnly")
			h.SetStatus(http.StatusOK)
			_, _ = h.BodyWriter().Write([]byte(strings.Repeat("x", 3<<20)))
		}}, nil
	})
	if err := s.api.ValidateDeclarations(); err != nil {
		t.Fatalf("the route does not declare itself: %v", err)
	}

	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/public/content/big", nil))
	// Reachability first, and it does not need the guard to work: the route is
	// mounted, the surface is the public one, and the answer arrived.
	if rec.Body.Len() < 2<<20 {
		t.Fatalf("the public route answered %d bytes; this case is about one that streams past the buffer", rec.Body.Len())
	}
	if got := rec.Header().Get("Set-Cookie"); got != "" {
		t.Errorf("a public answer carried %q to the visitor and told the cache in front of it to hold the response (%q); "+
			"the surface promises that nobody standing at it is remembered", got, rec.Header().Get("Cache-Control"))
	}
}

// TestAPublicDocumentIsServedWhereTheKernelComposesIt is a pin over a
// contradiction three documents carry. kit/httpx/README.md's surface table,
// ARCHITECTURE.md's surfaces paragraph and docs/adr/0017's table all say a public
// document answers at `/public/<module>/<rel>`, and the ADR's own closing argument
// — that a module named `public` is refused "because a module of that name would
// compose an address whose prefix and surface disagree" — only stands if that
// prefix is composed. It is not: `pagePrefix` gives `/<module>`, and
// `Router.relFault` refuses the documented spelling outright, so a module author
// who writes the address their own reference documentation names is refused at
// compose.
//
// This holds what the code does and what the gate refuses, so that settling the
// contradiction — correct the three documents, or compose the prefix and stop
// refusing it — is a deliberate change with a test that moves with it. It is not
// only tidiness: `/<module>/…` is the namespace a tenant's published slugs live
// in (modules/web claims the public root and answers `/{slug}`), which is the
// collision docs/adr/0017 opens by refusing to accept for the workspace.
func TestAPublicDocumentIsServedWhereTheKernelComposesIt(t *testing.T) {
	brochure := newSurfaces(t).api.Surfaces("brochure")
	if got := brochure.Public.PagePath("/index"); got != "/brochure/index" {
		t.Errorf("a public document composes %q; if the answer is meant to be /public/brochure/index, then "+
			"kit/httpx/README.md, ARCHITECTURE.md and docs/adr/0017 are right and this is the bug", got)
	}
	other := newSurfaces(t)
	other.api.Surfaces("brochure").Public.PagePath("/public/index")
	if err := other.api.ValidateDeclarations(); err == nil || !strings.Contains(err.Error(), "names a prefix") {
		t.Errorf("a module that writes the address the three documents name is not refused at the gate: %v", err)
	}
}

// TestThePublicDocumentAddressIsTheOneTheDocumentationPrints.
//
// Three shipped places print a public *document* address the kernel never composes:
// this package's README table ("Document address … `/public/<module>/<rel>`"),
// ARCHITECTURE.md's surfaces paragraph ("`/api/v1/public/<module>/…` and
// `/public/<module>/…` for a tenant's anonymous face") and docs/adr/0017's table.
// `pagePrefix` composes `/<module>/…` — which is what this package's own table in
// surfaces.go says, and what the specification for this change says — and
// `Router.relFault` refuses a relative path beneath `/public` outright, so a module
// that mounts the address the three places print is refused at compose. The ADR then
// argues from that prefix, in the paragraph that explains why a module named `public`
// is refused: a module of that name "would compose an address whose prefix and
// surface disagree" — an argument that only stands if the prefix is composed.
//
// The case takes the composed address as the fact and refuses a document that
// contradicts it, so the repair is either answer: correct the three places, or
// compose the prefix and stop refusing it. It skips the JSON address, which is the
// surface's other half and does say `/api/v1/public/<module>`, and it says nothing
// about a document that mentions `/public/<module>` as the address something *used*
// to answer at.
func TestThePublicDocumentAddressIsTheOneTheDocumentationPrints(t *testing.T) {
	composed := newSurfaces(t).api.Surfaces("brochure").Public.PagePath("/index")
	if composed != "/brochure/index" {
		t.Errorf("a public document composes %q; this case reads the documents against that fact, so update it with the composition", composed)
	}
	claim := "/public/<module>"
	for _, file := range []string{"README.md", "../../ARCHITECTURE.md", "../../docs/adr/0017-three-surfaces-by-path.md"} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for n, line := range strings.Split(string(b), "\n") {
			for _, lit := range strings.Split(line, "`") {
				if strings.HasPrefix(lit, "/api/v1/") || !strings.HasPrefix(lit, claim) {
					continue
				}
				t.Errorf("%s:%d prints a public document answering at %s/…, and the kernel composes %s and "+
					"refuses the printed spelling at mount; one of the two is wrong", file, n+1, claim, composed)
			}
		}
	}
}
