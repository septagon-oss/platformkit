package httpx_test

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
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// keyedWrite is the fixture every case below needs: one anonymous public POST that
// counts its own executions, over an idempotency store and a write limit a case can
// name. The counter is the assertion throughout — a replay that ran the handler is a
// replay that did the write, whatever the answer said — because a status code is what
// a guard claims and a handler run is what it cost.
type keyedWrite struct {
	ran atomic.Int32
	// hold and inside, when set, make the handler wait for the case to let it go,
	// which is how a case gets a request that is genuinely still in flight rather
	// than one it only pretends is.
	hold   chan struct{}
	inside chan struct{}
}

type keyedInput struct {
	Name string `json:"name"`
}

type keyedOut struct {
	ID string `json:"id"`
}

func keyedSetup(t *testing.T, store idempotency.Store, limiter httpx.WriteLimiter) (*httpx.API, http.Handler, *keyedWrite) {
	t.Helper()
	_, app := dbtest.Schema(t)
	who := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	k := &keyedWrite{}
	api, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: host,
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
			if h == host {
				return who, nil
			}
			return tenancy.Tenant{}, tenancy.ErrNoSuchHost
		}),
		Conn: app,
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return true, nil
		}),
		// Nobody signs in here: the door every case posts to is an anonymous one,
		// which is the surface both headers this file is about belong to, and the
		// caller column of its record is uuid.Nil.
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log:          slog.New(slog.NewTextHandler(&lines{}, nil)),
		Idempotency:  store,
		WriteLimiter: limiter,
	})
	// The door every case posts to. Anonymous and public on purpose: it is the
	// surface with no account to lock out, so it is the one that carries both of this
	// delivery's headers, and the caller is uuid.Nil in the record's caller column.
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "keyed-write", Method: http.MethodPost, Path: "/writes",
		DefaultStatus: http.StatusCreated,
	}, httpx.Public(), func(context.Context, *keyedInput) (*keyedOut, error) {
		k.ran.Add(1)
		if k.inside != nil {
			k.inside <- struct{}{}
			<-k.hold
		}
		return &keyedOut{ID: "task-1"}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the route does not declare itself: %v", err)
	}
	return api, router, k
}

// keyedPost sends one write with one key and one body. Those three are the whole of
// what the guard can tell two requests apart by.
func keyedPost(t *testing.T, h http.Handler, method, path, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set(httpx.IdempotencyKeyHeader, key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestARetriedWriteIsAnsweredFromTheRecordAndRunsOnce is the promise the header is
// bought for: the caller whose phone retried the POST gets the answer the first
// attempt was given, and the work happens once.
func TestARetriedWriteIsAnsweredFromTheRecordAndRunsOnce(t *testing.T) {
	_, router, k := keyedSetup(t, idempotency.Memory(nil), nil)
	const body = `{"name":"first"}`

	first := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", "abc", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("the first write = %d %s, want 201", first.Code, first.Body.String())
	}
	retry := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", "abc", body)
	if retry.Code != first.Code {
		t.Errorf("the retry = %d, want the first attempt's %d", retry.Code, first.Code)
	}
	if retry.Body.String() != first.Body.String() {
		t.Errorf("the retry answered %s, want the first attempt's %s", retry.Body.String(), first.Body.String())
	}
	if got, want := retry.Header().Get("Content-Type"), first.Header().Get("Content-Type"); got != want {
		t.Errorf("the retry's Content-Type = %q, want %q: a replayed answer is the same answer", got, want)
	}
	if n := k.ran.Load(); n != 1 {
		t.Errorf("the handler ran %d times, want once: a replay that ran is a second write", n)
	}
}

// TestARetriedWriteWithAnotherBodyIsRefusedAndWritesNothing is the 422 the draft
// names: the same key carrying a different write is the caller's mistake, and the
// only way out of it is a fresh key.
func TestARetriedWriteWithAnotherBodyIsRefusedAndWritesNothing(t *testing.T) {
	_, router, k := keyedSetup(t, idempotency.Memory(nil), nil)
	if got := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", "abc", `{"name":"first"}`); got.Code != http.StatusCreated {
		t.Fatalf("the first write = %d %s", got.Code, got.Body.String())
	}
	conflict := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", "abc", `{"name":"second"}`)
	if conflict.Code != http.StatusUnprocessableEntity {
		t.Errorf("the same key with another body = %d %s, want 422", conflict.Code, conflict.Body.String())
	}
	if !strings.Contains(conflict.Body.String(), httpx.CodeIdempotencyConflict) {
		t.Errorf("the refusal names no code the client can branch on: %s", conflict.Body.String())
	}
	if n := k.ran.Load(); n != 1 {
		t.Errorf("a refused second attempt ran the handler %d times, want once", n)
	}
	// And the answer the first attempt earned is still there to be replayed: a
	// conflict at the door may not spend the record behind it.
	if again := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", "abc", `{"name":"first"}`); again.Code != http.StatusCreated {
		t.Errorf("the original write no longer replays: %d %s", again.Code, again.Body.String())
	}
}

