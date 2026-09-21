package httpx_test

// The fifth review's cases, left in the tree by a reviewer who did not write the fix.
//
// ## The case that fails, and what it falsifies
//
// kit/httpx/README.md sells the control plane's secrecy in one sentence with two halves:
// the surface "answers exactly what an address nobody mounted answers — the same status,
// the same body, and the same headers every other refusal of that host carries, because
// the gate runs inside the middleware that writes them, so the surface discloses nothing;
// and at the installation's host a tenant that is not the installation's is refused the
// same way, before the Authorizer is consulted". CHANGELOG.md repeats it ("the same
// status, the same body and the same headers every other refusal of that host carries …
// including to a tenant that is not the installation's"), and middleware.go's notHere —
// the guard the second half names — states it most precisely: "the same verdict, the same
// sentence and the same writer as the address nobody mounted … This is what makes the
// answer byte-identical to the one a never-mounted address gets, Fault page included".
// kit/httpx/fault.go's own rule is the reason it should hold: "a second writer of problem
// bodies in this package is a second answer to the question this file exists to ask once".
//
// The first half is held and pinned: review_surfaces_test.go compares the host gate with a
// never-mounted address, headers and body, at both shapes, and the gate was moved inside
// the headers middleware to make that true. That fixture resolves the installation host to
// an *operator* tenant, so nothing it asks ever reaches notHere. The second half — the road
// round 8's table row "a door only the operator may open, asked at another tenant" walks —
// has never been compared to anything.
//
// This case compares it, at the installation host, over a real HTTP/1.1 server, at both
// document types and both ends of the renderer's opt-out. The page a browser is shown is
// byte-identical, exactly as claimed, and that subtest passes. The document a client that
// asked for a value is handed is not: `refuse` falls back to huma's writer and `fail` to
// writeProblem, and the two encoders are not the same writer — one adds
// `"$schema":"https://<host>/schemas/Problem.json"` and a `Link: … rel="describedby"`
// response header, the other adds neither. So the same refusal of the same host arrives as
// 211 bytes against 150, with a header the ordinary refusal does not answer: one body, yes
// — but not "the same body", and not "the same headers", and the kernel does keep a second
// writer of problem bodies.
//
// The fix has two honest shapes, and either turns every subtest green: answer the chain's
// fallback with the same encoder the kernel-side answers use (this reviewer tried it —
// `refuse` writing writeProblem on the writer humachi carries, four lines — and
// ./kit/httpx ./ui/... ./kit/app ./apps/platformkit ./modules/auth ./kit/rest
// ./modules/task all stay ok), or state in README.md, CHANGELOG.md and notHere that the
// two answers differ and give up the claim that the address discloses nothing.
//
// ## The case that passes
//
// Retry-After on the public write limit was left unverified by the fourth review for the
// limb this round changed. Headers are set before the answer is written, on either end of
// the opt-out, which the second case pins so a later change to the fallback writer that
// drops them is caught here rather than in a browser.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// round5Kernel is guardKernel with the one thing it cannot express: whether the tenant this
// installation resolves at the installation host *is* the operator's. Without it an Ops
// address is either never served (a customer host, refused by the host gate) or never
// refused by notHere (an operator tenant), and the road README's second half describes
// cannot be built at all.
func round5Kernel(t *testing.T, operator bool, fault httpx.Fault) (http.Handler, func(rel string) string, func(rel string) string) {
	t.Helper()
	who := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme", Operator: operator}
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Installation: installationHost, Conn: app,
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
			if h == host || h == installationHost {
				return who, nil
			}
			return tenancy.Tenant{}, tenancy.ErrNoSuchHost
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) { return true, nil }),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log:   slog.New(slog.DiscardHandler),
		Fault: fault,
	})
	httpx.Register(api.Surfaces(probe).Ops, huma.Operation{
		OperationID: "round5-operate", Method: http.MethodGet, Path: "/all",
	}, httpx.OperatorPermission("billing:operate"), ok)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the fixture does not describe itself: %v", err)
	}
	return router, api.Surfaces(probe).Ops.Path, func(rel string) string { return httpx.AppRoot + rel }
}

// round5Answer is what one request read off a real server: the status, the headers as they
// arrived (Content-Length included) and the whole body a client would parse.
type round5Answer struct {
	status int
	header http.Header
	body   string
}

func round5Ask(t *testing.T, server, authority, path, accept, id string) round5Answer {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server+path, nil)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	req.Host = authority
	req.Header.Set("Accept", accept)
	req.Header.Set(httpx.RequestIDHeader, id)
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return round5Answer{status: res.StatusCode, header: res.Header, body: string(body)}
}

// noncePattern is the per-request content security policy nonce, random by construction
// (headers.go), which is the one thing a comparison of two responses may not read apart.
var noncePattern = regexp.MustCompile(`'nonce-[A-Za-z0-9_=+/]+'`)

