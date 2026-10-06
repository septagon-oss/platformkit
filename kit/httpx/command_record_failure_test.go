package httpx_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestACommandWhoseAnswerCannotBeStoredIsNotAppliedAgainAfterPurge(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	events.DeclareAll([]events.Declared{events.Declare[noteBody]("note.recorded")})
	t.Cleanup(func() { events.DeclareAll(nil) })
	writeNote := note(runs)
	mount(t, api, "record-note", "/record-notes", true, func(ctx context.Context, in *noteIn) (*noteOut, error) {
		out, err := writeNote(ctx, in)
		if err != nil {
			return nil, err
		}
		tx, _ := httpx.TxFrom(ctx)
		return out, events.Publish(ctx, tx, "note.recorded", in.Body)
	})
	// Fail only the response-record write, after the handler has done its work.
	// Inserts and lease renewals remain available to the real request pipeline.
	f.exec(`CREATE FUNCTION refuse_command_record() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.settled THEN RAISE EXCEPTION 'command response store unavailable'; END IF;
			RETURN NEW;
		END $$`)
	f.exec(`CREATE TRIGGER refuse_command_record BEFORE UPDATE ON platformkit_idempotency
		FOR EACH ROW EXECUTE FUNCTION refuse_command_record()`)
	path := at(api, "/record-notes")
	first := send(t, router, path, keyA, "private original command")
	t.Logf("first response: %d %s; domain rows=%d outbox rows=%d", first.Code, first.Body,
		countRows(t, f, "notes"), countRows(t, f, "platformkit_outbox"))
	f.exec("DROP TRIGGER refuse_command_record ON platformkit_idempotency")
	// Six minutes elapse, inside the promised 24-hour key window. Shift every
	// deadline equally: a safely settled answer still has nearly a day to live.
	runSystem(t, f, `UPDATE platformkit_idempotency
		SET claimed_at = claimed_at - interval '6 minutes',
		    expires_at = expires_at - interval '6 minutes',
		    owner_lease = owner_lease - interval '6 minutes'`)
	if err := httpx.PurgeIdempotency(t.Context(), f.app); err != nil {
		t.Fatal(err)
	}
	retry := send(t, router, path, keyA, "private original command")
	t.Logf("same-key retry: %d %s", retry.Code, retry.Body)
	if retry.Code >= http.StatusBadRequest && strings.Contains(retry.Body.String(), "private original command") {
		t.Error("refused retry returned the private domain row")
	}
	for _, table := range []string{"notes", "platformkit_outbox"} {
		if n := countRows(t, f, table); n != 1 {
			t.Errorf("same submission committed %d rows in %s after the answer store failed; want one", n, table)
		}
	}
}
