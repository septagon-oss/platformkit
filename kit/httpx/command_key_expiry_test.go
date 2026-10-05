package httpx_test

import (
	"net/http"
	"testing"
)

func TestAnExpiredCommandKeyStopsReplayingBeforeThePurge(t *testing.T) {
	_, router, f, runs, path := setupNote(t)
	if first := send(t, router, path, keyA, "before expiry"); first.Code != http.StatusOK {
		t.Fatalf("first command returned %d: %s", first.Code, first.Body)
	}
	// Advance the database-owned deadline, as the existing claim-age tests do.
	// The request must enforce expiry without waiting for a scheduled purge.
	runSystem(t, f, "UPDATE platformkit_idempotency SET claimed_at = now() - interval '25 hours', expires_at = now() - interval '1 hour'")
	second := send(t, router, path, keyA, "after expiry")
	if second.Code != http.StatusOK {
		t.Errorf("expired key returned %d: %s; want a fresh command", second.Code, second.Body)
	}
	if replay := second.Header().Get("Idempotency-Replay"); replay != "" {
		t.Errorf("expired key replayed an old answer: %q", replay)
	}
	if n := runs.Load(); n != 2 {
		t.Errorf("after expiry the command ran %d times; want two", n)
	}
}
