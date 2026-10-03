package httpx_test

// One table, every status the kernel can write, both shapes of the one verdict.
//
// The brief's acceptance line is that a request preferring text/html gets a page for
// every status ≥ 400 the kernel writes *from any layer*, and that the same request
// asking for a value gets the problem document byte for byte as before. The first
// half is what this table holds: each row is driven by the cause that really produces
// that status — an authorizer that cannot answer, an entitlement store that cannot
// answer, a host that cannot be resolved, a mux that found nothing, a handler that
// fell over, a check that failed — and never by a stub asked to return a status, which
// would test the table instead of the kernel.
//
// The page half is rendered by the presentation layer's own renderer
// (ui/page.FaultHandler with the catalogue this repository ships), not by a
// test stand-in: a fake page would pass while the real one stayed broken, and the
// whole defect was that the real person saw something the test could not see.
//
// The JSON half is pinned to whole bodies rather than to "contains". Each request
// carries X-Request-ID, so the instance URN the encoder writes is known and the
// comparison is exact; nothing here is a fixture written during this change, and the
// strings below are what kit/problem's field order and writeProblem produce.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui"
	uipage "github.com/septagon-oss/platformkit/ui/page"
	g "maragu.dev/gomponents"
)

// traceID is the request id every row sends, so the instance URN in the problem
// body — and the reference on the page — is a value this file can write out in
// full rather than one it has to fish out of the response.
const traceID = "11111111-2222-3333-4444-555555555555"

// faultCase is one refusal, the cause that drives it, and both answers.
type faultCase struct {
	// name is what the row is called in the failure, in the words a person would
	// use for the cause: the row has to read as a story about a request.
	name string
	// method, path and accept are the request. The path is relative to a module's
	// surface, which is where the kernel mounts it, and surface names which of the
	// two: the workspace's own door and the public one are different addresses with
	// different rules, and the limit lives on the public one alone.
	method, path, surface string
	// host is the address the request names, when it matters.
	host string
	// configure is what has to be true of the collaborators for the cause to be
	// real: an authorizer that errors, a plan store that errors, a limit already
	// spent. nil leaves the composition as it is.
	configure func(*fixture, *httpx.Options)
	// setup registers the operation this row needs, when it is not one of the
	// three every API in this file already carries.
	setup func(*testing.T, *httpx.API)

	status      int
	detail      string
	retryAfter  string // the header the guard sets, "" when it sets none
	retryLink   bool   // the page offers Retry: a transient verdict on a safe method
	waitSaid    bool   // the page names the wait
	signInShown bool   // the page offers the chrome's sign-in address
	// internals are the things that must never reach a person's screen.
	internals []string
}

