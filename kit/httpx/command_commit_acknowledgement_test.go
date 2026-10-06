package httpx_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// lostCommitAcknowledgement delegates every operation to the real transaction.
// Only its successful COMMIT acknowledgement is replaced by a transport error:
// a disconnected caller cannot conclude that PostgreSQL rolled the work back.
type lostCommitAcknowledgement struct {
	gorm.ConnPool
	gorm.TxCommitter
	committed bool
}

func (c *lostCommitAcknowledgement) Commit() error {
	if err := c.TxCommitter.Commit(); err != nil {
		return err
	}
	c.committed = true
	return io.ErrUnexpectedEOF
}

func TestALostCommitAcknowledgementDoesNotReleaseTheCommandKey(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	events.DeclareAll([]events.Declared{events.Declare[noteBody]("note.recorded")})
	t.Cleanup(func() { events.DeclareAll(nil) })
	var interrupted *lostCommitAcknowledgement
	writeNote := note(runs)
	mount(t, api, "commit-note", "/commit-notes", true, func(ctx context.Context, in *noteIn) (*noteOut, error) {
		out, err := writeNote(ctx, in)
		if err != nil {
			return nil, err
		}
		tx, _ := httpx.TxFrom(ctx)
		if err := events.Publish(ctx, tx, "note.recorded", in.Body); err != nil {
			return nil, err
		}
		if interrupted == nil {
			pool := tx.DB().Statement.ConnPool
			interrupted = &lostCommitAcknowledgement{ConnPool: pool, TxCommitter: pool.(gorm.TxCommitter)}
			tx.DB().Statement.ConnPool = interrupted
		}
		return out, nil
	})
	path := at(api, "/commit-notes")
	first := send(t, router, path, keyA, "private committed note")
	if interrupted == nil || !interrupted.committed {
		t.Fatal("the fault did not reach a successful database commit")
	}
	t.Logf("lost acknowledgement response: %d %s; notes=%d outbox=%d", first.Code, first.Body,
		countRows(t, f, "notes"), countRows(t, f, "platformkit_outbox"))
	retry := send(t, router, path, keyA, "private committed note")
	t.Logf("same-key retry: %d %s", retry.Code, retry.Body)
	if retry.Code >= 400 && strings.Contains(retry.Body.String(), "private committed note") {
		t.Error("refused retry returned the private domain row")
	}
	for _, table := range []string{"notes", "platformkit_outbox"} {
		if n := countRows(t, f, table); n != 1 {
			t.Errorf("one submission committed %d rows in %s after a lost commit acknowledgement; want one", n, table)
		}
	}
}
