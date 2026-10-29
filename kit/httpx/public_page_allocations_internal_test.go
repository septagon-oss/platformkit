//go:build !race

package httpx

// public_page_allocations_internal_test.go is this kernel's only allocation guard,
// and it stands at the address a public page is served at: an anonymous GET that
// resolves a tenant, answers one document and reads no rows. It is the request a
// product's landing page makes and the one a deployment answers more often than
// any other, so the per-request work its chain does is the work every application
// on this runtime pays, once per visitor.
//
// The number is pinned rather than watched because a chain grows one middleware at
// a time and each one is cheap alone. The ceiling is what this revision costs, and
// a commit that raises it names the feature on this request that needs the extra
// allocations.
//
// The file is not built under -race: the detector instruments every allocation site,
// so the count there (163 at fabf73c) is not the one a deployment pays.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// publicPageHost is the customer host the measured request is addressed to. No
// other case resolves it, so warming it cannot disturb another case's belief and
// another case's belief cannot warm this one.
const publicPageHost = "stockpilot.example"

// publicPageCeiling is the most this chain may allocate per anonymous page
// request. It is the count this revision costs, not a target and not a claim
// about an older kernel: the line a later change explains itself over. This one
// sits above the page before it because the negotiated int64 media answers with
// Vary: Accept, and a header write is one allocation.
const publicPageCeiling = 151

// publicPageDoc is the document the measured route answers: the tenant's own
// landing page, one field, no query behind it.
type publicPageDoc struct {
	Body struct {
		Name string `json:"name"`
	}
}

// pageRecorder is the response writer of the measured request. It is not
// httptest.ResponseRecorder because this rig answers the same request hundreds of
// times and the writer has to be reset rather than rebuilt: a fresh recorder per
// iteration would count the harness's own allocations as the chain's.
type pageRecorder struct {
	header  http.Header
	body    []byte
	status  int
	written bool
}

func (r *pageRecorder) Header() http.Header { return r.header }

func (r *pageRecorder) Write(p []byte) (int, error) {
	if !r.written {
		r.status, r.written = http.StatusOK, true
	}
	r.body = append(r.body, p...)
	return len(p), nil
}

func (r *pageRecorder) WriteHeader(status int) {
	if !r.written {
		r.status, r.written = status, true
	}
}

// reset empties this writer for the next answer. The header map is kept and
// cleared rather than reallocated, for the reason the type exists at all.
func (r *pageRecorder) reset() {
	clear(r.header)
	r.body, r.status, r.written = r.body[:0], 0, false
}

// publicPageRig is the assembled chain, one request and one writer ready to be
// filled again, and the two witnesses the cases read: how many times the tenant
// loader was asked, and which tenant — if any — reached the handler.
type publicPageRig struct {
	handler http.Handler
	writer  *pageRecorder
	request *http.Request
	loader  int
	tenant  tenancy.Tenant
}