// The five answers a browser can be given by the kernel itself, plus the two the
// guards write, plus the one the probe writes. A status whose refusal the kernel does
// not authorise belongs to a module's handler and is a page Serve renders — the case
// that drives those is in ui/page, where the handler lives.
func faultCases() []faultCase {
	limiter := func(f *fixture, o *httpx.Options) { o.WriteLimiter = spentLimiter{} }
	cases := []faultCase{{
		name:      "an address nobody mounted",
		method:    http.MethodGet,
		path:      "/no-such-address",
		status:    http.StatusNotFound,
		detail:    "nothing is served at this address",
		internals: []string{"goroutine", "panic:", "SELECT", "pq:", "gorm", "127.0.0.1"},
	}, {
		name:      "an address that does not take the verb it was asked with",
		method:    http.MethodPost,
		path:      "/widgets/quiet",
		status:    http.StatusMethodNotAllowed,
		detail:    "this address does not accept POST requests",
		internals: []string{"goroutine", "panic:", "SELECT", "gorm"},
	}, {
		name:       "a host the installation cannot resolve",
		method:     http.MethodGet,
		path:       "/site",
		configure:  func(f *fixture, _ *httpx.Options) { f.loadErr = errors.New("the tenant store is unreachable") },
		status:     http.StatusServiceUnavailable,
		detail:     "this host cannot be resolved right now",
		retryAfter: "3",
		retryLink:  true, waitSaid: true,
		internals: []string{"goroutine", "the tenant store is unreachable", "SELECT", "pq:"},
	}, {
		name:   "an authorization decision that could not be made",
		method: http.MethodGet,
		path:   "/widgets/protected",
		configure: func(f *fixture, _ *httpx.Options) {
			f.signedIn()
			f.allow = false
			f.authErr = errors.New("the policy store is down")
		},
		setup: func(t *testing.T, api *httpx.API) {
			httpx.Register(api.Surfaces(probe).App, huma.Operation{
				OperationID: "read-protected", Method: http.MethodGet, Path: "/widgets/protected",
			}, httpx.Permission("widget:read"), ok)
		},
		status:     http.StatusServiceUnavailable,
		detail:     "authorization is temporarily unavailable",
		retryAfter: "3",
		retryLink:  true, waitSaid: true,
		internals: []string{"goroutine", "the policy store is down", "SELECT", "pq:"},
	}, {
		name:    "a plan that could not be read",
		method:  http.MethodGet,
		path:    "/widgets/plan",
		surface: "public",
		configure: func(f *fixture, _ *httpx.Options) {
			f.includes = false
			f.planErr = errors.New("the plan is unreadable")
		},
		setup: func(t *testing.T, api *httpx.API) {
			httpx.Register(api.Surfaces(probe).Public, huma.Operation{
				OperationID: "read-planned", Method: http.MethodGet, Path: "/widgets/plan",
			}, httpx.Public().Needing("analytics"), ok)
		},
		status:     http.StatusServiceUnavailable,
		detail:     "the plan could not be read right now",
		retryAfter: "3",
		retryLink:  true, waitSaid: true,
		internals: []string{"goroutine", "the plan is unreadable", "analytics"},
	}, {
		name:      "a handler that fell over",
		method:    http.MethodPost,
		path:      "/widgets/explode",
		status:    http.StatusInternalServerError,
		detail:    "",
		internals: []string{"goroutine", "a handler that fell over", "runtime/debug", "panic:"},
	}, {
		name:       "the public write limit, met by the write it is counting",
		method:     http.MethodPost,
		path:       "/anonymous",
		surface:    "public",
		configure:  limiter,
		status:     http.StatusTooManyRequests,
		detail:     httpx.CodeLimitExhausted + ": too many anonymous submissions from this address",
		retryAfter: "60",
		// The rule this row pins: a POST is never offered a Retry control, because an
		// anchor cannot resend a form, and the wait is still said, because the number
		// is true whatever the method.
		retryLink: false, waitSaid: true,
		internals: []string{"goroutine", "publicWriteKey", "SELECT"},
	}}
	return cases
}

// TestEveryStatusTheKernelWritesAnswersAPersonWithAPageAndAClientWithTheSameBytesAsAlways
// is the table. Each case is asked twice: once of a browser, once of a client that
// asked for a value.
func TestEveryStatusTheKernelWritesAnswersAPersonWithAPageAndAClientWithTheSameBytesAsAlways(t *testing.T) {
	dbtest.Schema(t) // one schema for the package's runs, so the table is not 7 databases
	for _, tc := range faultCases() {
		t.Run(tc.name, func(t *testing.T) {
			api, router := setupStatusFault(t, tc)

			browser := askStatus(t, api, router, tc, browserAccept)
			if browser.Code != tc.status {
				t.Fatalf("a browser meeting %s got %d, want the verdict %d: %s", tc.name, browser.Code, tc.status, trim(browser.Body.String()))
			}
			if ct := browser.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("a browser meeting %s was handed %s, not a document: %s", tc.name, ct, trim(browser.Body.String()))
			}
			// The JSON half, asked second so the browser's answer cannot colour it:
			// the two requests are independent.
			value := askStatus(t, api, router, tc, "application/json")
			if value.Code != tc.status {
				t.Errorf("%s: the verdict changed for a client that asked for a value: %d, want %d", tc.name, value.Code, tc.status)
			}
			if ct := value.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("%s: a client that asked for a value got Content-Type %q", tc.name, ct)
			}
			if want := problemBody(tc.status, tc.detail); value.Body.String() != want {
				t.Errorf("%s: the problem document changed shape\n got %q\nwant %q", tc.name, value.Body.String(), want)
			}

			document := browser.Body.String()
			for _, forbidden := range tc.internals {
				if strings.Contains(document, forbidden) {
					t.Errorf("the page for %s carries internal detail %q: %s", tc.name, forbidden, trim(document))
				}
			}
			if !strings.Contains(document, http.StatusText(tc.status)) {
				t.Errorf("the page for %s never says what the verdict is called: %s", tc.name, trim(document))
			}
			if !strings.Contains(document, "Request reference") || !strings.Contains(document, "<samp") {
				t.Errorf("the page for %s carries no reference a person can read to support: %s", tc.name, trim(document))
			}
			if got := strings.Contains(document, "Try again"); got != tc.retryLink {
				t.Errorf("the page for %s offers Retry = %v, want %v (a transient verdict on a safe method only)", tc.name, got, tc.retryLink)
			}
			if got := strings.Contains(document, "seconds"); got != tc.waitSaid {
				t.Errorf("the page for %s names a wait = %v, want %v", tc.name, got, tc.waitSaid)
			}
			if tc.retryAfter != "" {
				if got := browser.Header().Get("Retry-After"); got != tc.retryAfter {
					t.Errorf("%s: the guard's Retry-After is %q on the page answer and %q elsewhere", tc.name, got, tc.retryAfter)
				}
			}
		})
	}
}

