package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestThePurgeKeepsTheClaimOfACommandStillRunning(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	events.DeclareAll([]events.Declared{events.Declare[noteBody]("note.recorded")})
	t.Cleanup(func() { events.DeclareAll(nil) })
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan *httptest.ResponseRecorder, 1)
	finish := sync.OnceFunc(func() { close(release) })
	defer finish()
	writeNote := note(runs)
	mount(t, api, "record-held-note", "/held-event-notes", true, func(ctx context.Context, in *noteIn) (*noteOut, error) {
		out, err := writeNote(ctx, in)
		if err != nil {
			return nil, err
		}
		tx, _ := httpx.TxFrom(ctx)
		if err := events.Publish(ctx, tx, "note.recorded", in.Body); err != nil {
			return nil, err
		}
		if out.Body.Runs == 1 {
			close(started)
			<-release
		}
		return out, nil
	})
	path := at(api, "/held-event-notes")
	go func() { done <- send(t, router, path, keyA, "private original command") }()
	<-started
	// The owner is alive across the claim's deadline, when the scheduled job runs.
	runSystem(t, f, "UPDATE platformkit_idempotency SET claimed_at = now() - interval '5 minutes 1 second', expires_at = now() - interval '1 second'")
	if err := httpx.PurgeIdempotency(t.Context(), f.app); err != nil {
		t.Fatal(err)
	}
	second := send(t, router, path, keyA, "private original command")
	if second.Code != http.StatusConflict {
		t.Errorf("repeat while owner is running after purge = %d %s; want 409", second.Code, second.Body)
	}
	if strings.Contains(second.Body.String(), "private original command") {
		t.Error("repeat returned the domain row instead of a refusal")
	}
	finish()
	if first := <-done; first.Code != http.StatusOK {
		t.Fatalf("owning command = %d %s; want 200", first.Code, first.Body)
	}
	for _, table := range []string{"notes", "platformkit_outbox"} {
		if n := countRows(t, f, table); n != 1 {
			t.Errorf("one submission committed %d rows in %s; want 1", n, table)
		}
	}
	if n := runs.Load(); n != 1 {
		t.Errorf("one submission ran %d times; want 1", n)
	}
}
