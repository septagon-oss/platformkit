package httpx_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
)

// TestARefusalThatSaysAskAgainCarriesHowLongToWait puts a retry hint on a
// refusal a handler returns rather than one a guard in this chain makes. The
// saturation that answers "come back in a few seconds" is reached from inside a
// handler — it is the module's own bound on its own work — and the caller needs
// the same header either way, or it retries blind.
func TestARefusalThatSaysAskAgainCarriesHowLongToWait(t *testing.T) {
	api, router, _ := setup(t)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "busy-work", Method: http.MethodPost, Path: "/busy",
	}, httpx.Public(), func(ctx context.Context, in *struct{}) (*struct{}, error) {
		if !httpx.RetryAfter(ctx, 5*time.Second) {
			t.Error("the hint was refused inside a request that still has an answer to write")
		}
		return nil, problem.New(http.StatusServiceUnavailable, "every worker is busy")
	})

	res := request(t, router, http.MethodPost, at(api, "/busy"), host)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d %s, want 503", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Retry-After"); got != "5" {
		t.Errorf("Retry-After = %q, want the 5 seconds the handler asked for", got)
	}
}

// TestAHintAskedForOutsideARequestSaysThereWasNothingToWriteTo keeps the door
// honest about where it cannot be used: with no response being written there is
// no header to set, and the caller is told rather than left reading true.
func TestAHintAskedForOutsideARequestSaysThereWasNothingToWriteTo(t *testing.T) {
	if httpx.RetryAfter(context.Background(), time.Second) {
		t.Error("RetryAfter reported a response outside any request")
	}
}

// TestAWaitUnderASecondIsStillOneSecondIsTheRule Every Retry-After this package
// writes rounds down and never to zero: the shorter answer is the safe one beside
// a window that is draining, and "Retry-After: 0" invites the immediate retry the
// header exists to space out.
func TestAWaitUnderASecondIsStillOneSecond(t *testing.T) {
	api, router, _ := setup(t)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "soon-work", Method: http.MethodPost, Path: "/soon",
	}, httpx.Public(), func(ctx context.Context, in *struct{}) (*struct{}, error) {
		httpx.RetryAfter(ctx, 200*time.Millisecond)
		return nil, problem.New(http.StatusServiceUnavailable, "try again shortly")
	})
	res := request(t, router, http.MethodPost, at(api, "/soon"), host)
	if got := res.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q for a wait under a second, want 1", got)
	}
}
