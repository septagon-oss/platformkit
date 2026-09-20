package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// oneHost is a composition's own answer to "which tenant is this host". It is the
// tenant module's shape: a host this composition does not own is no such host,
// not a refusal to explain itself.
type oneHost struct {
	fixture
	host   string
	tenant tenancy.Tenant
}

func (o oneHost) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != o.host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return o.tenant, nil
}

// brand is a composition's whole content: one public route at the path every
// composition wants and one static tree at the prefix every composition wants.
// Those two names are the collision a shared router would have — the stylesheet
// one client's HTML references becoming another's — and the reason each Runtime
// gets its own router rather than a subtree.
func brand(which, sheet string) module.Module {
	return module.Module{
		Name: "brand" + which,
		Routes: func(api *httpx.API) {
			httpx.Register(api, huma.Operation{OperationID: "which", Method: http.MethodGet, Path: "/hello"},
				httpx.Public(), func(context.Context, *struct{}) (*helloOut, error) {
					out := &helloOut{}
					out.Body.Tenant = which
					return out, nil
				})
			// The guarded route is the third thing a caller will ask about: it needs a
			// tenant to answer at all, which is what makes it host-authoritative where
			// the public route and the asset tree are not. No permission is declared, so
			// an anonymous request reaches the route and is refused by the auth gate.
			httpx.Register(api, huma.Operation{OperationID: "guarded", Method: http.MethodGet, Path: "/guarded"},
				httpx.SignedIn(), func(context.Context, *struct{}) (*helloOut, error) { return &helloOut{}, nil })
			api.Static("/assets", fstest.MapFS{"site.txt": {Data: []byte(sheet)}})
		},
	}
}

// ask answers one request of a handler without taking an address: the seam exists
// so that the caller owns the listener, so these checks do not need one.
func ask(h http.Handler, host, path string) (int, string) {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+host+path, nil))
	return rec.Code, rec.Body.String()
}

// countingTransport is the application's own transport with Close counted. The
// memory transport closes idempotently and cannot show a double release, so the
// count is what makes Runtime.Close's once-only guarantee fail a test rather than
// sit behind a comment.
type countingTransport struct {
	events.Transport
	closes atomic.Int64
}

func (c *countingTransport) Close() error {
	c.closes.Add(1)
	return nil
}

