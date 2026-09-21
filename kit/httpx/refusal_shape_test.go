package httpx_test

// The guards inside the huma chain answer with API.refuse, and this is the file that
// holds what that means at the level of this package — the level the reviewer's case
// in ui/page cannot reach, because that one composes a shell and asks whether the
// person was shown a translated page.
//
// What is being held is one rule with two halves. A refusal that runs ahead of a
// handler — the authorization denial, the public write limit, a host that resolves to
// no site, a transaction that did not commit — is the same verdict in either shape:
// a page for a client that came to be *shown* the answer, the problem document for a
// client that asked for a value. The half that is easy to get wrong is the one an
// unwritten case hides: those guards answer inside a transaction that decides whether
// to commit from the status of the response, so a refusal written straight to the
// writer without saying its verdict to the context is rolled forward as an undecided
// response and replaced by a 500. The person asking for a page then gets an outage,
// which is the shape of the second review's finding 1 and the trap its own trial fix
// fell into. Every case below therefore asserts the status as loudly as the shape.
//
// The fourth review found the rule's other half unwritten: one *answer*. A refusal is
// one document — the renderer's page, or the one problem document httpx.Fault's opt-out
// promises — and the guards are held to it at both ends of that opt-out below, because
// refuse once asked the kernel-side writer and then wrote huma's copy after it, which
// is the same verdict twice in one body. The renderer is counted, so a row that never
// reached it fails instead of passing on a refusal this file is not about.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// entitlerFunc is the plan hook in the same shape as the loader and authorizer funcs
// this package's fixtures already use.
type entitlerFunc func(context.Context, tenancy.Tenant, string) (bool, error)

func (f entitlerFunc) Includes(ctx context.Context, t tenancy.Tenant, feature string) (bool, error) {
	return f(ctx, t, feature)
}

// guardSetup is one composed API with a guarded door, an anonymous public write and a
// renderer registered — the three refusals this file asks about, from one fixture, so
// that the shape each takes is a fact about the client and not about the route.
type guardSetup struct {
	who     tenancy.Tenant
	allow   bool
	limiter httpx.WriteLimiter
	// unresolvable stands for a host lookup that could not answer: an outage, and
	// not a site that does not exist.
	unresolvable bool
	// authErr, authnErr and planErr are the three outages a guard in the chain answers:
	// the roles table that cannot decide, the identity hook that cannot recognise the
	// caller, the plan that cannot be read. Each is a refusal of its own, so each is a
	// row in the table that walks the guards.
	authErr, authnErr, planErr error
	// feature puts a plan question on the guarded door and include is the plan's answer.
	feature string
	include bool
	// conflict mounts a door whose two inserts only collide at COMMIT, which is the
	// limb where the kernel holds a finished response and has to replace it.
	conflict bool
}

