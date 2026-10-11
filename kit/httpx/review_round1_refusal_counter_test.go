package httpx_test

// Review round 1 (T-0110). The delivery's claim, in its own words:
//
//   countRefusal ... is called by both writers of a refusal — fail, for the ones
//   the kernel makes for itself outside the operation chain, and refuse, for the
//   ones inside it — because a counter that counts only half the refusals is a
//   number an operator would be wrong to trust.  (kit/httpx/traced.go)
//
// This file counts the writers of a refusal in this router and finds a third one:
// huma's own error writer, reached whenever the router refuses a request before a
// guard decides — a query value that is not an integer, a body that is not JSON,
// a handler that returns an error. Those answers are refusals by the counter's own
// definition (they are 4xx and 5xx answers the kernel gave a client it would not
// serve) and they are what the class set is largely named for: "invalid" is
// 400/414/422/431 and "failed" is 5xx, and no call of fail or refuse in this
// package writes either. The cases below ask the counter about them.
//
// Each case reaches its assertion through what the fixed behaviour prints: the
// status the client was answered with, and the class attribute on the datapoint.
// Nothing here asks for a sentence.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// reviewTenant is the tenant this fixture's host resolves to.
var reviewTenant = uuid.MustParse("77777777-7777-4777-8777-777777777777")

// reviewFixture is a router with two operations: one that reads a query value and
// one whose handler fails. Both resolve "acme.test" to reviewTenant.
func reviewFixture(t *testing.T) http.Handler {
	t.Helper()
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host,
		Conn:       app,
		Cache:      cache.Memory("pkit"),
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
			if h != host {
				return tenancy.Tenant{}, tenancy.ErrNoSuchHost
			}
			return tenancy.Tenant{ID: reviewTenant, Slug: "acme", Name: "Acme"}, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return true, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "review_query", Method: http.MethodGet, Path: "/query",
	}, httpx.SignedIn(), func(context.Context, *reviewInput) (*struct{}, error) {
		return &struct{}{}, nil
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "review_public_query", Method: http.MethodGet, Path: "/public-query",
	}, httpx.Public(), func(context.Context, *reviewInput) (*struct{}, error) {
		return &struct{}{}, nil
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "review_failing", Method: http.MethodGet, Path: "/failing",
	}, httpx.Public(), func(context.Context, *struct{}) (*struct{}, error) {
		return nil, context.DeadlineExceeded
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the routes do not declare themselves: %v", err)
	}
	return router
}

type reviewInput struct {
	Limit int64 `query:"limit"`
}

// TestARefusalTheRouterWritesItselfIsCountedClassInvalid is the case: the client
// asked for ?limit=many, the router answered 400, and the class the delivery's own
// table gives a 400 is "invalid". The counter is asked for the difference this one
// request made, and for the tenant the request resolved.
func TestARefusalTheRouterWritesItselfIsCountedClassInvalid(t *testing.T) {
	router := reviewFixture(t)
	before := reviewRefusalTotal(t, "invalid")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/probe/public-query?limit=many", nil))
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a query value that is not an integer was answered %d, want a 4xx refusal: %s",
			rec.Code, rec.Body.String())
	}
	// telemetry.RefusalClass calls both answers "invalid": the table gives 400,
	// 414, 422 and 431 the same class, so which of them the router gave is not the
	// question this case asks. Whether the number noticed at all is.
	if class := telemetry.RefusalClass(rec.Code); class != "invalid" {
		t.Fatalf("RefusalClass(%d) = %q, want the class the table gives a refusal of this kind", rec.Code, class)
	}

	got := reviewRefusalTotal(t, "invalid") - before
	if got < 1 {
		t.Errorf("the counter moved by %d for a request the router answered %d; it refused it and "+
			"pkit.http.refusals did not notice, so a dashboard reading class=%q reads zero "+
			"while clients are being refused: the counted writers are fail and refuse only, and huma "+
			"answers this shape itself", got, rec.Code, "invalid")
	}
	if !reviewRefusalNamesTenant(t, "invalid", reviewTenant) {
		t.Errorf("a refusal of this request carries no pkit.tenant.id for %s, though the host resolved it", reviewTenant)
	}
}