// twoCompositions is the same client.yaml-shaped fact twice: two configurations,
// two brand trees, two host answers, and — because Start takes no address — one
// listener above them both. The dispatch below is the caller's, deliberately
// spelled out here: the kernel names no host.
func TestTwoStartedCompositionsAnswerOnlyTheirOwnHost(t *testing.T) {
	cfg, opts := compose(t)
	sites := map[string]*Runtime{}
	for _, which := range []string{"alpha", "beta"} {
		c, o := cfg, opts
		c.Server.PublicHost = which + ".test"
		o.Tenants = oneHost{fixture: fixture{}, host: which + ".test", tenant: tenancy.Tenant{ID: uuid.New(), Slug: which}}
		a, err := New(t.Context(), c, []module.Module{brand(which, which+"-stylesheet")}, o)
		if err != nil {
			t.Fatalf("New %s: %v", which, err)
		}
		rt, err := a.Start(t.Context())
		if err != nil {
			t.Fatalf("Start %s: %v", which, err)
		}
		t.Cleanup(func() { _ = rt.Close() })
		sites[which] = rt
	}

	at := func(which, path string) (int, string) {
		rt, ok := sites[which]
		if !ok {
			// The dispatcher's own answer: a host nobody composed has no handler,
			// and reaches no composition's router at all.
			rec := httptest.NewRecorder()
			http.NotFound(rec, httptest.NewRequest(http.MethodGet, "http://elsewhere.test"+path, nil))
			return rec.Code, rec.Body.String()
		}
		rec := httptest.NewRecorder()
		rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+which+".test"+path, nil))
		return rec.Code, rec.Body.String()
	}

	// Each host gets its own composition's page and its own stylesheet, at paths
	// that are identical in both. Asserted on the bytes, not the status: a 200
	// from the wrong brand is exactly what a shared mount table produces.
	for _, which := range []string{"alpha", "beta"} {
		if code, body := at(which, "/hello"); code != http.StatusOK || !strings.Contains(body, which) {
			t.Errorf("%s /hello = %d %s, want %s's page", which, code, body, which)
		}
		code, body := at(which, "/assets/site.txt")
		if code != http.StatusOK || body != which+"-stylesheet" {
			t.Errorf("%s /assets/site.txt = %d %q, want %s's own sheet", which, code, body, which)
		}
	}

	// And a host nobody composed, as this fixture's dispatcher chooses it: it answers
	// 404 itself and asks no Handler at all. That is the caller's default branch, not a
	// property of the seam, and the next block is what the seam actually guarantees.
	if code, body := at("elsewhere", "/hello"); code != http.StatusNotFound || strings.Contains(body, "alpha") || strings.Contains(body, "beta") {
		t.Errorf("unknown host /hello = %d %s, want a 404 that leaks no composition", code, body)
	}

	// Asked of a real Handler instead: which Host reaches a composition is decided
	// above it, because the kernel names no host list, so a foreign Host is not a
	// refusal in itself. What differs is whether the route needs the tenant a Host
	// resolves to. The guarded route does, and answers 404 for a host this
	// composition does not own while still answering its own host; the public route
	// and the asset tree do not, and answer with this composition's own page and own
	// stylesheet to anybody the caller routes here. A dispatcher with a permissive
	// default branch therefore serves alpha's sheet at any Host — the collision this
	// pair of compositions exists to make avoidable, and the caller's to get right.
	if code, _ := ask(sites["alpha"].Handler(), "alpha.test", "/guarded"); code == http.StatusNotFound {
		t.Errorf("own host /guarded = 404, want the anonymous request to reach the route and be refused by the auth gate")
	}
	if code, _ := ask(sites["alpha"].Handler(), "beta.test", "/guarded"); code != http.StatusNotFound {
		t.Errorf("foreign host /guarded = %d, want the 404 that says no site is served at that host", code)
	}
	if code, body := ask(sites["alpha"].Handler(), "beta.test", "/hello"); code != http.StatusOK || !strings.Contains(body, "alpha") {
		t.Errorf("foreign host /hello = %d %s, want alpha's public page: a public route resolves no tenant", code, body)
	}
	if code, body := ask(sites["alpha"].Handler(), "beta.test", "/assets/site.txt"); code != http.StatusOK || body != "alpha-stylesheet" {
		t.Errorf("foreign host /assets/site.txt = %d %q, want alpha's own sheet: the static tree carries no host either", code, body)
	}
}

// TestStartRefusesACompositionAndTakesNoAddress is Run's boot gate arriving in
// Start: the router is built, and refused, before anything listens, so a caller
// that owns the listener learns of a bad composition the same way Run does.
func TestStartRefusesACompositionAndTakesNoAddress(t *testing.T) {
	cfg, opts := compose(t)
	ghost := module.Module{Name: "ghost", Routes: func(api *httpx.API) {
		httpx.Register(api, huma.Operation{OperationID: "haunt", Method: http.MethodGet, Path: "/haunt"},
			httpx.Permission("ghost:read"), func(context.Context, *struct{}) (*helloOut, error) { return &helloOut{}, nil })
	}}
	a, err := New(t.Context(), cfg, []module.Module{ghost}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt, err := a.Start(t.Context())
	if err == nil || !strings.Contains(err.Error(), "ghost:read") {
		t.Fatalf("Start = %v, want the undefined permission", err)
	}
	if rt != nil {
		_ = rt.Close()
		t.Fatal("Start handed back the composition it refused")
	}
	if c, dialErr := net.DialTimeout("tcp", cfg.Server.Addr, time.Second); dialErr == nil {
		_ = c.Close()
		t.Error("Start listened on the address the refused composition would have served")
	}
}

// TestWorkRunsTheComposedJobsWithoutAnAddress is the worker half on its own: the
// module's job runs, nothing is served, and no second process or port is needed
// beside a caller-owned listener. It returns nil because it was asked to stop.
func TestWorkRunsTheComposedJobsWithoutAnAddress(t *testing.T) {
	cfg, opts := compose(t)
	opts.Role = Worker
	// The transport is the caller's choice exactly as it is for Run; naming the
	// in-process one keeps this check to the scheduler and the pool.
	opts.Transport = memory.New()
	ticked := make(chan struct{}, 1)
	var runs atomic.Int64
	ticker := module.Module{Name: "ticker", Jobs: []jobs.Job{{
		Name: "tick", Every: 10 * time.Millisecond, Parallel: true,
		Run: func(context.Context, *db.Conn) error {
			if runs.Add(1) == 1 {
				ticked <- struct{}{}
			}
			return nil
		},
	}}}
	a, err := New(t.Context(), cfg, []module.Module{ticker}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt, err := a.Start(t.Context())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- rt.Work(ctx) }()
	select {
	case <-ticked:
	case <-time.After(5 * time.Second):
		t.Fatal("the composed job never ran")
	}
	// One Runtime, one scheduler: a second Work would run every job twice on the
	// same connection, so it is refused rather than added. The context is bounded
	// because a refusal is expected at once, and a regression that accepted the
	// call would otherwise sit in the scheduler until Go's package timeout killed
	// the whole suite rather than reporting this case.
	secondCtx, cancelSecond := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancelSecond()
	if err := rt.Work(secondCtx); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("a second Work = %v, want the refusal that keeps one scheduler", err)
	}
	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Work: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Work did not return when its context was done")
	}
	if c, dialErr := net.DialTimeout("tcp", cfg.Server.Addr, time.Second); dialErr == nil {
		_ = c.Close()
		t.Error("Work took an address: the worker half of a shared listener must serve nothing")
	}
}

