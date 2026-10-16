package httpx_test

// The ask door counts twice per person per grant, and is a keyed write: its refusal
// is remembered like any other. So the retry of an ask that was refused must say
// which of the door's two ceilings refused it and how long that window has left —
// six hours is the door's own number, and a retry told to wait one minute comes back
// to the same refusal six hundred times over.

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestReplayedAskRefusalKeepsTheCeilingAndItsWindow spends the ask door's
// per-grant ceiling with real counter statements, refuses one keyed ask with it, and
// asks what the retry is told.
func TestReplayedAskRefusalKeepsTheCeilingAndItsWindow(t *testing.T) {
	const asked = `{"permission":"widget:read","path":"/app/widget/widgets"}`
	_, app := dbtest.Schema(t)
	reach := &reachF{recipients: []uuid.UUID{uuid.New()}}
	f := &fixture{
		tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"},
		app:    app,
		logs:   &lines{},
	}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), Installation: host, PublicHost: host,
		Tenants: f, Conn: app, Authorize: f, Entitle: f, Authenticate: f.authenticate,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Access:       reach,
		WriteLimiter: limit.Memory(),
		Idempotency:  idempotency.Postgres(httpx.ConnFrom),
	})
	api.Declare([]tenancy.Grant{{Permission: "widget:read", Label: "read widgets"}})
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "ask-widget", Method: http.MethodPost, Path: "/asks",
		DefaultStatus: http.StatusAccepted,
	}, httpx.SignedIn(), func(ctx context.Context, in *askInput) (*struct{}, error) {
		return nil, httpx.Ask(ctx, httpx.AccessAsk{Permission: in.Body.Permission, Path: in.Body.Path})
	})
	f.signedIn()
	f.allow = true

	ask := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://"+host+at(api, "/asks"), strings.NewReader(asked))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
		if key != "" {
			req.Header.Set(httpx.IdempotencyKeyHeader, key)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	// The ceiling is three asks for one grant inside its window; these spend it and
	// state no key, so the refusal below is the first attempt at its key rather than
	// a retry of a write somebody else made.
	for range 3 {
		if got := ask(""); got.Code != http.StatusAccepted {
			t.Fatalf("an ask inside the ceiling = %d %s", got.Code, got.Body.String())
		}
	}
	refused := ask("asked-too-often")
	if refused.Code != http.StatusTooManyRequests {
		t.Fatalf("the ask past the ceiling = %d %s, want 429", refused.Code, refused.Body.String())
	}
	wantSeconds, err := strconv.Atoi(refused.Header().Get(httpx.RetryAfterHeader))
	if err != nil {
		t.Fatalf("the refusal's Retry-After = %q, want the window the ceiling is measured over",
			refused.Header().Get(httpx.RetryAfterHeader))
	}
	// The door counts over six hours, so a wait under ten minutes names something
	// other than the ceiling — the kernel's floor, most likely.
	if wantSeconds < int((10 * time.Minute).Seconds()) {
		t.Fatalf("the refusal's Retry-After = %d seconds, which is not the ceiling this door counts under",
			wantSeconds)
	}
	replay := ask("asked-too-often")
	if replay.Code != http.StatusTooManyRequests || replay.Body.String() != refused.Body.String() {
		t.Fatalf("replay = %d %q, want the refusal again", replay.Code, replay.Body.String())
	}
	if len(reach.told) != 3 {
		t.Errorf("the replay told %d people of %d written before it: a replay runs no command",
			len(reach.told), 3)
	}
	if got := replay.Header().Get(httpx.RateLimitPolicyHeader); !strings.Contains(got, `"access-per-permission";q=3;w=21600`) {
		t.Errorf("replayed policy = %q, want the ceiling that refused the ask named on its retry", got)
	}
	got, err := strconv.Atoi(replay.Header().Get(httpx.RetryAfterHeader))
	if err != nil || got < wantSeconds-5 {
		t.Errorf("replayed Retry-After = %q, the deciding ceiling said %d seconds; the window a "+
			"refusal names is six hours and the kernel's one-minute floor is not it",
			replay.Header().Get(httpx.RetryAfterHeader), wantSeconds)
	}
}
