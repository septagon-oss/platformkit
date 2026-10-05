package httpx_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestAnUnavailableCommandStoreRefusesWithoutRunningTheCommand(t *testing.T) {
	_, router, f, runs, path := setupNote(t)
	f.exec("DROP TABLE platformkit_idempotency")
	res := send(t, router, path, keyA, "must not run")
	if res.Code != http.StatusServiceUnavailable || !strings.Contains(res.Body.String(), httpx.CodeIdempotencyUnavailable) {
		t.Fatalf("unavailable command store returned %d: %s", res.Code, res.Body)
	}
	if n := runs.Load(); n != 0 {
		t.Errorf("unavailable store silently allowed %d command runs", n)
	}
	for _, table := range []string{"notes", "platformkit_outbox"} {
		if n := countRows(t, f, table); n != 0 {
			t.Errorf("unavailable store left %d rows in %s", n, table)
		}
	}
}