// TestTheReadinessProbeAnswersAPersonWithTheSamePageAndAMonitorWithTheSameDocument is
// the hole this task exists to close. The probe runs on a mux that carries neither the
// request middleware nor a transaction, so it had no API to ask and wrote its own
// problem body — which made a person who navigated to /ready during an outage the one
// reader left with JSON in a window.
func TestTheReadinessProbeAnswersAPersonWithTheSamePageAndAMonitorWithTheSameDocument(t *testing.T) {
	_, app := dbtest.Schema(t)
	counts := new(atomic.Int32)
	hosts := probeHosts{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme"}, loads: counts}
	api, router := httpx.New(httpx.Options{
		Installation: host, PublicHost: host,
		Cache:        cache.Memory("pkit"),
		Tenants:      hosts,
		Conn:         app,
		Authorize:    sites{},
		Authenticate: anonymous,
		Fault:        kernelFault(),
		Log:          slog.New(slog.DiscardHandler),
	})
	health.Register(api, []health.Check{broken{"database"}, broken{"nats"}})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}

	got := probeRequest(t, router, browserAccept, "pod.invalid")
	if got.Code != http.StatusServiceUnavailable {
		t.Fatalf("a browser at /ready during an outage got %d, want 503: %s", got.Code, trim(got.Body.String()))
	}
	if ct := got.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("a browser at /ready was handed %s and not a page: %s", ct, trim(got.Body.String()))
	}
	body := got.Body.String()
	// The check names stay out: the page says it is our side and for a moment, and the
	// names an operator needs are in the log line and the problem document.
	for _, forbidden := range []string{"dial tcp", "connection refused", "SELECT", "pq:", "127.0.0.1", "goroutine"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the readiness page names %q, which is the operator's fact and not the visitor's: %s", forbidden, trim(body))
		}
	}
	if !strings.Contains(body, "Try again") {
		t.Errorf("the readiness page offers no way to ask again, which is the one thing that clears: %s", trim(body))
	}
	if !strings.Contains(body, "Request reference") {
		t.Errorf("the readiness page carries no reference, and the probe request ran with no id middleware to give it one: %s", trim(body))
	}
	if got.Header().Get("Retry-After") != "" {
		t.Errorf("the readiness answer grew a Retry-After (%q); no honest number belongs to a probe", got.Header().Get("Retry-After"))
	}
	// Pillar 1's shape: a probe that resolved a tenant from a pod address would be one
	// tenant's data reachable from another's probe.
	if counts.Load() > 0 {
		t.Errorf("the readiness page asked the host %d times; a probe answers the process, not a site", counts.Load())
	}

	// The same address, for the monitor: the same verdict from the same encoder, in
	// the bytes a readiness stanza has compared since the probe existed. No instance
	// member and no trailing newline — the two things a document that added no member
	// and ended no line is made of — and the same two failing check names in the
	// detail. The reference the page shows is the same identifier the X-Request-ID
	// header this mux has always sent; putting it in this body too would be a new
	// member in a document a monitor compares, which is a contract change nobody read.
	// The alternative — a page for the browser and a second encoder for everybody else
	// — is the second writer kit/httpx/fault.go exists to refuse.
	const monitorBytes = `{"type":"about:blank","title":"Service Unavailable","status":503,"detail":"not ready: database, nats"}`
	monitor := probeRequest(t, router, "*/*", "pod.invalid")
	if monitor.Body.String() != monitorBytes {
		t.Errorf("the readiness answer to a monitor changed\n got %q\nwant %q", monitor.Body.String(), monitorBytes)
	}
	if monitor.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("the readiness answer to a monitor changed Content-Type to %q", monitor.Header().Get("Content-Type"))
	}

	// A probe that sends no Accept at all is the commonest monitor there is.
	quiet := probeRequest(t, router, "", "pod.invalid")
	if quiet.Body.String() != monitorBytes {
		t.Errorf("a readiness request that sent no Accept at all got a different answer than a monitor:\n got %q\nwant %q", quiet.Body.String(), monitorBytes)
	}
	// And the reference a monitor can still read, in the header it was always read from.
	if monitor.Header().Get(httpx.RequestIDHeader) == "" {
		t.Errorf("the readiness answer carries no %s, so the reference is nowhere a program can read it", httpx.RequestIDHeader)
	}
}

