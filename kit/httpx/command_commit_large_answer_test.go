package httpx_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestAnUncommittedCommandWithALargeAnswerCanRunOnItsRetry(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	events.DeclareAll([]events.Declared{events.Declare[noteBody]("note.recorded")})
	t.Cleanup(func() { events.DeclareAll(nil) })
	writeNote := note(runs)
	var interrupted *abortedCommitAcknowledgement
	mount(t, api, "large-answer-note", "/large-answer-notes", true,
		func(ctx context.Context, in *noteIn) (*noteOut, error) {
			out, err := writeNote(ctx, in)
			if err != nil {
				return nil, err
			}
			tx, _ := httpx.TxFrom(ctx)
			if err := events.Publish(ctx, tx, "note.recorded", in.Body); err != nil {
				return nil, err
			}
			out.Body.Text = strings.Repeat("a", 70_000)
			if interrupted == nil {
				pool := tx.DB().Statement.ConnPool
				interrupted = &abortedCommitAcknowledgement{&lostCommitAcknowledgement{
					ConnPool: pool, TxCommitter: pool.(gorm.TxCommitter),
				}}
				tx.DB().Statement.ConnPool = interrupted
			}
			return out, nil
		})
	path := at(api, "/large-answer-notes")
	first := send(t, router, path, keyA, "recover the unapplied command")
	if interrupted == nil || first.Code != http.StatusInternalServerError || runs.Load() != 1 {
		t.Fatalf("did not reach the ambiguous commit: status=%d runs=%d", first.Code, runs.Load())
	}
	for _, table := range []string{"notes", "platformkit_outbox", effectTable} {
		if got := countRows(t, f, table); got != 0 {
			t.Fatalf("the rolled-back transaction left %d rows in %s", got, table)
		}
	}
	retry := send(t, router, path, keyA, "recover the unapplied command")
	t.Logf("first=%d retry=%d runs=%d notes=%d outbox=%d", first.Code, retry.Code,
		runs.Load(), countRows(t, f, "notes"), countRows(t, f, "platformkit_outbox"))
	if retry.Code != http.StatusOK {
		t.Errorf("retry must run the command whose transaction never committed, got %d: %s", retry.Code, retry.Body)
	}
	for _, table := range []string{"notes", "platformkit_outbox", effectTable} {
		if got := countRows(t, f, table); got != 1 {
			t.Errorf("resolved command left %d rows in %s, want one", got, table)
		}
	}
	if got := runs.Load(); got != 2 {
		t.Errorf("handler ran %d times, want one aborted run and one committed run", got)
	}
}
