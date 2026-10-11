package httpx_test

// The fence that refuses a press while another submission's COMMIT is still executing is
// only worth what it costs the caller if the key still resolves afterwards: the refusal
// must leave the claim exactly as it found it — unconfirmed, holding its answer, name on
// the submission whose COMMIT was in question — so the next press with that key reads the
// marker and is answered. Otherwise "wait" would be the state a key reached and never left
// inside its own day, which is the failure the marker was added to remove.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestACommandRefusedWhileItsCommitLandsAnswersThePressAfterIt(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	events.DeclareAll([]events.Declared{events.Declare[noteBody]("note.recorded")})
	t.Cleanup(func() { events.DeclareAll(nil) })
	f.exec(`CREATE FUNCTION hold_the_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_advisory_xact_lock(241, pg_backend_pid());
			RETURN NEW;
		END $$`)
	f.exec(`CREATE CONSTRAINT TRIGGER hold_the_commit AFTER INSERT ON notes
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION hold_the_commit()`)
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), f.tenant), 20*time.Second)
	defer cancel()
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	holdDone := make(chan error, 1)
	var interrupted *inflightCommitAcknowledgement
	writeNote := note(runs)
	mount(t, api, "resolved-note", "/resolved-notes", true,
		func(requestCtx context.Context, in *noteIn) (*noteOut, error) {
			out, err := writeNote(requestCtx, in)
			if err != nil {
				return nil, err
			}
			tx, _ := httpx.TxFrom(requestCtx)
			if err := events.Publish(requestCtx, tx, "note.recorded", in.Body); err != nil {
				return nil, err
			}
			if interrupted != nil {
				return out, nil
			}
			var pid int
			if err := tx.DB().Raw("SELECT pg_backend_pid()").Row().Scan(&pid); err != nil {
				return nil, err
			}
			locked := make(chan struct{})
			go func() {
				holdDone <- db.Run(ctx, f.app, func(_ context.Context, blocker db.Tx[db.Tenant]) error {
					if err := blocker.DB().Exec("SELECT pg_advisory_xact_lock(241, ?)", pid).Error; err != nil {
						return err
					}
					close(locked)
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			}()
			select {
			case <-locked:
			case err := <-holdDone:
				return nil, fmt.Errorf("hold the commit: %w", err)
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			pool := tx.DB().Statement.ConnPool
			interrupted = &inflightCommitAcknowledgement{
				lostCommitAcknowledgement: &lostCommitAcknowledgement{ConnPool: pool, TxCommitter: pool.(gorm.TxCommitter)},
				done:                      make(chan error, 1),
				wait: func() error {
					for {
						var waiting bool
						if err := db.Run(ctx, f.app, func(_ context.Context, reader db.Tx[db.Tenant]) error {
							return reader.DB().Raw("SELECT coalesce(bool_or(lower(query) = 'commit' AND wait_event = 'advisory'), false)"+
								" FROM pg_stat_activity WHERE pid = ?", pid).Row().Scan(&waiting)
						}); err != nil {
							return err
						}
						if waiting {
							return nil
						}
						select {
						case <-time.After(10 * time.Millisecond):
						case <-ctx.Done():
							return ctx.Err()
						}
					}
				},
			}
			tx.DB().Statement.ConnPool = interrupted
			return out, nil
		})

	path := at(api, "/resolved-notes")
	const body = "one effect, refused while it lands"
	first := send(t, router, path, keyA, body)
	if interrupted == nil || !interrupted.sent || first.Code != http.StatusInternalServerError {
		t.Fatalf("did not reach the unacknowledged COMMIT: status=%d body=%s", first.Code, first.Body)
	}

	// The press that arrives while that COMMIT is still executing.
	waited := send(t, router, path, keyA, body)
	if waited.Code != http.StatusConflict {
		t.Fatalf("a press during an unfinished COMMIT answered %d: %s", waited.Code, waited.Body)
	}
	if !strings.Contains(waited.Body.String(), httpx.CodeIdempotencyInProgress) {
		t.Errorf("refusal while the commit runs = %s, want %s", waited.Body, httpx.CodeIdempotencyInProgress)
	}
	if got := heldRows(t, f, true); got != 1 {
		t.Fatalf("the refused press left %d claims settled; want the one unconfirmed row still standing", got)
	}

	unblock()
	if err := <-holdDone; err != nil {
		t.Fatalf("release the held commit: %v", err)
	}
	if err := <-interrupted.done; err != nil {
		t.Fatalf("original COMMIT did not complete: %v", err)
	}
	if got := countRows(t, f, "notes"); got != 1 {
		t.Fatalf("the original submission committed %d notes; want one", got)
	}

	// The same press, once the write has landed: the key is answered, not re-run.
	resolved := send(t, router, path, keyA, body)
	if resolved.Code != http.StatusOK {
		t.Fatalf("the press after the commit answered %d: %s", resolved.Code, resolved.Body)
	}
	if got := resolved.Header().Get(httpx.IdempotencyReplayHeader); got != "true" {
		t.Errorf("the resolved press set %s=%q, want true: it must be the answer of the submission that committed",
			httpx.IdempotencyReplayHeader, got)
	}
	var got noteBody
	if err := json.Unmarshal(resolved.Body.Bytes(), &got); err != nil || got.Text != body {
		t.Errorf("resolved response = %s, decode error = %v", resolved.Body, err)
	}
	if n := countRows(t, f, "notes"); n != 1 {
		t.Errorf("the refused press and the one after it left %d notes; want the one the original committed", n)
	}
	if n := countRows(t, f, "platformkit_outbox"); n != 1 {
		t.Errorf("the refused press and the one after it left %d outbox events; want one", n)
	}
	if n := countRows(t, f, effectTable); n != 1 {
		t.Errorf("the command left %d effect markers; want one", n)
	}
	if n := runs.Load(); n != 1 {
		t.Errorf("the handler ran %d times across the three presses; want the one that committed", n)
	}
}