// TestAReadyInstanceAnswersABrowserWithItsOkAndNotWithAPage keeps the negotiation on
// its side of the line: the probe's success is not a fault, and a person who opens
// /ready on a healthy instance gets the same small body a monitor gets.
func TestAReadyInstanceAnswersABrowserWithItsOkAndNotWithAPage(t *testing.T) {
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		Installation: host, PublicHost: host,
		Cache:        cache.Memory("pkit"),
		Tenants:      sites{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme"}},
		Conn:         app,
		Authorize:    sites{},
		Authenticate: anonymous,
		Fault:        kernelFault(),
		Log:          slog.New(slog.DiscardHandler),
	})
	health.Register(api, []health.Check{ready{}})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	for _, accept := range []string{browserAccept, "*/*", ""} {
		got := probeRequest(t, router, accept, "pod.invalid")
		if got.Code != http.StatusOK || got.Body.String() != `{"status":"ok"}` {
			t.Errorf("a healthy /ready asked with Accept %q answered %d %q, want 200 {\"status\":\"ok\"}", accept, got.Code, got.Body.String())
		}
	}
}

// TestTheRefusalPageForAGrantDenialIsStillTheOneT0184Shipped holds the brief's
// exclusion: a 401/403 keeps T-0184's refusal page. The new props are empty there, so
// the page it draws has to be the one that was already being drawn.
func TestTheRefusalPageForAGrantDenialIsStillTheOneT0184Shipped(t *testing.T) {
	denial := faultCase{
		name: "a caller who lacks the grant", method: http.MethodGet, path: "/widgets/protected",
		configure: func(f *fixture, _ *httpx.Options) { f.signedIn(); f.allow = false },
		setup: func(_ *testing.T, api *httpx.API) {
			httpx.Register(api.Surfaces(probe).App, huma.Operation{
				OperationID: "read-protected", Method: http.MethodGet, Path: "/widgets/protected",
			}, httpx.Permission("widget:read"), ok)
		},
	}
	api, router := setupStatusFault(t, denial)
	got := askStatus(t, api, router, denial, browserAccept)
	if got.Code != http.StatusForbidden {
		t.Fatalf("a caller without the grant = %d, want the refusal this case is about: %s", got.Code, trim(got.Body.String()))
	}
	body := got.Body.String()
	for _, want := range []string{
		httpx.CodeDenied + ":", // the verdict, code first, as T-0184 shows it
		"What is missing",      // and its four parts, in their order
		"Ask your administrator",
		"Back to the workspace",
		// The reference stays inside the sentence here. This task moved it out of the
		// generic verdict page, and the brief excludes this page from the change.
		"(request ",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("T-0184's refusal page omits %q: %s", want, trim(body))
		}
	}
	if strings.Contains(body, "Try again") || strings.Contains(body, "Sign in") {
		t.Errorf("a denial grew an affordance signing in could not change: %s", trim(body))
	}
}

// setupStatusFault builds the API one row needs, with the real renderer registered.
func setupStatusFault(t *testing.T, tc faultCase) (*httpx.API, *chi.Mux) {
	t.Helper()
	_, app := dbtest.Schema(t)
	f := &fixture{
		tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"},
		app:    app,
		allow:  true,
		logs:   &lines{},
	}
	options := httpx.Options{
		Installation: host, PublicHost: host,
		Cache:        cache.Memory("pkit"),
		Tenants:      f,
		Conn:         app,
		Authorize:    f,
		Entitle:      f,
		Authenticate: f.authenticate,
		Log:          slog.New(slog.DiscardHandler),
		Fault:        kernelFault(),
	}
	if tc.configure != nil {
		tc.configure(f, &options)
	}
	api, router := httpx.New(options)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "write-widget", Method: http.MethodPost, Path: "/widgets",
	}, httpx.Public(), ok)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "read-widget", Method: http.MethodGet, Path: "/widgets/quiet",
	}, httpx.Public(), ok)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "explode-widget", Method: http.MethodPost, Path: "/widgets/explode",
	}, httpx.Public(), func(context.Context, *struct{}) (*struct{}, error) {
		panic("a handler that fell over")
	})
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "write-anonymously", Method: http.MethodPost, Path: "/anonymous",
	}, httpx.Public(), ok)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "read-site", Method: http.MethodGet, Path: "/site",
	}, httpx.SignedIn(), ok)
	if tc.setup != nil {
		tc.setup(t, api)
	}
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	return api, router
}

