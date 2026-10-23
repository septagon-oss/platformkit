package task_test

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

func TestRepeatedSLAChecksLeaveADueDateOnlyTaskUnchanged(t *testing.T) {
	admin, _, router, _ := mountedTaskTrail(t)
	code, body := call(t, router, http.MethodPost, path,
		`{"title":"late delivery","dueAt":"2020-01-01T02:00:00+02:00"}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	rowID := id(t, body)
	const stored = `SELECT row_to_json(tasks)::text FROM tasks WHERE id=$1 AND tenant_id=$2`
	var before string
	if err := admin.QueryRowContext(t.Context(), stored, rowID, acme.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		code, body = call(t, router, http.MethodPost, path+"/"+rowID+"/check-sla", "")
		if code != http.StatusOK {
			t.Fatalf("SLA check %d: %d %s", i+1, code, body)
		}
	}
	var after string
	if err := admin.QueryRowContext(t.Context(), stored, rowID, acme.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("SLA checks changed a task with no deadline: before=%s after=%s", before, after)
	}
	var total, created int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*), count(*) FILTER (WHERE name=$1) FROM platformkit_outbox WHERE name LIKE 'task.%' AND tenant_id=$2`,
		contracts.EventCreated, acme.ID).Scan(&total, &created); err != nil {
		t.Fatal(err)
	}
	if total != 1 || created != 1 {
		t.Errorf("outbox has %d task events and %d creations, want only the original creation", total, created)
	}
}
