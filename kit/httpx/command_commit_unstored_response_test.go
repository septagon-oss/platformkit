package httpx_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestAnUnstoredCommandAnswerStillResolvesItsCommit(t *testing.T) {
	for _, flushed := range []bool{false, true} {
		for _, committed := range []bool{false, true} {
			name := "large"
			if flushed {
				name = "flushed"
			}
			if committed {
				name += "/committed"
			} else {
				name += "/rolled-back"
			}
			t.Run(name, func(t *testing.T) {
				api, router, f, runs, _ := setupNote(t)
				events.DeclareAll([]events.Declared{events.Declare[noteBody]("note.recorded")})
				t.Cleanup(func() { events.DeclareAll(nil) })
				writeNote := note(runs)
				var interrupted *lostCommitAcknowledgement
				op := huma.Operation{OperationID: "unstored-note", Method: http.MethodPost, Path: "/unstored-notes"}
				httpx.DeclareIdempotency(&op)
				answer := strings.Repeat("private answer ", 5_000)
				httpx.Register(api.Surfaces(probe).App, op, httpx.Permission("note:write"),
					func(ctx context.Context, in *noteIn) (*huma.StreamResponse, error) {
						if _, err := writeNote(ctx, in); err != nil {
							return nil, err
						}
						tx, _ := httpx.TxFrom(ctx)
						if err := events.Publish(ctx, tx, "note.recorded", in.Body); err != nil {
							return nil, err
						}
						if interrupted == nil {
							pool := tx.DB().Statement.ConnPool
							interrupted = &lostCommitAcknowledgement{ConnPool: pool, TxCommitter: pool.(gorm.TxCommitter)}
							if committed {
								tx.DB().Statement.ConnPool = interrupted
							} else {
								tx.DB().Statement.ConnPool = &abortedCommitAcknowledgement{interrupted}
							}
						}
						return &huma.StreamResponse{Body: func(hctx huma.Context) {
							hctx.SetStatus(http.StatusOK)
							_, _ = hctx.BodyWriter().Write([]byte(answer))
							if flushed {
								hctx.BodyWriter().(http.Flusher).Flush()
							}
						}}, nil
					})
				path := at(api, "/unstored-notes")
				const body = "resolve an answer that cannot be kept"
				first := send(t, router, path, keyA, body)
				before, wantRuns := 0, int64(2)
				if committed {
					before, wantRuns = 1, 1
				}
				if interrupted == nil || interrupted.committed != committed || runs.Load() != 1 {
					t.Fatal("the acknowledgement fault did not reach the intended commit outcome")
				}
				for _, table := range []string{"notes", "platformkit_outbox", effectTable} {
					if n := countRows(t, f, table); n != before {
						t.Fatalf("before recovery, %s has %d rows, want %d", table, n, before)
					}
				}
				if changed := send(t, router, path, keyA, "another command"); changed.Code != http.StatusUnprocessableEntity {
					t.Fatalf("changed body answered %d: %s", changed.Code, changed.Body)
				}
				f.allow = false
				if denied := send(t, router, path, keyA, body); denied.Code != http.StatusForbidden {
					t.Fatalf("revoked grant answered %d: %s", denied.Code, denied.Body)
				}
				f.allow = true
				retry := send(t, router, path, keyA, body)
				if committed {
					if retry.Code != http.StatusConflict || !strings.Contains(retry.Body.String(), httpx.CodeIdempotencyResponseNotHeld) {
						t.Errorf("committed answer that cannot be held must refuse replay: %d %s", retry.Code, retry.Body)
					}
					if strings.Contains(retry.Body.String(), "private answer") {
						t.Error("the refusal returned the old private answer")
					}
				} else if retry.Code != http.StatusOK || retry.Body.String() != answer {
					t.Errorf("uncommitted command must run and return its full answer: %d, bytes=%d", retry.Code, retry.Body.Len())
				}
				for _, table := range []string{"notes", "platformkit_outbox", effectTable} {
					if n := countRows(t, f, table); n != 1 {
						t.Errorf("after recovery, %s has %d rows, want one", table, n)
					}
				}
				if n := runs.Load(); n != wantRuns {
					t.Errorf("handler ran %d times, want %d", n, wantRuns)
				}
				t.Logf("flushed=%t committed=%t first=%d retry=%d runs=%d", flushed, committed, first.Code, retry.Code, runs.Load())
			})
		}
	}
}
