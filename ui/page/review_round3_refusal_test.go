package page_test

// Reviewer's cases for the third review of T-0024 (three surfaces by path).
// Reviewer: a fresh pi session, 2026-09-21.
//
// Round 5 closed the second review's finding 1 by translating the refusal page. Every case
// that proves it — ui/page/fault_language_test.go and kit/httpx/review_round2_surfaces_test.go
// — builds a problem by hand and hands it to the renderer. Both are true whatever the guards
// do with the request that would have produced it. These cases ask the composed kernel
// instead: the real chains, the real guards, the real public write limit, and this package's
// renderer wired the way apps/platformkit/fault.go wires it. That is the level at which a
// person either sees a page in the language they asked for or does not.
//
// The first case is the pin: the second review's finding 3 (a refusal outside the host's
// chain) and finding 1 (an English-only refusal page) meet at the two addresses round 5
// changed — an address nobody mounted and a miss inside a mounted tree — and both hold when
// the question comes through the router. The rest are the refusals whose answer never
// reaches this package at all, because the guard that refuses them answers with
// huma.WriteErr, which does not consult httpx.Options.Fault: the authorization denial
// (six of the eight rows of fault.go's new table) and the public write limit.
//
// Each case reaches its assertion through what the fixed behaviour prints — a status, a
// media type, the shell's own sentence, the declared language — and through a reachability
// claim that is true before and after any fix (the same address answering 200 for a caller
// who is allowed, or for the verb it takes).

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/text/language"
	"golang.org/x/text/message/catalog"

	g "maragu.dev/gomponents"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
)

const (
	refusalHost = "demo.localhost"
	// refusalAskedFor is the language a person in this fixture speaks. Every assertion below
	// is about whether the answer they get is written in it.
	refusalAskedFor = "pt-PT"
	// refusalGrant is the permission the guarded page declares, and the name the kernel puts
	// in the denial it writes for a caller who does not hold it.
	refusalGrant = "billing:read"
)

// refusalSentences is what this shell ships, in the language it ships them in. The keys are
// the ones ui/page/fault.go's own rules produce — fault.<CODE> for a refusal carrying a
// published code, fault.<status> for the kernel's own verdicts — including the two the
// table and the status rule leave out, which is what the cases below are about.
var refusalSentences = map[string]string{
	"fault.AUTH_DENIED":     "Não pode fazer isto.",
	"fault.LIMIT_EXHAUSTED": "Demasiados envios a partir deste endereço.",
	"fault.404":             "Não há nada para ver nesta morada.",
	"fault.405":             "Este endereço não aceita este pedido.",
}

// refusalDirectory resolves every host to one tenant: these cases are about the answer, not
// about finding the address.
type refusalDirectory struct{}

func (refusalDirectory) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return tenancy.Tenant{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Slug: "acme", Name: "Acme"}, nil
}

// refusalAuthorizer answers the question the authorization guard asks. It is the only lever
// the cases need: the same navigation, same caller, same address, with and without the grant.
type refusalAuthorizer struct{ allow bool }

func (r refusalAuthorizer) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return r.allow, nil
}

// refusalFixture is one composed application: the guarded workspace page, a public write a
// person could fill in, a file tree, and this package's renderer as the failure page.
type refusalFixture struct {
	// granted is what the authorizer answers for the guarded page.
	granted bool
	// signedIn recognises the caller, the way a presented session cookie does.
	signedIn bool
	// writes counts anonymous public writes; nil leaves them uncounted.
	writes httpx.WriteLimiter
}

func refusalServer(t *testing.T, f refusalFixture) http.Handler {
	t.Helper()
	_, conn := dbtest.Schema(t)
	shell := refusalShell(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: refusalHost, Tenants: refusalDirectory{}, Authorize: refusalAuthorizer{allow: f.granted},
		Conn: conn, WriteLimiter: f.writes, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			if !f.signedIn {
				return tenancy.Principal{}, false, nil
			}
			return tenancy.Principal{UserID: uuid.MustParse("22222222-2222-4222-8222-222222222222")}, true, nil
		},
		Fault: page.FaultHandler(shell),
	})
	guarded := func(context.Context, page.Request, *struct{}) (page.View, error) {
		return page.View{Title: "Plans"}, nil
	}
	asked := func(context.Context, page.Request, *struct{}) (page.View, error) {
		return page.View{Title: "Obrigado"}, nil
	}
	page.Serve(api.Surfaces("billing").App, shell,
		page.Route{ID: "r3-plans", Method: http.MethodGet, Path: "/plans", Summary: "Plans"},
		httpx.Permission(refusalGrant), guarded)
	page.Serve(api.Surfaces("contact").Public, shell,
		page.Route{ID: "r3-ask", Method: http.MethodPost, Path: "/ask", Summary: "Ask us"},
		httpx.Public(), asked)
	api.Surfaces("admin").App.Static("/assets", fstest.MapFS{
		"app.css": &fstest.MapFile{Data: []byte(":root{--r3:1}")},
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the fixture does not describe itself: %v", err)
	}
	return router
}