// TestAHandlerFailureIsCountedAtAll is the 5xx half of the same claim. The client
// was answered 500 (the class table calls it "failed"), and the instrument's own
// comment says the counter counts refusals and not errors — but a 500 the client
// saw is still an answer nobody would find in the number if nothing writes it.
func TestAHandlerFailureIsCountedAtAll(t *testing.T) {
	router := reviewFixture(t)
	before := reviewRefusalTotal(t, "failed")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/probe/failing", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a handler that failed was answered %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if got := reviewRefusalTotal(t, "failed") - before; got < 1 {
		t.Errorf("a client was answered 500 by a handler that returned an error and the counter moved by "+
			"%d: that answer is written by huma, so the only writer class \"failed\" ever has is a handler "+
			"that panics, and an operator reading the number sees an outage that never happened", got)
	}
}

// TestAGuardRefusalOfAnAnonymousCallerNamesItsTenant is the half that works, pinned
// so a fix for the two above cannot arrive by moving the count somewhere that loses
// this: a 403 written by the authorization guard inside the huma chain is counted,
// and it names the tenant that request resolved.
func TestAGuardRefusalOfAnAnonymousCallerNamesItsTenant(t *testing.T) {
	router := reviewFixture(t)
	before := reviewRefusalTotal(t, "forbidden")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/probe/query?limit=5", nil))
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
		t.Fatalf("an anonymous caller at a signed-in route was answered %d, want a refusal: %s",
			rec.Code, rec.Body.String())
	}
	if got := reviewRefusalTotal(t, "forbidden") - before; got < 1 {
		t.Fatalf("the guard's refusal was not counted: the counter moved by %d for a %d answer",
			got, rec.Code)
	}
	if !reviewRefusalNamesTenant(t, "forbidden", reviewTenant) {
		t.Errorf("the guard's refusal was counted with no pkit.tenant.id for %s; the tenant on every "+
			"number is the brief's headline and this is the one refusal path that holds the tenant", reviewTenant)
	}
}

// --- helpers ---

// reviewRefusalTotal is the running total counted for one class. An instrument
// that nothing has ever recorded on is absent from a collection rather than
// present at zero, so absence is this number's zero — and a case that sees zero
// after a client was refused is asking the question the case exists to ask.
func reviewRefusalTotal(t *testing.T, class string) int64 {
	t.Helper()
	m, ok := metrics(t)["pkit.http.refusals"]
	if !ok {
		return 0
	}
	sum, ok := m.Data.(sdkmetric.Sum[int64])
	if !ok {
		t.Fatalf("pkit.http.refusals is %T, want a sum", m.Data)
	}
	var total int64
	for _, p := range sum.DataPoints {
		if v, found := p.Attributes.Value(attribute.Key(telemetry.AttrRefusalClass)); found && v.AsString() == class {
			total += p.Value
		}
	}
	return total
}

// reviewRefusalNamesTenant reports whether any datapoint of one class names want.
func reviewRefusalNamesTenant(t *testing.T, class string, want uuid.UUID) bool {
	t.Helper()
	m, ok := metrics(t)["pkit.http.refusals"]
	if !ok {
		return false
	}
	sum, ok := m.Data.(sdkmetric.Sum[int64])
	if !ok {
		t.Fatalf("pkit.http.refusals is %T, want a sum", m.Data)
	}
	for _, p := range sum.DataPoints {
		v, found := p.Attributes.Value(attribute.Key(telemetry.AttrRefusalClass))
		if !found || v.AsString() != class {
			continue
		}
		if id, has := p.Attributes.Value(attribute.Key(telemetry.AttrTenantID)); has && id.AsString() == want.String() {
			return true
		}
	}
	return false
}