func guardKernel(t *testing.T, f guardSetup, fault httpx.Fault) (*httpx.API, *chi.Mux) {
	t.Helper()
	owner, app := dbtest.Schema(t)
	f.who = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Installation: installationHost, Conn: app,
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
			if f.unresolvable {
				return tenancy.Tenant{}, errors.New("the tenant store is not answering")
			}
			if h == host || h == installationHost {
				return f.who, nil
			}
			return tenancy.Tenant{}, tenancy.ErrNoSuchHost
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return f.allow, f.authErr
		}),
		Entitle: entitlerFunc(func(_ context.Context, _ tenancy.Tenant, _ string) (bool, error) {
			return f.include, f.planErr
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			if f.authnErr != nil {
				return tenancy.Principal{}, false, f.authnErr
			}
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		WriteLimiter: f.limiter,
		Log:          slog.New(slog.DiscardHandler),
		Fault:        fault,
	})
	plans := httpx.Permission("billing:read")
	if f.feature != "" {
		plans = plans.Needing(f.feature)
	}
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "shape-plans", Method: http.MethodGet, Path: "/plans",
	}, plans, ok)
	httpx.Register(api.Surfaces(probe).Ops, huma.Operation{
		OperationID: "shape-operate", Method: http.MethodGet, Path: "/all",
	}, httpx.OperatorPermission("billing:operate"), ok)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "shape-plan-write", Method: http.MethodPost, Path: "/plans",
	}, httpx.Permission("billing:write"), ok)
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "shape-ask", Method: http.MethodPost, Path: "/ask",
	}, httpx.Public(), ok)
	if f.conflict {
		// A unique constraint the transaction only discovers at COMMIT: the handler
		// answered 200 and the database then said no, which is the refusal written over
		// a response the kernel was holding. Compare
		// TestAFailedCommitDoesNotKeepTheHandlersHeaders, which asks the same door with
		// no renderer registered.
		if _, err := owner.Exec(`CREATE TABLE shape_conflicts (id serial PRIMARY KEY, tenant_id uuid NOT NULL,
			body text NOT NULL, CONSTRAINT shape_conflicts_unique UNIQUE (tenant_id, body) DEFERRABLE INITIALLY DEFERRED)`); err != nil {
			t.Fatalf("the fixture cannot build its deferred constraint: %v", err)
		}
		httpx.Register(api.Surfaces(probe).App, huma.Operation{
			OperationID: "shape-conflict", Method: http.MethodPost, Path: "/conflict",
		}, httpx.Permission("billing:write"), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			tx, _ := httpx.TxFrom(ctx)
			for range 2 {
				if err := tx.DB().Exec("INSERT INTO shape_conflicts (tenant_id, body) VALUES (?, ?)",
					f.who.ID.String(), "the same row twice").Error; err != nil {
					return nil, err
				}
			}
			return &struct{}{}, nil
		})
	}
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the fixture does not describe itself: %v", err)
	}
	return api, router
}

// askFor sends the same request twice, once as a browser and once as a client, which is
// the only way to say "the shape is the caller's choice" in one place.
func askFor(t *testing.T, router http.Handler, method, path, accept string) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader("{}")
	}
	r := httptest.NewRequest(method, "http://"+host+path, body)
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("Accept", accept)
	r.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

// TestAGrantDenialTakesTheShapeTheCallerAskedFor. The person who navigated to a page
// they hold no grant for is the reader ui/page's faultKeys table exists for, and the
// six session codes in it travel on this one refusal. Before this file's rule they
// arrived as huma's JSON, because the guard ran inside the chain and answered there.
func TestAGrantDenialTakesTheShapeTheCallerAskedFor(t *testing.T) {
	f := guardSetup{allow: true}
	api, router := guardKernel(t, f, documentFault)
	guarded := at(api, "/plans")

	// Reachability, and it is the grant and nothing else that differs between this
	// request and every refusal below: the address is served and this caller holds it.
	if got := askFor(t, router, http.MethodGet, guarded, "application/json"); got.Code != http.StatusOK {
		t.Fatalf("the guarded door for a caller who holds the grant = %d; this case is about that door: %s",
			got.Code, got.Body.String())
	}

	denied := guardSetup{allow: false}
	_, refused := guardKernel(t, denied, documentFault)

	page := askFor(t, refused, http.MethodGet, guarded, browserAccept)
	if page.Code != http.StatusForbidden {
		t.Fatalf("a navigating caller without the grant = %d, want the verdict 403 and not a 500 standing in for "+
			"an undecided response: %s", page.Code, page.Body.String())
	}
	if ct := page.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("a navigating caller without the grant was handed %s: %s", ct, page.Body.String())
	}
	for _, want := range []string{httpx.CodeDenied + ":", "urn:request:"} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("the denial page omits %q, which is the half of the line support and the log match on: %s",
				want, page.Body.String())
		}
	}

	value := askFor(t, refused, http.MethodGet, guarded, "application/json")
	if value.Code != http.StatusForbidden || !strings.HasPrefix(value.Header().Get("Content-Type"), "application/problem+json") ||
		!strings.Contains(value.Body.String(), httpx.CodeDenied) {
		t.Errorf("a client that asked for a value got %d %s %s; it has to keep getting the document with the code in it",
			value.Code, value.Header().Get("Content-Type"), value.Body.String())
	}
}

