package httpx_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestAReplayAndItsRefusalsKeepOneDomainEffectAndOneOutboxEvent(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	events.DeclareAll([]events.Declared{events.Declare[noteBody]("note.recorded")})
	t.Cleanup(func() { events.DeclareAll(nil) })
	writeNote := note(runs)
	mount(t, api, "record-event-note", "/event-notes", true, func(ctx context.Context, in *noteIn) (*noteOut, error) {
		out, err := writeNote(ctx, in)
		if err != nil {
			return nil, err
		}
		tx, _ := httpx.TxFrom(ctx)
		return out, events.Publish(ctx, tx, "note.recorded", in.Body)
	})
	path := at(api, "/event-notes")
	first := send(t, router, path, keyA, "private original note")
	if first.Code != http.StatusOK {
		t.Fatalf("first command returned %d: %s", first.Code, first.Body)
	}
	replay := send(t, router, path, keyA, "private original note")
	if replay.Code != first.Code || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay differs: %d %s", replay.Code, replay.Body)
	}
	changed := send(t, router, path, keyA, "another note")
	if changed.Code != http.StatusUnprocessableEntity || strings.Contains(changed.Body.String(), "private original note") {
		t.Fatalf("changed body was not refused without a stale row: %d %s", changed.Code, changed.Body)
	}
	f.allow = false
	denied := send(t, router, path, keyA, "private original note")
	if denied.Code != http.StatusForbidden || strings.Contains(denied.Body.String(), "private original note") {
		t.Fatalf("removed grant still returned the original row: %d %s", denied.Code, denied.Body)
	}
	for _, table := range []string{"notes", "platformkit_outbox", "platformkit_idempotency"} {
		n := 0
		if table == "platformkit_idempotency" {
			n = heldRows(t, f, true)
		} else {
			n = countRows(t, f, table)
		}
		if n != 1 {
			t.Errorf("%s contains %d effects after replay and refusals; want one", table, n)
		}
	}
	if n := runs.Load(); n != 1 {
		t.Errorf("handler ran %d times; want once", n)
	}
}