// newPublicPageRig builds the chain through the real constructor, mounts one public
// GET, and warms the host resolution in the shared store the build was handed — the
// same entry the first request after a deploy writes and the same one
// InvalidateHost closes.
//
// The connection is a zero *db.Conn that is never dereferenced: a warm resolution
// means this request opens no transaction, and the case that asserts the loader was
// never asked is what proves that rather than the comment. A reflect-allocated zero
// stands in for a pool because the request under measurement must not need one.
func newPublicPageRig(tb testing.TB, store cache.Cache, conn *db.Conn, tenant tenancy.Tenant) *publicPageRig {
	tb.Helper()
	rig := &publicPageRig{writer: &pageRecorder{header: http.Header{}}}
	a, router := New(Options{
		Cache: store, PublicHost: publicPageHost, Installation: publicPageInstallation, Conn: conn,
		Tenants: publicPageLoader{rig: rig},
		Authorize: publicPageAuthorizer(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return true, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	Register(a.Surfaces("home").Public, huma.Operation{
		OperationID: "public.home.page", Method: http.MethodGet, Path: "/page",
	}, Public(), func(ctx context.Context, _ *struct{}) (*publicPageDoc, error) {
		if t, ok := tenancy.FromContext(ctx); ok {
			rig.tenant = t
		}
		doc := &publicPageDoc{}
		doc.Body.Name = "Acme"
		return doc, nil
	})
	if err := a.ValidateDeclarations(); err != nil {
		tb.Fatalf("the fixture does not describe itself: %v", err)
	}
	if tenant.ID != uuid.Nil {
		warmPublicPageHost(tb, store, tenant)
	}
	rig.handler = router
	rig.request = httptest.NewRequest(http.MethodGet, "http://"+publicPageHost+a.Surfaces("home").Public.Path("/page"), nil)
	return rig
}

// warmPublicPageHost writes the resolution the shared store holds after the first
// request, through the store itself rather than a map this rig owns, so the request
// under measurement reads the entry the deployment reads. The generation it is
// stamped with is the one that read answers open, as every resolution write is.
func warmPublicPageHost(tb testing.TB, store cache.Cache, tenant tenancy.Tenant) {
	tb.Helper()
	key := hostScope.Entry(publicPageHost)
	_, _, under, err := store.Get(context.Background(), key)
	if err != nil {
		tb.Fatalf("ask the store about %s: %v", key, err)
	}
	buf, err := json.Marshal(newCachedTenant(tenant))
	if err != nil {
		tb.Fatalf("encode the resolution: %v", err)
	}
	if err := store.Set(context.Background(), key, buf, hostTTL, under); err != nil {
		tb.Fatalf("warm the resolution: %v", err)
	}
}

// wiredPublicPageRig is the request the deployment serves: a connection it could
// open a transaction on, and a host already resolved in the shared store.
func wiredPublicPageRig(tb testing.TB) *publicPageRig {
	tb.Helper()
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	conn := reflect.New(reflect.TypeOf(db.Conn{})).Interface().(*db.Conn)
	return newPublicPageRig(tb, cache.Memory("pkit"), conn, tenant)
}

// run answers the measured request once, against the same writer and the same
// request, so the count is the chain's own and not the harness re-arming itself.
func (r *publicPageRig) run() {
	r.writer.reset()
	r.handler.ServeHTTP(r.writer, r.request)
}

// TestAPublicPageIsAnsweredFromTheSharedResolution is the control the number below
// depends on: the anonymous read is served, the tenant loader is never asked, and
// the tenant that reached the handler is the one the shared store holds. A rig that
// had quietly stopped reading the resolution would still answer 200 — from the
// public branch that serves an unresolved host — and would be seen here, where the
// handler is asked what it was given.
func TestAPublicPageIsAnsweredFromTheSharedResolution(t *testing.T) {
	rig := wiredPublicPageRig(t)
	rig.run()
	if rig.writer.status != http.StatusOK {
		t.Fatalf("the public page answered %d, want 200: %s", rig.writer.status, rig.writer.body)
	}
	if rig.loader != 0 {
		t.Fatalf("the page asked the tenant loader %d times, want 0; the count would be timing a database miss", rig.loader)
	}
	if rig.tenant.Slug != "acme" {
		t.Errorf("the handler was given %+v, want the warmed resolution of acme; the page no longer reads the shared host entry", rig.tenant)
	}
}

// TestAPublicPageCostsWhatItWasMeasuredAt is the guard: an anonymous page read
// stays at or below the recorded number of allocations per request.
func TestAPublicPageCostsWhatItWasMeasuredAt(t *testing.T) {
	rig := wiredPublicPageRig(t)
	rig.run()
	if rig.writer.status != http.StatusOK {
		t.Fatalf("the public page answered %d, want 200: %s", rig.writer.status, rig.writer.body)
	}
	per := testing.AllocsPerRun(200, rig.run)
	if rig.loader != 0 {
		t.Fatalf("the page asked the tenant loader %d times, want 0", rig.loader)
	}
	if per > publicPageCeiling {
		t.Errorf("the public page costs %v allocations per request, the ceiling is %d; name the feature that needs the extra %v in the commit that raises this number",
			per, publicPageCeiling, per-publicPageCeiling)
	}
}

// BenchmarkAPublicPageRequest counts and times the anonymous page read through the
// assembled chain: the same constructor, the same middleware and the same warm
// resolution a deployment answers with.
func BenchmarkAPublicPageRequest(b *testing.B) {
	rig := wiredPublicPageRig(b)
	rig.run()
	if rig.writer.status != http.StatusOK {
		b.Fatalf("the public page answered %d, want 200: %s", rig.writer.status, rig.writer.body)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		rig.run()
	}
}

const publicPageInstallation = "platformkit.example"

// publicPageLoader answers one tenant and counts every asking, so a page that
// reached the database would be seen in the count rather than in the number.
type publicPageLoader struct {
	rig *publicPageRig
}

func (l publicPageLoader) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	l.rig.loader++
	return tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}, nil
}

// publicPageAuthorizer answers the permission questions of the measured fixture,
// which declares nothing beyond being public.
type publicPageAuthorizer func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error)

func (f publicPageAuthorizer) Allowed(ctx context.Context, t tenancy.Tenant, g tenancy.Grant) (bool, error) {
	return f(ctx, t, g)
}
