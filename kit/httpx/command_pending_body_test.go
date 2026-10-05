package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestAChangedBodyCannotDisplaceAnOutstandingCommandsAnswer(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	started, release := make(chan struct{}), make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	defer finish()
	mount(t, api, "pending-note", "/pending-notes", true, func(ctx context.Context, in *noteIn) (*noteOut, error) {
		out, err := note(runs)(ctx, in)
		if err == nil && out.Body.Runs == 1 {
			close(started)
			<-release
		}
		return out, err
	})
	path := at(api, "/pending-notes")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- send(t, router, path, keyA, "private original") }()
	<-started
	refused := send(t, router, path, keyA, "different intent")
	if refused.Code != http.StatusConflict || strings.Contains(refused.Body.String(), "private original") {
		t.Errorf("pending mismatch returned %d %s; want 409 without original row", refused.Code, refused.Body)
	}
	finish()
	first := <-done
	if first.Code != http.StatusOK {
		t.Fatalf("original returned %d: %s", first.Code, first.Body)
	}
	refused = send(t, router, path, keyA, "different intent")
	if refused.Code != http.StatusUnprocessableEntity {
		t.Errorf("settled mismatch returned %d: %s; want 422", refused.Code, refused.Body)
	}
	replay := send(t, router, path, keyA, "private original")
	if replay.Code != first.Code || replay.Body.String() != first.Body.String() || replay.Header().Get("Idempotency-Replay") != "true" {
		t.Errorf("refused caller displaced original answer: %d %s", replay.Code, replay.Body)
	}
	if n := countRows(t, f, "notes"); n != 1 || runs.Load() != 1 || heldRows(t, f, true) != 1 {
		t.Errorf("want one effect, execution and settled claim; effects=%d executions=%d", n, runs.Load())
	}
}