// refusalShell is a shell that ships its sentences in two languages and has a frame, which is
// the condition FaultHandler requires to answer a browser with a page at all.
func refusalShell(t *testing.T) page.Shell {
	t.Helper()
	builder := catalog.NewBuilder(catalog.Fallback(language.English))
	for key, sentence := range refusalSentences {
		if err := builder.SetString(language.EuropeanPortuguese, key, sentence); err != nil {
			t.Fatal(err)
		}
	}
	s := shell()
	s.Messages = page.FromCatalog(builder)
	s.Frame = func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) }
	return s
}

// navigate asks for an answer the way a browser does: a document, in the language the person
// set, optionally holding this installation's session cookie.
func navigate(t *testing.T, h http.Handler, method, path string, withSession bool, accept string) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader = strings.NewReader("")
	if method == http.MethodPost {
		body = strings.NewReader(`{}`)
	}
	r := httptest.NewRequest(method, "http://"+refusalHost+path, body)
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("Accept", accept)
	r.Header.Set("Accept-Language", refusalAskedFor)
	if withSession {
		r.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const (
	plansPath  = "/app/billing/plans"
	askPath    = "/contact/ask"
	absentPage = "/app/billing/nowhere"
	absentFile = "/app/admin/assets/absent.css"
	// refusalSaid is what a refusal page has to say to be this shell's: the way out it offers.
	refusalWayOut = `href="/admin"`
)

// TestTheRefusalOfAnAddressNobodyMountedSpeaksTheRequestThroughTheRouter is the pin, and it
// passes: the second review's two findings meet at these two addresses, and what round 5
// shipped holds when the question is asked of the router rather than of the renderer. An
// address nobody mounted and a file a mounted tree does not hold are the same refusal of the
// same host, in the language the request brought, with the headers that say so and the
// caching that says a refusal is nobody's to keep.
func TestTheRefusalOfAnAddressNobodyMountedSpeaksTheRequestThroughTheRouter(t *testing.T) {
	h := refusalServer(t, refusalFixture{granted: true, signedIn: true})

	// Reachability: the page that is mounted answers, and the file the tree holds answers.
	for _, want := range []struct{ path, verb string }{{plansPath, http.MethodGet}, {"/app/admin/assets/app.css", http.MethodGet}} {
		if got := navigate(t, h, want.verb, want.path, true, "text/html"); got.Code != http.StatusOK {
			t.Fatalf("%s %s = %d; this case is about the addresses beside these; %s", want.verb, want.path, got.Code, firstLine(got.Body.String()))
		}
	}

	for _, at := range []struct{ what, path string }{
		{"an address nobody mounted", absentPage},
		{"a file a mounted tree does not hold", absentFile},
	} {
		got := navigate(t, h, http.MethodGet, at.path, false, "text/html")
		if got.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want the host's own 404: %s", at.what, got.Code, firstLine(got.Body.String()))
		}
		if !strings.HasPrefix(got.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s answered a navigating browser with %s, not a page: %s", at.what, got.Header().Get("Content-Type"), firstLine(got.Body.String()))
		}
		body := got.Body.String()
		if want := refusalSentences["fault.404"]; !strings.Contains(body, want) {
			t.Errorf("%s did not show the sentence this shell ships for a 404 (%q); this shell ships it in %s: %s",
				at.what, want, refusalAskedFor, firstLine(body))
		}
		if !strings.Contains(body, `lang="`+refusalAskedFor+`"`) {
			t.Errorf("%s declares no %s: %s", at.what, refusalAskedFor, firstLine(body))
		}
		if got.Header().Get("Content-Language") != refusalAskedFor || got.Header().Get("Vary") != "Accept-Language" {
			t.Errorf("%s says %q %q about the language it varies on", at.what,
				got.Header().Get("Content-Language"), got.Header().Get("Vary"))
		}
		if got.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s says %q about caching; a refusal is nobody's to keep", at.what, got.Header().Get("Cache-Control"))
		}
		if !strings.Contains(body, refusalWayOut) {
			t.Errorf("%s offers no way out: %s", at.what, firstLine(body))
		}
	}
}

