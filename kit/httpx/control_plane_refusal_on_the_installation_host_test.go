package httpx_test

// Two refusals of the installation's own host must not be told apart by anything a client can
// read: the control plane refused to a tenant that is not the installation's, and an address
// nobody mounted at that host. README.md, CHANGELOG.md and authorize.go's notHere all promise
// one status, one body and one set of headers, so what is compared here is the whole answer as
// it arrived — every header, and the body a client would parse — rather than a list of names.
//
// kit/httpx/control_plane_refusal_test.go holds the host gate; its fixture resolves the
// installation host to the operator's tenant, so nothing it asks is refused for who is asking.
// This file holds the two things that fixture cannot reach: the refusal of a tenant that is not
// the installation's at the very host the control plane is served at, at both document types and
// both ends of the renderer's opt-out, and the answer the same opt-out leaves open on the public
// write limit's Retry-After. Both go over a real HTTP/1.1 server, because the shape of an answer
// — Content-Length, Date, which encoder wrote the body — belongs to the response a client reads,
// not to a handler's arguments.

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
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// controlPlaneKernel is guardKernel with the one thing it cannot express: whether the tenant this
// installation resolves at the installation host *is* the operator's. Without it an Ops
// address is either never served (a customer host, refused by the host gate) or never
// refused by notHere (an operator tenant), and the road README's second half describes
// cannot be built at all.
func controlPlaneKernel(t *testing.T, operator bool, fault httpx.Fault) (http.Handler, func(rel string) string, func(rel string) string) {
	t.Helper()
	who := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme", Operator: operator}
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
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
		OperationID: "controlPlane-operate", Method: http.MethodGet, Path: "/all",
	}, httpx.OperatorPermission("billing:operate"), ok)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the fixture does not describe itself: %v", err)
	}
	return router, api.Surfaces(probe).Ops.Path, func(rel string) string { return httpx.AppRoot + rel }
}

// controlPlaneAnswer is what one request read off a real server: the status, the headers as they
// arrived (Content-Length included) and the whole body a client would parse. The two stamps
// bracket the exchange on the client's own clock, which is the interval inside which the
// server can have written the Date this answer carries; controlPlaneDate is what reads them.
type controlPlaneAnswer struct {
	status int
	header http.Header
	body   string
	sentAt time.Time
	readAt time.Time
}

func controlPlaneAsk(t *testing.T, server, authority, path, accept, id string) controlPlaneAnswer {
	t.Helper()
	return controlPlaneSend(t, http.MethodGet, "", server, authority, path, accept, id)
}

// controlPlaneSend is one exchange with a real HTTP/1.1 server, asked at `authority` — the host
// this composition serves, which is the only thing its tenant loader reads. A non-empty `body`
// makes it a write, sent as a browser's form sends JSON, because the answer worth reading off a
// server is the one a limit gives to a person at a form.
func controlPlaneSend(t *testing.T, method, body, server, authority, path, accept, id string) controlPlaneAnswer {
	t.Helper()
	sent := time.Now()
	var payload io.Reader
	if body != "" {
		payload = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, server+path, payload)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	req.Host = authority
	req.Header.Set("Accept", accept)
	req.Header.Set(httpx.RequestIDHeader, id)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	defer res.Body.Close()
	read, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return controlPlaneAnswer{status: res.StatusCode, header: res.Header, body: string(read),
		sentAt: sent, readAt: time.Now()}
}

// noncePattern is the per-request content security policy nonce, random by construction
// (headers.go), which is the one thing a comparison of two responses may not read apart.
var noncePattern = regexp.MustCompile(`'nonce-[A-Za-z0-9_=+/]+'`)

