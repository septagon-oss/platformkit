package task_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
)

func TestTaskCreateCannotSupplyItsOwnHistory(t *testing.T) {
	admin, conn, router, transport := mountedTaskTrail(t)
	code, body := call(t, router, http.MethodPost, path,
		`{"title":"new task","changes":[{"field":"title","before":"forged previous title","after":"forged next title"}]}`)
	if code == http.StatusUnprocessableEntity || code == http.StatusBadRequest {
		var rows, emitted int
		if err := admin.QueryRowContext(t.Context(), "SELECT (SELECT count(*) FROM tasks), (SELECT count(*) FROM platformkit_outbox)").Scan(&rows, &emitted); err != nil {
			t.Fatal(err)
		}
		if rows != 0 || emitted != 0 {
			t.Fatalf("refused create left %d tasks and %d events", rows, emitted)
		}
		return
	}
	if code != http.StatusCreated {
		t.Fatalf("create must reject or discard client history: %d %s", code, body)
	}
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := admin.QueryRowContext(t.Context(), "SELECT payload::text FROM audit_events WHERE name='task.task.created'").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Changes []events.Change `json:"changes"`
	}
	if err := json.Unmarshal([]byte(payload), &recorded); err != nil {
		t.Fatal(err)
	}
	if len(recorded.Changes) != 0 {
		t.Errorf("client supplied a create event's history: %s", payload)
	}
}
