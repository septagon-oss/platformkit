package httpx_test

// Reviewer's cases for the fourth review of T-0024 (three surfaces by path). Reviewer: a
// fresh pi session, 2026-09-21.
//
// httpx.Fault documents one opt-out and one consequence of using it: "returning false
// falls back to the Problem JSON, which is how a renderer opts out for a request it has no
// chrome for rather than inventing one." ui/page.FaultHandler uses it for real twice over —
// for a shell with no frame, and for a document it could not build ("let the kernel's JSON
// answer, which is honest about being a failure").
//
// A refusal ahead of the huma chain honours that: API.fail writes the document once. The
// refusals inside the chain answer through API.refuse, which calls fail and then, when fail
// reports the renderer did not answer, writes huma's error over the same response. These
// cases ask both levels for the same opt-out and hold them to the one answer the contract
// promises. Each opens with a reachability claim true whatever the answer — the same chain
// answering 200 at a door its caller may use — and asserts the fixed behaviour's own
// output: one document, the verdict's status, the media type a parser reads.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// decliningFault is a renderer registered for every request and answering none of them —
// the opt-out httpx.Fault documents. It counts calls, so a case can say the renderer was
// reached and declined rather than never consulted.
type decliningFault struct{ calls int }

func (f *decliningFault) render(http.ResponseWriter, *http.Request, *problem.Problem) bool {
	f.calls++
	return false
}

type noHeadroom struct{}

func (noHeadroom) Allow(context.Context, string, int, time.Duration) (bool, time.Duration, error) {
	return false, 30 * time.Second, nil
}

// declineKernel composes one application: a guarded workspace door, a door that needs only
// a session, an anonymous public write, and the renderer above as its failure page.
func declineKernel(t *testing.T) (*httpx.API, http.Handler, *decliningFault) {
	t.Helper()
	_, conn := dbtest.Schema(t)
	fault := &decliningFault{}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Installation: installationHost, Conn: conn,
		Tenants: loaderFunc(func(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
			return tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return false, nil // the refusals below are grants the caller does not hold
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		WriteLimiter: noHeadroom{},
		Log:          slog.New(slog.DiscardHandler),
		Fault:        fault.render,
	})
	httpx.Register(api.Surfaces("optout").App, huma.Operation{
		OperationID: "optout-plans", Method: http.MethodGet, Path: "/plans",
	}, httpx.Permission("billing:read"), ok)
	httpx.Register(api.Surfaces("optout").App, huma.Operation{
		OperationID: "optout-session", Method: http.MethodGet, Path: "/session",
	}, httpx.SignedIn(), ok)
	httpx.Register(api.Surfaces("optout").Public, huma.Operation{
		OperationID: "optout-ask", Method: http.MethodPost, Path: "/ask",
	}, httpx.Public(), ok)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the fixture does not describe itself: %v", err)
	}
	return api, router, fault
}

// navigate sends the request a browser sends: Accept asking to be shown the answer, a
// session cookie presented, so the guard that refuses is the one the case is about.
func navigate(t *testing.T, router http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader("{}"))
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", browserAccept)
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

// oneProblem is the fallback a declining renderer is owed: exactly one problem document —
// not that document followed by a second answer to the same request — carrying the verdict.
func oneProblem(t *testing.T, res *httptest.ResponseRecorder, want int, wantCode string) {
	t.Helper()
	if res.Code != want {
		t.Fatalf("%s = %d, want %d: %s", wantCode, res.Code, want, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
		t.Fatalf("a refusal the renderer declined is answered %q, want the fallback httpx.Fault names (application/problem+json): %q",
			got, res.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &doc); err != nil {
		t.Fatalf("the body is not one problem document (%v); it holds %q", err, res.Body.String())
	}
	if doc["status"] != float64(want) {
		t.Errorf("the fallback document says %v, want the verdict %d", doc["status"], want)
	}
	if detail, _ := doc["detail"].(string); !strings.Contains(detail, wantCode) {
		t.Errorf("the fallback document carries %q, want the published code %q in it", detail, wantCode)
	}
}

// TestAGuardRefusalTheRendererDeclinesIsOneProblemDocument. A composition registered a
// renderer, the renderer opted out as it may, and the guard runs inside the huma chain.
func TestAGuardRefusalTheRendererDeclinesIsOneProblemDocument(t *testing.T) {
	for _, one := range []struct {
		name, method, path, wantCode string
		want                         int
	}{
		{"the authorization denial", http.MethodGet, "/plans", httpx.CodeDenied, http.StatusForbidden},
		{"the public write limit", http.MethodPost, "/ask", httpx.CodeLimitExhausted, http.StatusTooManyRequests},
	} {
		t.Run(one.name, func(t *testing.T) {
			api, router, fault := declineKernel(t)
			door := api.Surfaces("optout").App.Path("/plans")
			if one.method == http.MethodPost {
				door = api.Surfaces("optout").Public.Path(one.path)
			}

			// Reachability, true before and after any fix: the host is live and this
			// caller's session is accepted, so the same chain answers 200 at the door that
			// needs no grant. Only the grant separates the refusal from that 200.
			if got := navigate(t, router, http.MethodGet, api.Surfaces("optout").App.Path("/session")); got.Code != http.StatusOK {
				t.Fatalf("a door of this chain that asks only for a session = %d, want 200; this case is about that chain: %s",
					got.Code, got.Body.String())
			}

			res := navigate(t, router, one.method, door)
			if fault.calls == 0 {
				t.Fatalf("%s never asked the registered renderer, so this case proves nothing about the opt-out", one.name)
			}
			oneProblem(t, res, one.want, one.wantCode)
		})
	}
}

// TestARefusalAheadOfTheChainThatTheRendererDeclinesIsOneProblemDocument. The same
// composition and the same opt-out, one refusal answered through API.fail instead of
// API.refuse — an address nobody mounted. This is the answer the cases above are held to,
// so a fix that reaches one level and not the other stays visible, and it passes today.
func TestARefusalAheadOfTheChainThatTheRendererDeclinesIsOneProblemDocument(t *testing.T) {
	api, router, fault := declineKernel(t)

	res := navigate(t, router, http.MethodGet, api.Surfaces("optout").App.Path("/nothing-here"))
	if fault.calls == 0 {
		t.Fatalf("the router never asked the registered renderer, so this case proves nothing about the opt-out")
	}
	oneProblem(t, res, http.StatusNotFound, "")
}
