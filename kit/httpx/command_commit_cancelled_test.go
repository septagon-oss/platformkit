package httpx_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// cancelledCommitAcknowledgement delegates every operation to the real transaction and
// changes one thing: the answer a *successful* COMMIT gives back becomes the caller's
// expired deadline. That is what the driver answers when the cancellation lands while
// it is waiting for the reply to the COMMIT it already sent, and the server may well
// have written the commit before the connection went away. A caller that read this
// error as a rollback would release the key of a write that happened.
type cancelledCommitAcknowledgement struct {
	gorm.ConnPool
	gorm.TxCommitter
	committed atomic.Bool
}

func (c *cancelledCommitAcknowledgement) Commit() error {
	if err := c.TxCommitter.Commit(); err != nil {
		return err
	}
	c.committed.Store(true)
	return context.Canceled
}

// TestACommitCancelledOnTheWayBackDoesNotReleaseTheCommandKey keeps the fence honest
// for the cancellation shape of a lost commit acknowledgement. Review 8's pin breaks
// the answer with io.ErrUnexpectedEOF; a deadline that expires on the way back is the
// same silence wearing the shape of the caller, and the one this middleware is most
// likely to excuse. It must not be excused: the effect is what is counted here.
func TestACommitCancelledOnTheWayBackDoesNotReleaseTheCommandKey(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	var interrupted *cancelledCommitAcknowledgement
	writeNote := note(runs)
	mount(t, api, "cancelled-note", "/cancelled-notes", true, func(ctx context.Context, in *noteIn) (*noteOut, error) {
		out, err := writeNote(ctx, in)
		if err != nil {
			return nil, err
		}
		if interrupted == nil {
			tx, _ := httpx.TxFrom(ctx)
			pool := tx.DB().Statement.ConnPool
			interrupted = &cancelledCommitAcknowledgement{ConnPool: pool, TxCommitter: pool.(gorm.TxCommitter)}
			tx.DB().Statement.ConnPool = interrupted
		}
		return out, nil
	})
	path := at(api, "/cancelled-notes")

	first := send(t, router, path, keyA, "note whose commit was cancelled")
	if interrupted == nil || !interrupted.committed.Load() {
		t.Fatal("the fault did not reach a successful database commit")
	}
	t.Logf("commit cancelled on the way back: %d %s; notes=%d runs=%d",
		first.Code, first.Body, countRows(t, f, "notes"), runs.Load())

	retry := send(t, router, path, keyA, "note whose commit was cancelled")
	t.Logf("same-key retry: %d %s; notes=%d runs=%d", retry.Code, retry.Body, countRows(t, f, "notes"), runs.Load())
	if retry.Code >= 400 && strings.Contains(retry.Body.String(), "note whose commit was cancelled") {
		t.Error("refused retry returned the private domain row")
	}
	if n := countRows(t, f, "notes"); n != 1 {
		t.Errorf("one submission committed %d rows in notes after a commit cancelled on the way back; want one", n)
	}
	if n := runs.Load(); n != 1 {
		t.Errorf("the command ran %d times, once after a commit the caller could not read; want one", n)
	}
}