// TestThePublicWriteLimitRefusesInTheShapeTheVisitorAsked. The public surface is the one
// surface with no account to lock out, so the person this limit answers is the one
// person with no session, no tenant of their own and no log entry of their own to be
// told through. They were told in JSON.
func TestThePublicWriteLimitRefusesInTheShapeTheVisitorAsked(t *testing.T) {
	counted := &window{allow: 0}
	f := guardSetup{allow: true, limiter: counted}
	api, router := guardKernel(t, f, documentFault)
	door := publicly(api, "/ask")

	full := askFor(t, router, http.MethodPost, door, browserAccept)
	if full.Code != http.StatusTooManyRequests {
		t.Fatalf("a public write past the limit = %d, want 429 and not a 500: %s", full.Code, full.Body.String())
	}
	if ct := full.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("a browser past the limit was handed %s: %s", ct, full.Body.String())
	}
	if full.Header().Get("Retry-After") == "" {
		t.Error("the page refuses without saying when to come back")
	}
	if !strings.Contains(full.Body.String(), httpx.CodeLimitExhausted+":") {
		t.Errorf("the refusal names no code a shell can hang a sentence on: %s", full.Body.String())
	}

	value := askFor(t, router, http.MethodPost, door, "application/json")
	if value.Code != http.StatusTooManyRequests ||
		!strings.HasPrefix(value.Header().Get("Content-Type"), "application/problem+json") ||
		!strings.Contains(value.Body.String(), httpx.CodeLimitExhausted) || value.Header().Get("Retry-After") == "" {
		t.Errorf("a client past the limit got %d %s %s; the document and its Retry-After have to survive",
			value.Code, value.Header().Get("Content-Type"), value.Body.String())
	}
}

// TestAnUnresolvableHostIsRefusedInTheShapeAsked. The guards that answer an outage — a
// host the tenant store could not resolve — are the same rule: a person waiting at a
// hostname gets a page that says so, a monitor gets the document it parses. The 503
// carries no code, and this package's own answer for that is the kernel's sentence,
// which is what the page shows.
func TestAnUnresolvableHostIsRefusedInTheShapeAsked(t *testing.T) {
	api, router := guardKernel(t, guardSetup{allow: true, unresolvable: true}, documentFault)

	page := askFor(t, router, http.MethodGet, at(api, "/plans"), browserAccept)
	if page.Code != http.StatusServiceUnavailable {
		t.Fatalf("an unresolvable host = %d for a browser, want the verdict 503 and not a 500: %s", page.Code, page.Body.String())
	}
	if ct := page.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("a browser at a host that will not resolve was handed %s: %s", ct, page.Body.String())
	}
	if page.Header().Get("Retry-After") == "" {
		t.Error("the outage page says nothing about trying again")
	}

	value := askFor(t, router, http.MethodGet, at(api, "/plans"), "application/json")
	if value.Code != http.StatusServiceUnavailable ||
		!strings.HasPrefix(value.Header().Get("Content-Type"), "application/problem+json") {
		t.Errorf("a monitor at the same host got %d %s; a probe has to keep reading a document it can parse",
			value.Code, value.Header().Get("Content-Type"))
	}
}

