package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

type guardedTask struct{ Task }

func (task *guardedTask) Validate(ctx context.Context) error {
	if task.Status == "done" && task.Priority >= 10 {
		return errors.New("a high-priority task cannot be completed")
	}
	return task.Task.Validate(ctx)
}

// Both transports must decide on the committed row after waiting, not just
// acquire a write lock after validating a stale copy. PostgreSQL confirms the
// actual contention before the first writer commits; no sleep orders the writes.
func TestResourceMutationsUseTheLockedRow(t *testing.T) {
	for _, transport := range []string{"json", "screen"} {
		for _, mutation := range []string{"update", "invalid update", "delete"} {
			t.Run(transport+"/"+mutation, func(t *testing.T) {
				var hookPriority int
				s := rest.Spec[*guardedTask]{
					Module: "tasks", Entity: "task", Path: "/task",
					Read: "task:read", Write: "task:write", SoftDelete: true,
					AfterDelete: func(_ context.Context, _ db.Tx[db.Tenant], task *guardedTask) error {
						hookPriority = task.Priority
						return nil
					},
				}
				api, router, admin := mountAs(t, s, caller{})
				code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"shared","status":"open","priority":1}`)
				if code != http.StatusCreated {
					t.Fatalf("create = %d %s", code, body)
				}
				rowID := id(t, body)
				method, path, payload := http.MethodPatch, "/api/v1/tasks/task/"+rowID, `{"notes":"saved after waiting"}`
				wantStatus, event, wantNotes := http.StatusOK, rest.Updated, "saved after waiting"
				switch mutation {
				case "invalid update":
					payload, wantStatus, wantNotes = `{"status":"done"}`, http.StatusUnprocessableEntity, ""
				case "delete":
					method, payload, wantStatus, event, wantNotes = http.MethodDelete, "", http.StatusNoContent, rest.Deleted, ""
				}
				if transport == "screen" {
					resource := api.Resources()[0]
					httpx.Register(api.Surfaces("tasks").App, huma.Operation{OperationID: "screen-mutation-probe", Method: http.MethodPost,
						Path: "/screen-probe/{id}", DefaultStatus: http.StatusOK}, httpx.Permission(s.Write),
						func(ctx context.Context, input *struct {
							ID   uuid.UUID `path:"id"`
							Body map[string]any
						}) (*struct{ Body map[string]any }, error) {
							if mutation == "delete" {
								return nil, resource.Delete(ctx, input.ID)
							}
							row, err := resource.Update(ctx, input.ID, input.Body)
							return &struct{ Body map[string]any }{Body: row}, err
						})
					method, path = http.MethodPost, api.Surfaces("tasks").App.Path("/screen-probe/"+rowID)
					if mutation == "delete" {
						payload, wantStatus = "{}", http.StatusOK
					}
				}

				var work sync.WaitGroup
				defer work.Wait()
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				writer, err := admin.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer writer.Rollback()
				var holderPID int
				if err := writer.QueryRowContext(ctx,
					"UPDATE rest_tasks SET priority = 99 WHERE id = $1 RETURNING pg_backend_pid()", rowID).Scan(&holderPID); err != nil {
					t.Fatal(err)
				}
				request := httptest.NewRequestWithContext(ctx, method, "http://"+host+path, strings.NewReader(payload))
				request.Header.Set("Content-Type", "application/json")
				request.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
				result := make(chan *httptest.ResponseRecorder, 1)
				work.Go(func() {
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					result <- response
				})
				poll := time.Tick(5 * time.Millisecond)
				for {
					var blocked bool
					if err := admin.QueryRowContext(ctx,
						"SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))", holderPID).Scan(&blocked); err != nil {
						t.Fatal(err)
					}
					if blocked {
						break
					}
					select {
					case early := <-result:
						t.Fatalf("mutation did not wait for the held row: %d %s", early.Code, early.Body.String())
					case <-ctx.Done():
						t.Fatal("mutation never contended on the held row")
					case <-poll:
					}
				}
				if err := writer.Commit(); err != nil {
					t.Fatal(err)
				}
				response := <-result
				if response.Code != wantStatus {
					t.Errorf("mutation = %d %s, want %d", response.Code, response.Body.String(), wantStatus)
				}
				var priority int
				var status, notes string
				var deleted bool
				if err := admin.QueryRowContext(t.Context(),
					"SELECT priority, status, notes, deleted_at IS NOT NULL FROM rest_tasks WHERE id = $1", rowID).
					Scan(&priority, &status, &notes, &deleted); err != nil {
					t.Fatal(err)
				}
				if priority != 99 || status != "open" || notes != wantNotes || deleted != (mutation == "delete") {
					t.Errorf("persisted row = priority %d, status %q, notes %q, deleted %v", priority, status, notes, deleted)
				}
				if mutation == "invalid update" {
					if got := count(t, admin, s.Event(event)); got != 0 {
						t.Errorf("refused mutation emitted %d events", got)
					}
					return
				}
				if mutation == "update" {
					var returned map[string]any
					if err := json.Unmarshal(response.Body.Bytes(), &returned); err != nil || returned["priority"] != float64(99) {
						t.Errorf("mutation returned stale row %s: %v", response.Body.String(), err)
					}
				} else if hookPriority != 99 {
					t.Errorf("delete hook read priority %d, want the committed 99", hookPriority)
				}
				var emitted int
				if err := admin.QueryRowContext(t.Context(),
					"SELECT (payload->>'priority')::int FROM platformkit_outbox WHERE name = $1", s.Event(event)).Scan(&emitted); err != nil {
					t.Fatal(err)
				}
				if emitted != 99 || count(t, admin, s.Event(event)) != 1 {
					t.Errorf("event did not describe exactly one committed mutation: priority %d", emitted)
				}
			})
		}
	}
}