// TestAGrantDenialOfAPersonWhoIsSignedInIsTheShellsPageInTheRequestsLanguage.
//
// apps/platformkit/fault.go: "The application, not kit/app, decides this: a failure page is
// chrome… and every guard in the kernel then answers a navigating client with this shell's
// page" (ui/page/fault.go). ui/page/fault.go's new table names six refusals of the session
// and the permission — AUTH_DENIED among them — and the second review's finding 1 was closed
// by translating them.
//
// The guard that asks for a permission refuses with huma.WriteErr
// (kit/httpx/middleware.go:1024 → :1213), and Options.Fault is consulted in exactly one
// place, kit/httpx/fault.go:52. A person who is signed in, lacks the grant, and navigated,
// therefore gets application/problem+json in the browser window: the page this package ships
// is never reached, so fault.AUTH_DENIED and its five siblings are rows no shell can fill.
//
// The fix has a passing branch in both halves of this case: refuse a navigating client the
// way csrf, methodNotAllowed and root.NotFound already refuse one — through a.fail — and
// keep the problem document for the client that asked for a value, which the second half
// asserts so the fix cannot be "answer everybody with a page".
func TestAGrantDenialOfAPersonWhoIsSignedInIsTheShellsPageInTheRequestsLanguage(t *testing.T) {
	// Reachability, with none of the refusal's own answer in it: this is the address that
	// asks for the grant, and this caller holds it. True today and after any fix.
	allowed := refusalServer(t, refusalFixture{granted: true, signedIn: true})
	if got := navigate(t, allowed, http.MethodGet, plansPath, true, "text/html"); got.Code != http.StatusOK {
		t.Fatalf("the guarded page for a caller who holds %s = %d; this case is about that page: %s",
			refusalGrant, got.Code, firstLine(got.Body.String()))
	}

	refused := refusalServer(t, refusalFixture{granted: false, signedIn: true})
	got := navigate(t, refused, http.MethodGet, plansPath, true, "text/html")
	if got.Code != http.StatusForbidden {
		t.Fatalf("a caller without %s = %d, want the guard's refusal; this case is about what they are shown: %s",
			refusalGrant, got.Code, firstLine(got.Body.String()))
	}
	body := got.Body.String()
	if !strings.HasPrefix(got.Header().Get("Content-Type"), "text/html") {
		t.Errorf("a person without %s, navigating, was handed %s and not the shell's page: %s",
			refusalGrant, got.Header().Get("Content-Type"), strings.TrimSpace(body))
	}
	for _, want := range []string{
		refusalSentences["fault.AUTH_DENIED"], // the sentence this shell ships for the code
		`lang="` + refusalAskedFor + `"`,      // and says which language it is in
		refusalWayOut,                         // one way out
		httpx.CodeDenied + ":",                // the code a person reads back to support
		"(request ",                           // the reference an operator finds the log line by
		`href="/admin/assets/`,                // the shell's own stylesheet
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the denial page this composition should serve omits %q: %s", want, firstLine(body))
		}
	}
	if got.Header().Get("Content-Language") != refusalAskedFor || got.Header().Get("Vary") != "Accept-Language" {
		t.Errorf("the denial says %q %q about the language it varies on",
			got.Header().Get("Content-Language"), got.Header().Get("Vary"))
	}

	// The same refusal, for the client that asked for a value: the document, unchanged, with
	// the code in its detail. This half passes today and has to keep passing.
	value := navigate(t, refused, http.MethodGet, plansPath, true, "application/json")
	if value.Code != http.StatusForbidden || !strings.Contains(value.Body.String(), httpx.CodeDenied) ||
		!strings.Contains(value.Header().Get("Content-Type"), "problem+json") {
		t.Errorf("a client that asked for a value got %d %s %s; it has to keep getting the document with the code in it",
			value.Code, value.Header().Get("Content-Type"), strings.TrimSpace(value.Body.String()))
	}
}

