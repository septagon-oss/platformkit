package httpx_test

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
)

func TestPostgresWriteReplaysAfterTheResponseCompletes(t *testing.T) {
	_, router, k := keyedSetup(t, idempotency.Postgres(httpx.ConnFrom), nil)
	const path = "/api/v1/public/probe/writes"
	first := keyedPost(t, router, http.MethodPost, path, "persisted-answer", "{}")
	if first.Code != http.StatusCreated {
		t.Fatalf("first write = %d %s", first.Code, first.Body.String())
	}
	retry := keyedPost(t, router, http.MethodPost, path, "persisted-answer", "{}")
	if retry.Code != first.Code || retry.Body.String() != first.Body.String() {
		t.Errorf("retry = %d %s; want first answer %d %s", retry.Code, retry.Body.String(), first.Code, first.Body.String())
	}
	if got := k.ran.Load(); got != 1 {
		t.Errorf("handler executions = %d, want 1", got)
	}
}