func round5Same(t *testing.T, a, b round5Answer, asked, reference string) {
	t.Helper()
	if a.status != b.status {
		t.Errorf("%s answers %d where %s answers %d: README.md promises one status for both",
			asked, a.status, reference, b.status)
	}
	if a.body != b.body {
		t.Errorf("%s and %s are not the same body:\n %s: %s\n %s: %s", asked, reference,
			asked, a.body, reference, b.body)
	}
	for name := range b.header {
		if strings.EqualFold(name, "X-Request-Id") {
			continue // the caller sent it, and sent the same value on both
		}
		got := fmt.Sprint(a.header.Values(name))
		want := fmt.Sprint(b.header.Values(name))
		if name == "Content-Security-Policy" {
			got, want = noncePattern.ReplaceAllString(got, "'nonce-…'"), noncePattern.ReplaceAllString(want, "'nonce-…'")
		}
		if got != want {
			t.Errorf("%s answers %s=%s where every other refusal of this host answers %s=%s: a caller that can tell the two apart has been told a control plane is served here, which README.md, CHANGELOG.md and notHere all say it cannot do",
				asked, name, got, name, want)
		}
	}
	for name := range a.header {
		if _, ok := b.header[http.CanonicalHeaderKey(name)]; !ok && !strings.EqualFold(name, "X-Request-Id") {
			t.Errorf("%s answers a header %s=%s that %s does not answer at all",
				asked, name, fmt.Sprint(a.header.Values(name)), reference)
		}
	}
}

// TestTheControlPlaneRefusalAtTheInstallationHostIsTheAnswerOfAnAddressNobodyMounted holds
// the half of the README's Ops paragraph that review_surfaces_test.go could not reach: not
// the host gate, but the refusal of a tenant that is not the installation's *at the host the
// control plane is served at*.
func TestTheControlPlaneRefusalAtTheInstallationHostIsTheAnswerOfAnAddressNobodyMounted(t *testing.T) {
	const id = "one-request-id-for-both"
	const nowhere = "/nothing-is-mounted-at-this-address"

	// Reachability first, and it does not read the refusal's own output: at a
	// composition where the host resolves to the operator's tenant the very same address
	// is *served*. If this fails, the address is not mounted and the table below compares
	// two ordinary 404s and means nothing.
	served, opsAt, _ := round5Kernel(t, true, nil)
	opServer := httptest.NewServer(served)
	defer opServer.Close()
	if got := round5Ask(t, opServer.URL, installationHost, opsAt("/all"), "application/json", id); got.status != http.StatusOK {
		t.Fatalf("the operator's tenant at %s = %d %s; the address is not mounted, so the case below compares nothing",
			opsAt("/all"), got.status, got.body)
	}

	for _, shape := range []struct {
		name   string
		accept string
		answer bool
	}{
		{"a client that asked for a value", "application/json", false},
		{"a browser, the renderer declining", browserAccept, false},
		{"a browser, the renderer answering", browserAccept, true},
	} {
		t.Run(shape.name, func(t *testing.T) {
			fault := func(http.ResponseWriter, *http.Request, *problem.Problem) bool { return false }
			if shape.answer {
				fault = documentFault
			}
			router, opsAt, appAt := round5Kernel(t, false, fault)
			server := httptest.NewServer(router)
			defer server.Close()

			refusal := round5Ask(t, server.URL, installationHost, opsAt("/all"), shape.accept, id)
			if refusal.status != http.StatusNotFound {
				t.Fatalf("the control plane asked by a tenant that is not the installation's = %d %s, want the 404 this case compares",
					refusal.status, refusal.body)
			}
			// One answer rather than two: the doubled body the neighbouring cases in
			// refusal_shape_test.go exist for would show here as a body no parser reads
			// whole, or as more than one page in one document.
			if refusal.header.Get("Content-Type") == problem.ContentType || shape.accept == "application/json" {
				var one map[string]any
				if err := json.Unmarshal([]byte(refusal.body), &one); err != nil {
					t.Fatalf("%s is not one problem document (%v): %s", shape.name, err, refusal.body)
				}
			} else if n := strings.Count(refusal.body, "<!doctype html>"); n != 1 {
				t.Errorf("%s carries %d pages in one body: %s", shape.name, n, refusal.body)
			}
			round5Same(t, refusal, round5Ask(t, server.URL, installationHost, appAt(nowhere), shape.accept, id),
				"the control plane at the installation host", "an address nobody mounted")
		})
	}
}

// TestThePublicWriteLimitSaysWhenToComeBackAtBothEndsOfTheRenderer closes the fourth
// review's Unverified: Retry-After was asserted for the refusal a renderer answered, and
// round 8 changed what happens when the renderer declines. The limit is worth nothing to a
// person at a form if the answer that reaches them stops saying when to try again.
func TestThePublicWriteLimitSaysWhenToComeBackAtBothEndsOfTheRenderer(t *testing.T) {
	for _, shape := range []struct {
		name   string
		answer bool
	}{
		{"the renderer answering with a page", true},
		{"the renderer declining and the document answering", false},
	} {
		t.Run(shape.name, func(t *testing.T) {
			fault := func(http.ResponseWriter, *http.Request, *problem.Problem) bool { return false }
			if shape.answer {
				fault = documentFault
			}
			api, router := guardKernel(t, guardSetup{allow: true, limiter: &window{allow: 0}}, fault)
			asked := askFor(t, router, http.MethodPost, publicly(api, "/ask"), browserAccept)
			if asked.Code != http.StatusTooManyRequests {
				t.Fatalf("the limit refused %s = %d, want 429: the case says nothing unless it reached the refusal",
					publicly(api, "/ask"), asked.Code)
			}
			if got := asked.Header().Get("Retry-After"); got == "" {
				t.Errorf("the refusal carries no Retry-After when %s: the caller is told to stop and not when to come back (%s)",
					shape.name, asked.Body.String())
			}
		})
	}
}