// askStatus asks one row's question of one API, with the id this file can name in
// advance so the JSON half can be compared byte for byte.
func askStatus(t *testing.T, api *httpx.API, router http.Handler, tc faultCase, accept string) *httptest.ResponseRecorder {
	t.Helper()
	address := tc.path
	switch tc.surface {
	case "public":
		address = publicly(api, tc.path)
	case "ops":
		address = onControlPlane(api, tc.path)
	default:
		address = at(api, tc.path)
	}
	target := "http://" + host + address
	if tc.host != "" {
		target = "http://" + tc.host + tc.path
	}
	req := httptest.NewRequest(tc.method, target, nil)
	if tc.host != "" {
		req.Host = tc.host
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set(httpx.RequestIDHeader, traceID)
	// A session cookie, so a guard that runs after the identity hook is reached at
	// all: the row about an authorizer that cannot answer is about the authorizer,
	// and an anonymous caller is refused two middleware earlier.
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func probeRequest(t *testing.T, router http.Handler, accept, hostname string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+hostname+"/ready", nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// problemBody is the exact bytes writeProblem encodes for a request that named the id
// the instance will carry — field order and all, as kit/problem declares them.
func problemBody(status int, detail string) string {
	body := `{"type":"about:blank","title":"` + http.StatusText(status) + `","status":` + itoa(status)
	if detail != "" {
		body += `,"detail":"` + detail + `"`
	}
	return body + `,"instance":"urn:request:` + traceID + `"}` + "\n"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

func trim(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

// broken is a check that cannot pass, with the name an operator reads.
type broken struct{ name string }

func (b broken) Name() string              { return b.name }
func (broken) Check(context.Context) error { return errors.New("dial tcp: connection refused") }

// ready is a check that passes.
type ready struct{}

func (ready) Name() string                { return "database" }
func (ready) Check(context.Context) error { return nil }

// probeHosts is a host loader that counts its calls: a probe that asks it anything is
// the pod-restart storm kit/health's comment describes.
type probeHosts struct {
	tenant tenancy.Tenant
	// loads counts the questions asked of this loader. A probe that asks it
	// anything is the pod-restart storm kit/health's comment describes, so the
	// count is the assertion and not a diagnostic.
	loads *atomic.Int32
}

func (p probeHosts) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	p.loads.Add(1)
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return p.tenant, nil
}

func (probeHosts) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return false, nil
}

// sites is health_test's answer to the same question, copied into this package's
// fixture shape: an authorizer that never says yes, because nothing reaching a probe
// is anybody.
type sites struct{ tenant tenancy.Tenant }

func (s sites) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return s.tenant, nil
}

func (sites) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) { return false, nil }

func anonymous(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
	return tenancy.Principal{}, false, nil
}

// kernelFault is the presentation layer's renderer, with the catalogue this
// repository ships: the page the table asserts is the page a shell draws.
func kernelFault() httpx.Fault {
	return uipage.FaultHandler(uipage.Shell{
		Chrome: uipage.Chrome{
			Brand:      "Acme",
			Assets:     "/admin/assets",
			Stylesheet: ui.Compose(design.Default()),
			SignIn:     "/admin/login",
		},
		Frame:     func(_ context.Context, _ uipage.Request, body []g.Node) g.Node { return uipage.Bare(body) },
		Back:      "/admin",
		BackLabel: "Back to the workspace",
		Granter:   uipage.Granter{Permission: "user:admin", Label: "managing people"},
		Ask:       "/admin/access-request",
		Messages:  xtext.Load("en", uipage.Catalogue()),
	})
}

// spentLimiter is the public write limit already reached, which is the state the
// 429 row needs without sixty-one requests through a real counter. It is the
// collaborator's failure, not a stubbed status: the guard still runs, still reads
// the answer, and still writes its own Retry-After and its own refusal.
type spentLimiter struct{}

func (spentLimiter) Allow(context.Context, string, int, time.Duration) (bool, time.Duration, error) {
	return false, 60 * time.Second, nil
}