// TestCloseReleasesThePoolOnce is the release a caller that owns the listener now
// has to sequence itself, and the two halves of its promise: readiness asks the
// database and so reports the closed pool instead of serving a stale 200, while
// liveness is a fact about the process and keeps answering; and the transport and
// the pool are each released exactly once however many times the caller closes.
func TestCloseReleasesThePoolOnce(t *testing.T) {
	cfg, opts := compose(t)
	transport := &countingTransport{Transport: memory.New()}
	opts.Transport = transport
	a, err := New(t.Context(), cfg, []module.Module{brand("alpha", "alpha-stylesheet")}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt, err := a.Start(t.Context())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if code, body := ask(rt.Handler(), tenantHost, "/ready"); code != http.StatusOK {
		t.Fatalf("/ready before Close = %d %s, want 200", code, body)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if code, body := ask(rt.Handler(), tenantHost, "/ready"); code != http.StatusServiceUnavailable {
		t.Errorf("/ready after Close = %d %s, want 503 from the released pool", code, body)
	}
	if code, body := ask(rt.Handler(), tenantHost, "/health"); code != http.StatusOK {
		t.Errorf("/health after Close = %d %s, want 200: liveness asks about the process", code, body)
	}
	// A caller's shutdown and a deferred cleanup both closing the Runtime is the
	// normal shape; a second Close returns the first's result and releases nothing
	// a second time, which is the only way to see the once-only guarantee at all.
	for range 2 {
		if err := rt.Close(); err != nil {
			t.Errorf("Close again = %v, want the first call's result", err)
		}
	}
	if n := transport.closes.Load(); n != 1 {
		t.Errorf("the transport was closed %d times, want once", n)
	}
}

// TestTheTransportConstructorMustAnswerWithATransport is the invalid provider
// result a role that works can be configured into: New has refused a missing
// constructor, and a constructor that is present can still answer (nil, nil). That
// is a misconfiguration, not a broker that is down, and it has to refuse the boot
// the way an unreachable broker does — before the port and before a scheduler is
// handed a nil transport to publish through. Both doors answer alike, because Run is
// Start plus its halves.
func TestTheTransportConstructorMustAnswerWithATransport(t *testing.T) {
	cfg, opts := compose(t)
	cfg.NATS.Transport = "jetstream"
	opts.Transports = Transports{Memory: memory.New, JetStream: func(config.NATS) (events.Transport, error) {
		return nil, nil
	}}
	for _, role := range []Role{Worker, All} {
		o := opts
		o.Role = role
		a, err := New(t.Context(), cfg, []module.Module{brand("alpha", "alpha-stylesheet")}, o)
		if err != nil {
			t.Fatalf("New role %s: %v", role, err)
		}
		rt, err := a.Start(t.Context())
		if rt != nil {
			_ = rt.Close()
			t.Errorf("Start role %s handed back a Runtime whose transport constructor answered nothing", role)
		}
		if err == nil || !strings.Contains(err.Error(), "no transport") {
			t.Errorf("Start role %s = %v, want the refusal that names the empty constructor", role, err)
		}
		if c, dialErr := net.DialTimeout("tcp", cfg.Server.Addr, time.Second); dialErr == nil {
			_ = c.Close()
			t.Errorf("Start role %s listened while refusing a transport that does not exist", role)
		}
	}
}

// TestAWorkerServesItsProbesAndNoProductRoute is the smaller surface the role
// implies: Start builds the composition and gates its real routes either way, and
// what a worker then answers is liveness and readiness — never the module's route
// or its assets, which are the web half's to serve.
func TestAWorkerServesItsProbesAndNoProductRoute(t *testing.T) {
	cfg, opts := compose(t)
	opts.Role = Worker
	opts.Transport = memory.New()
	a, err := New(t.Context(), cfg, []module.Module{brand("alpha", "alpha-stylesheet")}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt, err := a.Start(t.Context())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()
	if code, body := ask(rt.Handler(), tenantHost, "/ready"); code != http.StatusOK {
		t.Errorf("/ready as role worker = %d %s, want 200", code, body)
	}
	if code, body := ask(rt.Handler(), tenantHost, "/hello"); code != http.StatusNotFound {
		t.Errorf("/hello as role worker = %d %s, want 404: a worker composes no product route", code, body)
	}
	if code, body := ask(rt.Handler(), tenantHost, "/assets/site.txt"); code != http.StatusNotFound {
		t.Errorf("/assets/site.txt as role worker = %d %s, want 404: no static tree either", code, body)
	}
}

// TestTheBrokerIsAskedOfTheRoleThatNeedsIt is the ordering Run has always had,
// arriving in Start: a worker composition whose transport cannot be built refuses
// before the caller has a listener to serve, while a web composition starts and
// answers without a reachable broker at all. The constructor is the application's
// own, so the failure is supplied rather than provoked over the network.
func TestTheBrokerIsAskedOfTheRoleThatNeedsIt(t *testing.T) {
	cfg, opts := compose(t)
	cfg.NATS.Transport = "jetstream"
	opts.Transports = Transports{Memory: memory.New, JetStream: func(config.NATS) (events.Transport, error) {
		return nil, errors.New("the broker is down")
	}}

	workerOpts := opts
	workerOpts.Role = Worker
	worker, err := New(t.Context(), cfg, []module.Module{brand("alpha", "alpha-stylesheet")}, workerOpts)
	if err != nil {
		t.Fatalf("New worker: %v", err)
	}
	if _, err := worker.Start(t.Context()); err == nil || !strings.Contains(err.Error(), "broker is down") {
		t.Errorf("worker Start = %v, want the transport's own error", err)
	}
	// Bounded, because a regression that opened the listener instead of refusing
	// would otherwise block here until Go's package timeout, not report this case.
	runCtx, cancelRun := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelRun()
	if err := worker.Run(runCtx); err == nil || !strings.Contains(err.Error(), "broker is down") {
		t.Errorf("worker Run = %v, want the same refusal Start gives", err)
	}

	webOpts := opts
	webOpts.Role = Web
	web, err := New(t.Context(), cfg, []module.Module{brand("alpha", "alpha-stylesheet")}, webOpts)
	if err != nil {
		t.Fatalf("New web: %v", err)
	}
	rt, err := web.Start(t.Context())
	if err != nil {
		t.Fatalf("web Start with no reachable broker: %v", err)
	}
	defer rt.Close()
	webWorkCtx, cancelWork := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancelWork()
	if err := rt.Work(webWorkCtx); err == nil || !strings.Contains(err.Error(), "role web") {
		t.Errorf("Work as role web = %v, want the refusal that says this half is not composed", err)
	}
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+tenantHost+"/hello", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "alpha") {
		t.Errorf("/hello as role web = %d %s, want the composition's own page", rec.Code, rec.Body.String())
	}
}

// TestWorkAfterCloseIsRefused is the already-closed guard: Work asks whether this
// Runtime has been closed before it starts a scheduler, so a caller that shuts its
// worker half down and then calls Work again gets a refusal naming the release
// rather than a scheduler churning against a pool it can no longer reach and
// reporting nil at cancellation. It is a check of a state, not a shutdown mechanism:
// a Work already inside the scheduler is stopped by its own context, and the caller
// still stops served requests and that Work before Close. The composed job is the
// witness — a refused Work must never have started it. The context is short because
// the refusal belongs at once, and a regression that accepted the call would
// otherwise sit in the scheduler until Go's package timeout killed the whole suite
// rather than naming this case.
func TestWorkAfterCloseIsRefused(t *testing.T) {
	cfg, opts := compose(t)
	opts.Role = All
	opts.Transport = memory.New()
	var runs atomic.Int64
	ticker := module.Module{Name: "ticker", Jobs: []jobs.Job{{
		Name: "tick", Every: 10 * time.Millisecond, Parallel: true,
		Run: func(context.Context, *db.Conn) error {
			runs.Add(1)
			return nil
		},
	}}}
	a, err := New(t.Context(), cfg, []module.Module{ticker}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt, err := a.Start(t.Context())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	err = rt.Work(ctx)
	elapsed := time.Since(started)
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("Work after Close = %v after %s, want the prompt refusal naming the released Runtime",
			err, elapsed.Round(time.Millisecond))
	}
	if elapsed > time.Second {
		t.Errorf("Work after Close spent %s of its 2s context, want the refusal before a scheduler starts",
			elapsed.Round(time.Millisecond))
	}
	if n := runs.Load(); n != 0 {
		t.Errorf("the composed job ran %d time(s) on a closed Runtime, want none: Work must refuse before it schedules", n)
	}
	// The guard refuses the work half and disturbs no release: Close is still
	// idempotent afterwards, and the second call says the same thing.
	if err := rt.Close(); err != nil {
		t.Errorf("Close after a refused Work = %v, want the first call's result", err)
	}
	// What this refuses is a Work that starts after Close returned. It is not a way
	// to stop a Work already inside the scheduler — stopping the served requests and
	// the earlier Work before Close stays the caller's, so this is the refusal of a
	// mistake and not a shutdown mechanism.
	if err := rt.Work(ctx); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("a second Work after Close = %v, want the same refusal", err)
	}
}

// TestCloseFlushesTheTracesTheRuntimePathMade is the difference between Run and
// this seam, spelled as an assertion rather than a comment. Run is told when the
// process ends, so it can push what the batch processor still holds; a caller that
// owns its listener never calls Run, and Close is the only teardown that path has.
// Without the flush such a process loses every span recorded since the processor's
// last export — and one that started and finished inside a single batch interval
// keeps none of its trace at all.
func TestCloseFlushesTheTracesTheRuntimePathMade(t *testing.T) {
	cfg, opts := compose(t)
	// The flush failure has to surface somewhere, and Close's error is not it; the
	// logger is, which is where Run reports the same loss.
	var logged bytes.Buffer
	opts.Log = slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelError}))
	a, err := New(t.Context(), cfg, []module.Module{brand("tracer", "tracer-stylesheet")}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// New installed the shutdown an empty endpoint gives, which is a function that
	// does nothing. This replaces it with one that counts and fails, so both that
	// Close calls it and what Close does with its failure are observable.
	var flushes atomic.Int64
	a.traces = func(ctx context.Context) error {
		flushes.Add(1)
		if err := ctx.Err(); err != nil {
			t.Errorf("the flush was handed a context already past its deadline: %v", err)
		}
		return errors.New("the collector did not answer")
	}

	rt, err := a.Start(t.Context())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := flushes.Load(); got != 0 {
		t.Fatalf("Start flushed %d times, want the flush to belong to the end of the lifecycle", got)
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := flushes.Load(); got != 1 {
		t.Fatalf("Close flushed %d times, want the last batch pushed once", got)
	}
	// Spans nobody read are a loss; a shutdown that did not finish is a worse one,
	// so Close still answers nil and the log carries the cause. Same bargain Run
	// makes, and the reason the failure is logged rather than returned.
	for range 2 {
		if err := rt.Close(); err != nil {
			t.Errorf("Close again = %v, want the first call's result", err)
		}
	}
	// Run's own deferred flush, reached here directly: a process that goes through
	// Run hits Close and then that defer, and the Once is what keeps the provider
	// from being shut down twice on the only path that does both.
	a.flushTraces(t.Context())
	if got := flushes.Load(); got != 1 {
		t.Errorf("the provider was shut down %d times, want once however many teardowns", got)
	}
	if !strings.Contains(logged.String(), "the collector did not answer") {
		t.Errorf("Close swallowed the flush failure; the log says: %q", logged.String())
	}
}