// TestAWriteStillInFlightIsRefused is the 409, and the case that proves the claim is
// taken before the handler runs rather than after: the second request arrives while
// the first is inside the handler, which is the only moment a 409 is the true answer
// rather than a guess.
func TestAWriteStillInFlightIsRefused(t *testing.T) {
	_, router, k := keyedSetup(t, idempotency.Memory(nil), nil)
	k.hold, k.inside = make(chan struct{}), make(chan struct{})

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", "abc", `{"name":"one"}`)
	}()
	<-k.inside

	inflight := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", "abc", `{"name":"one"}`)
	close(k.hold)
	if inflight.Code != http.StatusConflict {
		t.Errorf("a retry while the first runs = %d %s, want 409", inflight.Code, inflight.Body.String())
	}
	if !strings.Contains(inflight.Body.String(), httpx.CodeIdempotencyInFlight) {
		t.Errorf("the 409 names no code: %s", inflight.Body.String())
	}
	if first := <-done; first.Code != http.StatusCreated {
		t.Errorf("the first attempt = %d %s, want 201", first.Code, first.Body.String())
	}
	if n := k.ran.Load(); n != 1 {
		t.Errorf("the handler ran %d times, want once", n)
	}
}

// TestAKeyThatIsNotAKeyIsRefusedBeforeAnythingRuns, and the reason the syntax gate
// exists at all: the key is a primary-key column, so an unbounded or exotic value is
// a row anybody can mint, or a record two clients cannot agree on.
func TestAKeyThatIsNotAKeyIsRefusedBeforeAnythingRuns(t *testing.T) {
	for name, key := range map[string]string{
		"two hundred characters": strings.Repeat("k", 200),
		"a space":                "two words",
		"a comma":                "one,two",
		"a quote inside":         `ab"cd`,
		"empty inside quotes":    `""`,
	} {
		_, router, k := keyedSetup(t, idempotency.Memory(nil), nil)
		got := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", key, `{}`)
		if got.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, got.Code, got.Body.String())
		}
		if !strings.Contains(got.Body.String(), httpx.CodeIdempotencyKey) {
			t.Errorf("%s: the refusal names no code: %s", name, got.Body.String())
		}
		if n := k.ran.Load(); n != 0 {
			t.Errorf("%s: the handler ran %d times for a key it should never have seen", name, n)
		}
	}
	// The shapes the gate does accept, including the draft's Structured-Fields
	// string: refusing a client that quotes its key would be refusing a client that
	// read the same document this delivery writes the parameter into.
	for name, key := range map[string]string{
		"a token":       "abc-123",
		"a uuid":        "6f1a6b60-3f2e-4c9d-9f2f-1f0f2a3b4c5d",
		"a quoted uuid": `"6f1a6b60-3f2e-4c9d-9f2f-1f0f2a3b4c5d"`,
	} {
		_, router, k := keyedSetup(t, idempotency.Memory(nil), nil)
		if got := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", key, `{}`); got.Code != http.StatusCreated {
			t.Errorf("%s: a key the gate should accept = %d %s", name, got.Code, got.Body.String())
		}
		if n := k.ran.Load(); n != 1 {
			t.Errorf("%s: the handler ran %d times, want once", name, n)
		}
	}
}

// TestAWriteWithoutAKeyIsAnsweredAsItAlwaysWas is the optional half of the header:
// twice, because the first request of a pair cannot tell an unkeyed write from a
// keyed one that happened to be remembered.
func TestAWriteWithoutAKeyIsAnsweredAsItAlwaysWas(t *testing.T) {
	_, router, k := keyedSetup(t, idempotency.Memory(nil), nil)
	for i := range 2 {
		if got := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", "", `{"name":"one"}`); got.Code != http.StatusCreated {
			t.Fatalf("write %d without a key = %d %s", i+1, got.Code, got.Body.String())
		}
	}
	if n := k.ran.Load(); n != 2 {
		t.Errorf("the handler ran %d times, want twice: a write with no key is not remembered", n)
	}
}