// TestThePublicWriteLimitRefusalIsTheShellsPage. The public surface is the one surface with
// no account to lock out, which is why the brief puts a rate limit on its writes and why the
// person who meets that limit is the one person this kernel refuses without a session, a
// tenant of their own or an account to be told anything to. They are refused by
// kit/httpx/middleware.go:1181, with huma.WriteErr. apps/platformkit/app_fault_test.go's own
// standard is the one this case applies to them: "a person who navigated and gets JSON" is
// the defect.
func TestThePublicWriteLimitRefusalIsTheShellsPage(t *testing.T) {
	h := refusalServer(t, refusalFixture{writes: limit.Memory()})

	// Reachability: the first anonymous write is answered by the handler. True today and
	// after any fix — this is the door, and it is open to the anonymous caller up to the
	// count the surface keeps.
	first := navigate(t, h, http.MethodPost, askPath, false, "text/html")
	if first.Code != http.StatusOK {
		t.Fatalf("the first anonymous public write = %d; this case is about that door: %s", first.Code, firstLine(first.Body.String()))
	}

	var last *httptest.ResponseRecorder
	for range 70 {
		last = navigate(t, h, http.MethodPost, askPath, false, "text/html")
		if last.Code == http.StatusTooManyRequests {
			break
		}
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("70 anonymous public writes and the surface's limit never answered; last was %d", last.Code)
	}
	if last.Header().Get("Retry-After") == "" {
		t.Errorf("the refusal gives no Retry-After, so the person has no idea whether to wait or go away")
	}
	body := last.Body.String()
	if !strings.HasPrefix(last.Header().Get("Content-Type"), "text/html") {
		t.Errorf("a navigating browser past the limit was handed %s and not the shell's page: %s",
			last.Header().Get("Content-Type"), strings.TrimSpace(body))
	}
	for _, want := range []string{"LIMIT_EXHAUSTED", refusalWayOut, "(request "} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal this person is shown omits %q: %s", want, firstLine(body))
		}
	}
}

// TestThePublicWriteLimitRefusalSpeaksTheLanguageTheRequestBrought is the same refusal's
// other half, and it fails for a second, separate reason: ui/page/fault.go looks up the
// prefix of "<CODE>: <sentence>" in one table keyed by httpx's published Code* constants, and
// the limit's refusal carries a code no constant publishes (grep LIMIT_EXHAUSTED in
// kit/httpx: one string literal in a WriteErr call and no constant). So even once the
// refusal reaches the page, the shell that ships the sentence has nothing to ship it under.
func TestThePublicWriteLimitRefusalSpeaksTheLanguageTheRequestBrought(t *testing.T) {
	h := refusalServer(t, refusalFixture{writes: limit.Memory()})
	var last *httptest.ResponseRecorder
	for range 70 {
		last = navigate(t, h, http.MethodPost, askPath, false, "text/html")
		if last.Code == http.StatusTooManyRequests {
			break
		}
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("the surface's limit never answered; last was %d", last.Code)
	}
	body := last.Body.String()
	if want := refusalSentences["fault.LIMIT_EXHAUSTED"]; !strings.Contains(body, want) {
		t.Errorf("a pt-PT person past the public write limit was not answered in pt-PT; this shell ships %q under "+
			"fault.LIMIT_EXHAUSTED and the refusal page shows the kernel's English instead: %s", want, firstLine(body))
	}
	if !strings.Contains(body, `lang="`+refusalAskedFor+`"`) {
		t.Errorf("the refusal declares no %s although the shell ships this refusal: %s", refusalAskedFor, firstLine(body))
	}
}

// TestTheKernelsOwnMethodNotAllowedSentenceIsTranslatable. ui/page/fault.go limits the
// status-keyed sentences to the 404 and the 500 on the stated ground that "A 400, a 409 or a
// 422 carries a sentence about the caller's own request, written by a module or by the
// decoder, and translating that is the writer's share". The 405 is not one of those: its
// sentence, "this address does not accept POST requests", is written by kit/httpx's own
// methodNotAllowed and by nobody else — it is the same kind of copy as the 404 and the 500,
// from the same writer, on the same page. This case asks for the sentence this shell ships
// under fault.405 and gets the kernel's English.
func TestTheKernelsOwnMethodNotAllowedSentenceIsTranslatable(t *testing.T) {
	h := refusalServer(t, refusalFixture{granted: true, signedIn: true})

	// Reachability: the address is served and takes the verb it takes.
	if got := navigate(t, h, http.MethodGet, plansPath, true, "text/html"); got.Code != http.StatusOK {
		t.Fatalf("GET %s = %d; this case is about the verb it does not take: %s", plansPath, got.Code, firstLine(got.Body.String()))
	}

	got := navigate(t, h, http.MethodPost, plansPath, true, "text/html")
	if got.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST %s = %d, want the refusal of a verb this address does not take: %s", plansPath, got.Code, firstLine(got.Body.String()))
	}
	body := got.Body.String()
	if want := refusalSentences["fault.405"]; !strings.Contains(body, want) {
		t.Errorf("a pt-PT person told a verb this address does not take was not told in pt-PT; this shell ships %q "+
			"under fault.405 and the page shows the kernel's English sentence: %s", want, firstLine(body))
	}
	if !strings.Contains(body, `lang="`+refusalAskedFor+`"`) {
		t.Errorf("the refusal declares no %s: %s", refusalAskedFor, firstLine(body))
	}
}