// TestAnHTMXFormWriteRefusedByAGuardIsTheDocumentItsControllerParses. The generated
// screens write through htmx, and htmx asks with `Accept: text/html,*/*` — so the shape
// rule needs to know that a controller in a page is not a person who navigated.
// ui/assets/js/htmx-config.js swaps nothing for a 4xx and reads the code out of the
// problem body to choose the recovery notice and keep the form's unsaved input; the
// journeys in e2e/session-recovery.spec.ts are that contract being used.
func TestAnHTMXFormWriteRefusedByAGuardIsTheDocumentItsControllerParses(t *testing.T) {
	api, router := guardKernel(t, guardSetup{allow: false}, documentFault)

	req := httptest.NewRequest(http.MethodPost, "http://"+host+at(api, "/plans"), strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/html,*/*") // htmx's own default
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	got := httptest.NewRecorder()
	router.ServeHTTP(got, req)

	if got.Code != http.StatusForbidden {
		t.Fatalf("an htmx write without the grant = %d: %s", got.Code, got.Body.String())
	}
	if ct := got.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("an htmx write refused by a guard was handed %s; htmx discards a 4xx body, so the code would reach nobody: %s",
			ct, got.Body.String())
	}
	if !strings.Contains(got.Body.String(), httpx.CodeDenied) {
		t.Errorf("the refusal names no code for the controller to classify: %s", got.Body.String())
	}
}

// TestAGuardRefusalWithNoRendererIsTheDocumentItAlwaysWas. Every one of the guards above
// answers through the same call now, so the default has to be the old answer for all of
// them — an application that registers no renderer is not being asked to render.
func TestAGuardRefusalWithNoRendererIsTheDocumentItAlwaysWas(t *testing.T) {
	api, router := guardKernel(t, guardSetup{allow: false, limiter: &window{allow: 0}}, nil)

	for _, one := range []struct{ name, method, path string }{
		{"the denial", http.MethodGet, at(api, "/plans")},
		{"the public door", http.MethodPost, publicly(api, "/ask")},
	} {
		got := askFor(t, router, one.method, one.path, browserAccept)
		if !strings.HasPrefix(got.Header().Get("Content-Type"), "application/problem+json") {
			t.Errorf("%s: an application with no renderer answered a browser with %s, want the body it has always answered with",
				one.name, got.Header().Get("Content-Type"))
		}
	}
}

// countedFault is a presentation layer with a tally. `answer` decides whether it takes
// the opt-out httpx.Fault documents — returning false, which the same type promises
// falls back to the problem JSON. `calls` says the guard reached it: a case about the
// answer a renderer is given has to establish that the renderer was asked before it
// asserts what it was given, and that tally is what turns "every guard in the chain"
// from a list into a checked claim about this package's one writer.
type countedFault struct {
	answer bool
	calls  int
}

func (f *countedFault) render(w http.ResponseWriter, r *http.Request, p *problem.Problem) bool {
	f.calls++
	if !f.answer {
		return false
	}
	return documentFault(w, r, p)
}

// guardAsk is askFor with the two things a table of guards needs: the host the request
// names, which is how a row reaches the gate of a site that is not served, and whether
// the caller presents a session at all, which is how a row reaches the door that asks
// for somebody and finds nobody.
func guardAsk(t *testing.T, router http.Handler, method, authority, path string, anonymous bool) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if method != http.MethodGet {
		body = strings.NewReader("{}")
	}
	r := httptest.NewRequest(method, "http://"+authority+path, body)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("Accept", browserAccept)
	if !anonymous {
		r.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

// oneAnswer is the fourth review's finding in one place: a guard decides one verdict and
// writes one answer. Either the renderer's page, or the one problem document a declining
// renderer opted into — never the page and the document, and never the document twice.
func oneAnswer(t *testing.T, res *httptest.ResponseRecorder, want int, code string, page bool, asked int) {
	t.Helper()
	if asked == 0 {
		t.Fatal("the refusal never reached the registered renderer, so it says nothing about the answer a renderer is given")
	}
	if res.Code != want {
		t.Fatalf("%s = %d, want the verdict the guard decided: %s", http.StatusText(want), res.Code, res.Body.String())
	}
	body := res.Body.String()
	if code != "" && !strings.Contains(body, code) {
		t.Errorf("the refusal dropped %s on the way out: %s", code, body)
	}
	if page {
		if ct := res.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("a refusal a renderer answered came back as %s: %s", ct, body)
		}
		if n := strings.Count(body, "<!doctype html>"); n != 1 {
			t.Errorf("the body holds %d pages, want one: %s", n, body)
		}
		return
	}
	if ct := res.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("a refusal the renderer declined came back as %s: %s", ct, body)
	}
	var declared map[string]any
	if err := json.Unmarshal([]byte(body), &declared); err != nil {
		t.Fatalf("the body is not one problem document (%v); it holds %q", err, body)
	}
	if got := declared["status"]; got != float64(want) {
		t.Errorf("the fallback document says %v, want the verdict %d", got, want)
	}
}

// TestEveryGuardRefusalInTheChainIsOneDocument. The guards inside the huma chain answer
// through refuse, and refuse used to ask the kernel-side writer (fail) and then write
// huma's own error after it: for a composition whose renderer took the opt-out
// httpx.Fault documents, that is one body holding two problem documents, which is no
// JSON at all. Both ends of the opt-out are asked of every guard this fixture can
// compose, and the renderer is counted on every row — a guard that answered on some
// other way out fails its row for never having asked, rather than passing on a refusal
// this file is not about.
//
// Two limbs of refuse are not in the table, for one reason each rather than none: the
// transaction that could not be opened (db.Lazy refuses only a request with no tenant
// and no system token, neither of which a composed API can present — the first skips
// this middleware and the second is built at httpx.New) and the streamed body whose
// commit failed, which needs a database that dies between the handler's last byte and
// the COMMIT. TestAStreamedResponseCommits holds the streaming path's commit, and
// kit/db's own suite holds a begin that fails.
func TestEveryGuardRefusalInTheChainIsOneDocument(t *testing.T) {
	for _, one := range []struct {
		name string
		give guardSetup
		// surface, method and path name the door the request is sent to; authority is
		// the host it names, empty for the tenant's own host.
		surface, method, path, authority, code string
		anonymous                              bool
		want                                   int
	}{
		{
			name: "the caller who holds no grant", give: guardSetup{allow: false},
			surface: "app", method: http.MethodGet, path: "/plans",
			want: http.StatusForbidden, code: httpx.CodeDenied,
		},
		{
			name: "a door that asks for somebody and found nobody", give: guardSetup{allow: true},
			surface: "app", method: http.MethodGet, path: "/plans", anonymous: true,
			want: http.StatusForbidden, code: httpx.CodeAnonymous,
		},
		{
			name: "the roles table that could not decide", give: guardSetup{authErr: errors.New("the roles table is not answering")},
			surface: "app", method: http.MethodGet, path: "/plans",
			want: http.StatusServiceUnavailable,
		},
		{
			name: "a plan that excludes the feature", give: guardSetup{allow: true, feature: "audit-trail"},
			surface: "app", method: http.MethodGet, path: "/plans",
			want: http.StatusPaymentRequired, code: httpx.CodePlanExcludes,
		},
		{
			name: "a plan that could not be read", give: guardSetup{allow: true, feature: "audit-trail", planErr: errors.New("the subscription store is unreachable")},
			surface: "app", method: http.MethodGet, path: "/plans",
			want: http.StatusServiceUnavailable,
		},
		{
			name: "a host this installation serves no site at", give: guardSetup{allow: true},
			surface: "app", method: http.MethodGet, path: "/plans", authority: "nobody.test",
			want: http.StatusNotFound,
		},
		{
			name: "a host that would not resolve at all", give: guardSetup{allow: true, unresolvable: true},
			surface: "app", method: http.MethodGet, path: "/plans",
			want: http.StatusServiceUnavailable,
		},
		{
			name: "the identity hook that could not recognise the caller", give: guardSetup{allow: true, authnErr: errors.New("the session store is not answering")},
			surface: "app", method: http.MethodGet, path: "/plans",
			want: http.StatusInternalServerError,
		},
		{
			// The control plane is served at this host, so the address is mounted; what
			// refuses the caller is that the tenant the host resolved to is not the
			// operator's, and the answer is the same 404 an unmounted address gives.
			name:    "a door only the operator may open, asked at another tenant",
			give:    guardSetup{allow: true},
			surface: "ops", method: http.MethodGet, path: "/all", authority: installationHost,
			want: http.StatusNotFound,
		},
		{
			name: "the anonymous writes that ran out", give: guardSetup{allow: true, limiter: &window{allow: 0}},
			surface: "public", method: http.MethodPost, path: "/ask",
			want: http.StatusTooManyRequests, code: httpx.CodeLimitExhausted,
		},
		{
			name: "the transaction that did not commit", give: guardSetup{allow: true, conflict: true},
			surface: "app", method: http.MethodPost, path: "/conflict",
			want: http.StatusInternalServerError,
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			for _, shape := range []struct {
				name   string
				answer bool
			}{
				{"answered with a page", true},
				{"declined and answered with the document", false},
			} {
				t.Run(shape.name, func(t *testing.T) {
					renderer := &countedFault{answer: shape.answer}
					api, router := guardKernel(t, one.give, renderer.render)
					door := api.Surfaces(probe).App.Path(one.path)
					switch one.surface {
					case "public":
						door = api.Surfaces(probe).Public.Path(one.path)
					case "ops":
						door = api.Surfaces(probe).Ops.Path(one.path)
					}
					authority := one.authority
					if authority == "" {
						authority = host
					}
					oneAnswer(t, guardAsk(t, router, one.method, authority, door, one.anonymous),
						one.want, one.code, shape.answer, renderer.calls)
				})
			}
		})
	}
}

// TestAGuardRefusalTheRendererDeclinesIsOneDocumentOfTheLengthItPromises. A recorder
// shows one body; a server shows what a client reads — the status, the media type, a
// Content-Length in front of the bytes, and a body a parser accepts whole. The doubled
// answer broke the last of those: two documents under one length, and reading the whole
// of what the client was promised came back "invalid character '{' after top-level
// value", a refusal no client can classify. The length is asserted alongside it because
// the review measured the defect at the wire and this is the case that keeps measuring
// it there rather than in a recorder.
func TestAGuardRefusalTheRendererDeclinesIsOneDocumentOfTheLengthItPromises(t *testing.T) {
	api, router := guardKernel(t, guardSetup{allow: false, limiter: &window{allow: 0}}, (&countedFault{}).render)
	server := httptest.NewServer(router)
	defer server.Close()

	for _, one := range []struct {
		name, method, path string
		want               int
	}{
		{"the authorization denial", http.MethodGet, at(api, "/plans"), http.StatusForbidden},
		{"the public write limit", http.MethodPost, publicly(api, "/ask"), http.StatusTooManyRequests},
	} {
		var payload io.Reader
		if one.method != http.MethodGet {
			payload = strings.NewReader("{}")
		}
		req, err := http.NewRequest(one.method, server.URL+one.path, payload)
		if err != nil {
			t.Fatalf("%s: %v", one.name, err)
		}
		// The server answers at whatever host the request names, and this installation
		// serves its tenant at host; 127.0.0.1:port would be refused for reasons that
		// have nothing to do with the answer under test.
		req.Host = host
		req.Header.Set("Accept", browserAccept)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})

		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("%s: %v", one.name, err)
		}
		body, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			t.Fatalf("%s: %v", one.name, readErr)
		}
		if res.StatusCode != one.want {
			t.Errorf("%s = %d, want the verdict the guard decided: %q", one.name, res.StatusCode, body)
		}
		if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
			t.Errorf("%s came back as %s: %q", one.name, ct, body)
		}
		if got := res.Header.Get("Content-Length"); got != strconv.Itoa(len(body)) {
			t.Errorf("%s: Content-Length %s in front of %d bytes of body — a reader stops where it was told to, so a body longer than its own promise is a refusal with part of it unread: %q",
				one.name, got, len(body), body)
		}
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Errorf("%s: the wire carries no single problem document (%v): %q", one.name, err, body)
			continue
		}
		if got := doc["status"]; got != float64(one.want) {
			t.Errorf("%s: the document says %v, want the verdict %d", one.name, got, one.want)
		}
	}
}
