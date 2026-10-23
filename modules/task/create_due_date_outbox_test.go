package task_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

func TestPastDueDatePersistsWithoutAnSLABreachEvent(t *testing.T) {
	admin, _, router, _ := mountedTaskTrail(t)
	due := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	code, body := call(t, router, http.MethodPost, path,
		`{"title":"late delivery","dueAt":"2020-01-01T02:00:00+02:00"}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	var got contracts.Task
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.DueAt == nil || !got.DueAt.Equal(due) || got.SLADeadline != nil || got.SLABreached || got.Revision != 1 {
		t.Fatalf("created task lost its soft due date or recorded a breach: %+v", got)
	}
	var storedDue time.Time
	var deadline sql.NullTime
	var breached bool
	var revision int64
	if err := admin.QueryRowContext(t.Context(),
		`SELECT due_at, sla_deadline, sla_breached, revision FROM tasks WHERE id=$1 AND tenant_id=$2`,
		got.ID, acme.ID).Scan(&storedDue, &deadline, &breached, &revision); err != nil {
		t.Fatal(err)
	}
	if !storedDue.Equal(due) || deadline.Valid || breached || revision != 1 {
		t.Errorf("stored due=%v deadline=%v breached=%v revision=%d", storedDue, deadline, breached, revision)
	}
	var taskEvents int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE name LIKE 'task.%'`).Scan(&taskEvents); err != nil {
		t.Fatal(err)
	}
	if taskEvents != 1 {
		t.Fatalf("outbox has %d task events, want only the creation", taskEvents)
	}
	var payload string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT payload::text FROM platformkit_outbox WHERE name=$1 AND tenant_id=$2`,
		contracts.EventCreated, acme.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var emitted contracts.Task
	if err := json.Unmarshal([]byte(payload), &emitted); err != nil {
		t.Fatal(err)
	}
	if emitted.ID != got.ID || emitted.DueAt == nil || !emitted.DueAt.Equal(due) ||
		emitted.SLADeadline != nil || emitted.SLABreached || emitted.Revision != 1 {
		t.Errorf("creation event does not describe the stored task without a breach: %s", payload)
	}
}