// TestTheMemoryIsNotReadByAWriteThatPromisedNothing is the cheap half of the outage
// story, and the expensive half beside it: an idempotency store that is down costs
// nothing to the traffic that never asked to be remembered, and refuses the traffic
// that did rather than running a write it cannot promise about.
func TestTheMemoryIsNotReadByAWriteThatPromisedNothing(t *testing.T) {
	_, router, k := keyedSetup(t, brokenStore{}, nil)
	if got := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", "", `{}`); got.Code != http.StatusCreated {
		t.Errorf("an unkeyed write with the store down = %d %s, want 201", got.Code, got.Body.String())
	}
	if n := k.ran.Load(); n != 1 {
		t.Fatalf("the handler ran %d times, want once", n)
	}
	if got := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", "abc", `{}`); got.Code != http.StatusServiceUnavailable {
		t.Errorf("a keyed write with the store down = %d %s, want 503", got.Code, got.Body.String())
	}
	if n := k.ran.Load(); n != 1 {
		t.Errorf("the store's outage let a keyed write run: the handler ran %d times", n)
	}
}

// TestAPATCHIsKeyedAndAPUTIsNot is the verb rule, said by the two requests that show
// it: PUT is idempotent in the protocol already (RFC 9110, and the draft follows it),
// so a record of it is a body kept for nothing and a replay nobody needed.
func TestAPATCHIsKeyedAndAPUTIsNot(t *testing.T) {
	api, router, k := keyedSetup(t, idempotency.Memory(nil), nil)
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "keyed-put", Method: http.MethodPut, Path: "/replaced",
	}, httpx.Public(), func(context.Context, *keyedInput) (*keyedOut, error) {
		k.ran.Add(1)
		return &keyedOut{ID: "put"}, nil
	})
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "keyed-patch", Method: http.MethodPatch, Path: "/patches",
		// huma answers a PATCH with no body of its own 204; the case wants the
		// first attempt's answer replayed, which needs an answer to replay.
		DefaultStatus: http.StatusOK,
	}, httpx.Public(), func(context.Context, *keyedInput) (*keyedOut, error) {
		k.ran.Add(1)
		return &keyedOut{ID: "patched"}, nil
	})

	for range 2 {
		if got := keyedPost(t, router, http.MethodPatch, "/api/v1/public/probe/patches", "patch-key", `{"name":"one"}`); got.Code != http.StatusOK {
			t.Fatalf("a PATCH = %d %s, want the door's own 200", got.Code, got.Body.String())
		}
	}
	if n := k.ran.Load(); n != 1 {
		t.Errorf("a retried PATCH ran the handler %d times, want once", n)
	}
	before := n0(k)
	for range 2 {
		// huma answers a PUT that returns no body 204; the case is about the count,
		// and any answer the door actually gives will do.
		if got := keyedPost(t, router, http.MethodPut, "/api/v1/public/probe/replaced", "put-key", `{"name":"one"}`); got.Code/100 != 2 {
			t.Fatalf("a PUT = %d %s", got.Code, got.Body.String())
		}
	}
	if n := k.ran.Load() - before; n != 2 {
		t.Errorf("two PUTs of one body ran the handler %d times, want twice: PUT carries no record", n)
	}
}

// TestTheDocumentDeclaresTheHeaderOnWhatTheGuardReplays keeps the document and the
// middleware one decision: the verbs a key is promised on, minus the route that reads
// its own request, minus the control plane.
func TestTheDocumentDeclaresTheHeaderOnWhatTheGuardReplays(t *testing.T) {
	api, _, _ := keyedSetup(t, idempotency.Memory(nil), nil)
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "declared-patch", Method: http.MethodPatch, Path: "/other",
	}, httpx.Public(), func(context.Context, *keyedInput) (*keyedOut, error) { return &keyedOut{}, nil })
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "declared-get", Method: http.MethodGet, Path: "/reads",
	}, httpx.Public(), func(context.Context, *struct{}) (*keyedOut, error) { return &keyedOut{}, nil })
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "declared-delete", Method: http.MethodDelete, Path: "/gone",
	}, httpx.Public(), func(context.Context, *struct{}) (*keyedOut, error) { return &keyedOut{}, nil })
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "declared-stream", Method: http.MethodPost, Path: "/uploads", Extensions: map[string]any{httpx.StreamedBodyExtension: true},
	}, httpx.Public(), func(context.Context, *struct{}) (*keyedOut, error) { return &keyedOut{}, nil })
	httpx.Register(api.Surfaces(probe).Ops, huma.Operation{
		OperationID: "declared-ops", Method: http.MethodPost, Path: "/control",
	}, httpx.OperatorPermission("ops:write"), func(context.Context, *struct{}) (*keyedOut, error) { return &keyedOut{}, nil })

	want := map[string]bool{
		"keyed-write":     true,  // the POST every case here posts to
		"declared-patch":  true,  // PATCH is a keyed verb
		"declared-get":    false, // GET is idempotent already
		"declared-delete": false, // and so is DELETE, which unsafeMethod would have keyed
		"declared-stream": true,  // it reads its own request, and a key it ignored would be a promise broken
		"declared-ops":    false, // the control plane is not a surface the brief keys
	}
	seen := 0
	for _, op := range api.Recorded() {
		declared := false
		for _, p := range op.Parameters {
			if p != nil && p.Name == httpx.IdempotencyKeyHeader {
				declared = true
			}
		}
		expected, ok := want[op.OperationID]
		if !ok {
			continue
		}
		seen++
		if declared != expected {
			t.Errorf("%s %s declares the header = %v, want %v", op.Method, op.Path, declared, expected)
		}
	}
	if seen != len(want) {
		t.Errorf("the recorded list holds %d of the %d operations this case mounted", seen, len(want))
	}
}

