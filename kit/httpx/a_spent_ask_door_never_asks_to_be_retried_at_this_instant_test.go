package httpx_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// closedWindow is a counter whose verdict is no and whose window is already gone.
// kit/limit clamps a countdown at zero rather than below it, so the row-backed
// counter hands exactly that reading when it waits on a key another writer holds
// past the window's end: the row still says the window was open when the event
// landed in it, and the countdown taken at the instant the event landed says none
// is left. Nothing else answers this, so a case that reaches it has to name it.
type closedWindow struct{}

func (closedWindow) Reserve(_ context.Context, _ string, _ int, _ time.Duration) (bool, int, time.Duration, error) {
	return false, 0, 0, nil
}

// TestASpentAskDoorNeverAsksToBeRetriedAtThisInstant refuses an access ask under a
// counter whose window closed as it was read, and asks two figures of that one
// answer: the seconds the door states are left of its window, which are honestly
// zero, and the seconds it asks the caller to sit out, which are never zero — a
// Retry-After of "0" is an instruction to retry this instant, from the one counter
// whose job is to make trying now useless. Writing the first figure into the
// second invites back the traffic the refusal paid for.
func TestASpentAskDoorNeverAsksToBeRetriedAtThisInstant(t *testing.T) {
	const asked = `{"permission":"widget:read","path":"/app/widget/widgets"}`
	r := &reachF{recipients: []uuid.UUID{uuid.New()}}
	h, path, f, records := askSetup(t, r, closedWindow{})
	f.signedIn()
	f.allow = true

	res := askOnce(t, h, path, asked)
	if res.Code != http.StatusTooManyRequests || len(r.told) != 0 || len(*records) != 0 {
		t.Fatalf("an ask past the limit = %d, %d notices, %d records; want 429 and nothing written: %s",
			res.Code, len(r.told), len(*records), res.Body)
	}
	wait, err := strconv.Atoi(res.Header().Get(httpx.RetryAfterHeader))
	if err != nil {
		t.Fatalf("Retry-After = %q on a refusal whose window closed as it was read, want a whole number of seconds: %v",
			res.Header().Get(httpx.RetryAfterHeader), err)
	}
	if wait < 1 {
		t.Errorf("Retry-After = %d on a refusal whose window closed as it was read, want at least 1: a shut door that asks no time at all asks to be retried now", wait)
	}
	if line := res.Header().Get(httpx.RateLimitHeader); !strings.Contains(line, ";t=0") {
		t.Errorf("RateLimit = %q, want the door's own remainder stated as the zero it is beside a wait of %q: the two figures differ at this reading alone", line, res.Header().Get(httpx.RetryAfterHeader))
	}
}