// controlPlaneDate checks the one other header two responses may not be read apart on. `Date`
// is not part of the answer this kernel writes — nothing in kit/httpx sets or clears one; the
// server stamps it from its own clock as it writes a response — so two refusals of one host
// print the same string only while the clock stays inside one second, which says when the pair
// was taken and not how the host was refused. What is pinned is the instant: exactly one Date,
// readable as an HTTP date, inside the interval of its own exchange, with one second of
// allowance because the header resolves to whole seconds and truncates downward. A refusal that
// answers no Date, two, an unreadable one, or one from outside the round trip that carried it
// (copied off a cached answer, or a writer that stopped the clock) fails here.
func controlPlaneDate(t *testing.T, which string, answer controlPlaneAnswer) {
	t.Helper()
	values := answer.header.Values("Date")
	if len(values) != 1 {
		t.Errorf("%s answers %d Date headers (%v), want the one every response of this server carries",
			which, len(values), values)
		return
	}
	when, err := http.ParseTime(values[0])
	if err != nil {
		t.Errorf("%s answers Date=%s, which is not an HTTP date a client could read: %v", which, values[0], err)
		return
	}
	if before, after := answer.sentAt.Add(-time.Second), answer.readAt; before.After(when) || when.After(after) {
		t.Errorf("%s answers Date=%s, outside the exchange that produced it (asked %s, read whole %s): the stamp is not the moment this answer was written",
			which, values[0], answer.sentAt.UTC().Format(time.RFC3339Nano), after.UTC().Format(time.RFC3339Nano))
	}
}

func controlPlaneSame(t *testing.T, a, b controlPlaneAnswer, asked, reference string) {
	t.Helper()
	if a.status != b.status {
		t.Errorf("%s answers %d where %s answers %d: README.md promises one status for both",
			asked, a.status, reference, b.status)
	}
	if a.body != b.body {
		t.Errorf("%s and %s are not the same body:\n %s: %s\n %s: %s", asked, reference,
			asked, a.body, reference, b.body)
	}
	// The two Date stamps are compared as instants, which is the only way two of them taken a
	// second apart are comparable: each must be the single readable stamp of its own exchange,
	// and that bracket bounds the pair, since both then lie between the first ask and the
	// reading of the second. controlPlaneDate says why, and fails the case when either answer fails
	// it; the header is out of the loop below because no string comparison has anything left
	// to catch in it.
	controlPlaneDate(t, asked, a)
	controlPlaneDate(t, reference, b)
	for name := range b.header {
		if strings.EqualFold(name, "X-Request-Id") {
			continue // the caller sent it, and sent the same value on both
		}
		if name == "Date" {
			continue // controlPlaneDate compared it above, as the instant it is
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
// the half of the README's Ops paragraph that kit/httpx/control_plane_refusal_test.go could
// not reach: not the host gate, but the refusal of a tenant that is not the installation's
// *at the host the control plane is served at*.
func TestTheControlPlaneRefusalAtTheInstallationHostIsTheAnswerOfAnAddressNobodyMounted(t *testing.T) {
	const id = "one-request-id-for-both"
	const nowhere = "/nothing-is-mounted-at-this-address"

	// Reachability first, and it does not read the refusal's own output: at a
	// composition where the host resolves to the operator's tenant the very same address
	// is *served*. If this fails, the address is not mounted and the table below compares
	// two ordinary 404s and means nothing.
	served, opsAt, _ := controlPlaneKernel(t, true, nil)
	opServer := httptest.NewServer(served)
	defer opServer.Close()
	if got := controlPlaneAsk(t, opServer.URL, installationHost, opsAt("/all"), "application/json", id); got.status != http.StatusOK {
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
			router, opsAt, appAt := controlPlaneKernel(t, false, fault)
			server := httptest.NewServer(router)
			defer server.Close()

			refusal := controlPlaneAsk(t, server.URL, installationHost, opsAt("/all"), shape.accept, id)
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
			controlPlaneSame(t, refusal, controlPlaneAsk(t, server.URL, installationHost, appAt(nowhere), shape.accept, id),
				"the control plane at the installation host", "an address nobody mounted")
		})
	}
}

// TestThePublicWriteLimitSaysWhenToComeBackAtBothEndsOfTheRenderer asks the question the
// renderer's opt-out leaves open: Retry-After was asserted for the refusal a renderer
// answered, and a refusal the renderer declines is written by the fallback instead. The
// limit is worth nothing to a person at a form if the answer that reaches them stops saying
// when to try again.
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
			server := httptest.NewServer(router)
			defer server.Close()
			asked := controlPlaneSend(t, http.MethodPost, "{}", server.URL, host, publicly(api, "/ask"), browserAccept, "one-write-at-the-limit")
			if asked.status != http.StatusTooManyRequests {
				t.Fatalf("the limit refused %s = %d, want 429: the case says nothing unless it reached the refusal",
					publicly(api, "/ask"), asked.status)
			}
			if got := asked.header.Get("Retry-After"); got == "" {
				t.Errorf("the refusal carries no Retry-After when %s: the caller is told to stop and not when to come back (%s)",
					shape.name, asked.body)
			}
		})
	}
}