// TestAPublicWriteSaysItsPolicyBeforeItRunsOut and the refusal after it, which are
// the two halves of why the fields exist. Both lines go on every answer this limit
// applies to: the policy because it names the ceiling a client is walking towards and
// costs no statement, the rate because the number a client backs off by has to be the
// one the counter gave for the request it just took. An allowed write that carried no
// rate line left the client to discover the quota by exceeding it, which is the traffic
// these two fields exist to prevent — and the line is the counter's own reading of the
// event it just recorded, not a second look at the row.
func TestAPublicWriteSaysItsPolicyBeforeItRunsOut(t *testing.T) {
	_, router, k := keyedSetup(t, idempotency.Memory(nil), &window{allow: 1})

	first := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", "one", `{}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("the first write = %d %s", first.Code, first.Body.String())
	}
	if got := first.Header().Get("RateLimit-Policy"); !strings.Contains(got, `"public-writes"`) ||
		!strings.Contains(got, "q=60") || !strings.Contains(got, "w=60") {
		t.Errorf(`RateLimit-Policy on an allowed write = %q, want the policy named with its quota and window`, got)
	}
	if got := first.Header().Get("RateLimit"); !strings.Contains(got, `"public-writes";r=59;t=`) {
		t.Errorf(`the allowed write's rate line = %q, want the quota the counter said was left and the window it said was left`, got)
	}
	if got := first.Header().Get("Retry-After"); got != "" {
		t.Errorf("an allowed write carries Retry-After: %q", got)
	}

	second := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/writes", "two", `{}`)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("the write that ran out = %d %s, want 429", second.Code, second.Body.String())
	}
	if got := second.Header().Get("RateLimit-Policy"); !strings.Contains(got, "public-writes") {
		t.Errorf("the 429 carries no policy line: %q", got)
	}
	if got := second.Header().Get("RateLimit"); !strings.Contains(got, `"public-writes";r=58;t=60`) {
		t.Errorf(`the 429's rate line = %q, want the quota the counter reported left and the window it said was left`, got)
	}
	if got := second.Header().Get("Retry-After"); got != "60" {
		t.Errorf("the 429's Retry-After = %q, want the seconds the limiter reported", got)
	}
	if n := k.ran.Load(); n != 1 {
		t.Errorf("the refused write ran the handler: %d runs", n)
	}
}

// TestA429ThatNamedNoWaitStillNamesOne is the floor under every 429. The kernel
// cannot read a handler's own counter — modules/auth's lockout counts in its own
// table and answers its own 429 — and a refusal that says "too many" with no number
// is one a client can only guess at, so the answer names the shortest window this
// package knows and logs which route owed its own.
func TestA429ThatNamedNoWaitStillNamesOne(t *testing.T) {
	api, router, _ := keyedSetup(t, idempotency.Memory(nil), nil)
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "counts-its-own", Method: http.MethodPost, Path: "/too-many",
	}, httpx.Public(), func(context.Context, *struct{}) (*keyedOut, error) {
		return nil, problem.New(http.StatusTooManyRequests, "this route counted its own limit and said no wait")
	})
	if got := keyedPost(t, router, http.MethodPost, "/api/v1/public/probe/too-many", "", `{}`); got.Code != http.StatusTooManyRequests {
		t.Fatalf("the route's own refusal = %d %s", got.Code, got.Body.String())
	}
	req := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/v1/public/probe/too-many", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("the route's own refusal = %d %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got == "" || got == "0" {
		t.Errorf("a 429 the kernel could not read a counter for answered Retry-After = %q, want a wait", got)
	}
}

// brokenStore is the memory that cannot be read or written.
type brokenStore struct{}

var errStoreDown = errors.New("the store is down")

func (brokenStore) Claim(context.Context, idempotency.Scope, time.Time) (idempotency.Verdict, error) {
	return idempotency.Verdict{}, errStoreDown
}

func (brokenStore) Begin(context.Context, idempotency.Scope, uuid.UUID, time.Time) error {
	return errStoreDown
}

func (brokenStore) Complete(context.Context, idempotency.Scope, uuid.UUID, idempotency.Answer, time.Time) error {
	return errStoreDown
}

// n0 is a zero reading of the counter, written so the arithmetic below reads as a
// difference rather than as a magic starting point.
func n0(k *keyedWrite) int32 { return k.ran.Load() }
