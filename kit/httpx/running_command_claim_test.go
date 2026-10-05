package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestARunningCommandIsNotAppliedTwiceWhenItsClaimAges(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan *httptest.ResponseRecorder, 1)
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	writeNote := note(runs)
	mount(t, api, "held-note", "/held-note", true, func(ctx context.Context, in *noteIn) (*noteOut, error) {
		out, err := writeNote(ctx, in)
		if err != nil {
			return nil, err
		}
		if out.Body.Runs == 1 {
			close(started)
			<-release
		}
		return out, nil
	})
	path := at(api, "/held-note")
	go func() { done <- send(t, router, path, keyA, "one submission") }()
	<-started
	// Age the database clock boundary while the owning request is still alive.
	// An abandoned marker and an active command cannot safely be treated alike.
	runSystem(t, f, "UPDATE platformkit_idempotency SET claimed_at = now() - interval '5 minutes 1 second', expires_at = now() - interval '1 second'")
	second := send(t, router, path, keyA, "one submission")
	if second.Code != http.StatusConflict {
		t.Errorf("repeat while owner is running returned %d: %s; want 409", second.Code, second.Body)
	}
	finish()
	first := <-done
	if first.Code != http.StatusOK {
		t.Fatalf("owning command returned %d: %s", first.Code, first.Body)
	}
	if n := countRows(t, f, "notes"); n != 1 {
		t.Errorf("one submission committed %d notes; want one", n)
	}
	if n := runs.Load(); n != 1 {
		t.Errorf("one submission ran %d times; want one", n)
	}
}
