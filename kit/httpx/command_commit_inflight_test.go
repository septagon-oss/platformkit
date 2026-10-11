package httpx_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// inflightCommitAcknowledgement loses the answer after PostgreSQL receives COMMIT,
// while a deferred trigger still holds it. The real transaction finishes separately.
type inflightCommitAcknowledgement struct {
	*lostCommitAcknowledgement
	wait func() error
	done chan error
	sent bool
}

func (c *inflightCommitAcknowledgement) Commit() error {
	go func() { c.done <- c.TxCommitter.Commit() }()
	if err := c.wait(); err != nil {
		return err
	}
	c.sent = true
	return io.ErrUnexpectedEOF
}

func TestACommandWhoseCommitIsStillRunningIsNotAppliedAgain(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	events.DeclareAll([]events.Declared{events.Declare[noteBody]("note.recorded")})
	t.Cleanup(func() { events.DeclareAll(nil) })
	f.exec(`CREATE FUNCTION hold_note_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_advisory_xact_lock(241, pg_backend_pid());
			RETURN NEW;
		END $$`)
	f.exec(`CREATE CONSTRAINT TRIGGER hold_note_commit AFTER INSERT ON notes
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION hold_note_commit()`)
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), f.tenant), 15*time.Second)
	defer cancel()
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	blockDone := make(chan error, 1)
	secondStarted := make(chan struct{}, 1)
	var interrupted *inflightCommitAcknowledgement
	writeNote := note(runs)
	mount(t, api, "inflight-note", "/inflight-notes", true,
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
				secondStarted <- struct{}{}
				return out, nil
			}
			var pid int
			if err := tx.DB().Raw("SELECT pg_backend_pid()").Row().Scan(&pid); err != nil {
				return nil, err
			}
			locked := make(chan struct{})
			go func() {
				blockDone <- db.Run(ctx, f.app, func(_ context.Context, blocker db.Tx[db.Tenant]) error {
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
			case err := <-blockDone:
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
							return reader.DB().Raw("SELECT coalesce(bool_or(lower(query) = 'commit' AND wait_event = 'advisory'), false) FROM pg_stat_activity WHERE pid = ?", pid).Row().Scan(&waiting)
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
	path := at(api, "/inflight-notes")
	first := send(t, router, path, keyA, "one effect through an unfinished commit")
	if interrupted == nil || !interrupted.sent || first.Code != http.StatusInternalServerError {
		t.Fatalf("did not reach the unacknowledged COMMIT: status=%d body=%s", first.Code, first.Body)
	}
	if got := countRows(t, f, "notes"); got != 0 {
		t.Fatalf("the held COMMIT is already visible: %d notes", got)
	}
	retried := make(chan *httptest.ResponseRecorder, 1)
	go func() { retried <- send(t, router, path, keyA, "one effect through an unfinished commit") }()
	var retry *httptest.ResponseRecorder
	select {
	case <-secondStarted:
		t.Error("retry ran the command before the original COMMIT finished")
	case retry = <-retried:
	case <-time.After(500 * time.Millisecond):
	}
	unblock()
	if err := <-blockDone; err != nil {
		t.Fatalf("release the held commit: %v", err)
	}
	if err := <-interrupted.done; err != nil {
		t.Fatalf("original COMMIT did not complete: %v", err)
	}
	if retry == nil {
		retry = <-retried
	}
	t.Logf("first=%d retry=%d runs=%d notes=%d outbox=%d", first.Code, retry.Code,
		runs.Load(), countRows(t, f, "notes"), countRows(t, f, "platformkit_outbox"))
	for _, table := range []string{"notes", "platformkit_outbox"} {
		if got := countRows(t, f, table); got != 1 {
			t.Errorf("one submission committed %d rows in %s, want one", got, table)
		}
	}
	if got := runs.Load(); got != 1 {
		t.Errorf("one submission ran the command %d times, want once", got)
	}
}
