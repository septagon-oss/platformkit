package httpx_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// abortedCommitAcknowledgement gives the caller the same transport error as
// lostCommitAcknowledgement, but the real database transaction did not commit.
type abortedCommitAcknowledgement struct {
	*lostCommitAcknowledgement
}

func (c *abortedCommitAcknowledgement) Commit() error {
	if err := c.TxCommitter.Rollback(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

func TestAnAmbiguousCommandCommitResolvesOnTheNextRequest(t *testing.T) {
	for _, committed := range []bool{true, false} {
		name := "rolled-back"
		if committed {
			name = "committed"
		}
		t.Run(name, func(t *testing.T) {
			api, router, f, runs, _ := setupNote(t)
			writeNote := note(runs)
			var interrupted *lostCommitAcknowledgement
			mount(t, api, "resolve-note", "/resolve-notes", true, func(ctx context.Context, in *noteIn) (*noteOut, error) {
				out, err := writeNote(ctx, in)
				if err != nil {
					return nil, err
				}
				if interrupted == nil {
					tx, _ := httpx.TxFrom(ctx)
					pool := tx.DB().Statement.ConnPool
					interrupted = &lostCommitAcknowledgement{ConnPool: pool, TxCommitter: pool.(gorm.TxCommitter)}
					if committed {
						tx.DB().Statement.ConnPool = interrupted
					} else {
						tx.DB().Statement.ConnPool = &abortedCommitAcknowledgement{interrupted}
					}
				}
				return out, nil
			})
			path := at(api, "/resolve-notes")
			first := send(t, router, path, keyA, "recover this command")
			before := 0
			wantRuns := int64(2)
			if committed {
				before, wantRuns = 1, 1
			}
			if interrupted == nil || interrupted.committed != committed || countRows(t, f, "notes") != before {
				t.Fatal("the fault did not reach the intended database outcome")
			}
			retry := send(t, router, path, keyA, "recover this command")
			t.Logf("first=%d retry=%d body=%s notes=%d runs=%d", first.Code, retry.Code, retry.Body, countRows(t, f, "notes"), runs.Load())
			if retry.Code != http.StatusOK {
				t.Errorf("retry must resolve the ambiguous commit, got %d: %s", retry.Code, retry.Body)
			} else {
				var result noteBody
				if err := json.Unmarshal(retry.Body.Bytes(), &result); err != nil || result.Text != "recover this command" {
					t.Errorf("resolved response = %s, decode error = %v", retry.Body, err)
				}
			}
			if got := countRows(t, f, "notes"); got != 1 {
				t.Errorf("resolved command left %d notes, want one", got)
			}
			if got := runs.Load(); got != wantRuns {
				t.Errorf("handler ran %d times, want %d", got, wantRuns)
			}
		})
	}
}
