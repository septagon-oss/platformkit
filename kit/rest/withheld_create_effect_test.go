package rest_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// A withheld create never reaches the write transaction or its outbox publisher.
func TestAWithheldCreateReturnsNoRowAndEmitsNoEvent(t *testing.T) {
	s := spec
	s.Operations = []httpx.CRUD{rest.List, rest.Read}
	_, router, admin := mount(t, s)
	rowID := uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)",
		rowID, acme.ID, "existing row"); err != nil {
		t.Fatal(err)
	}

	status, body := call(t, router, http.MethodPost, "/api/v1/tasks/task",
		`{"title":"unoffered create"}`)
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("POST a resource without create = %d %s, want 405", status, body)
	}
	if strings.Contains(body, "existing row") || strings.Contains(body, rowID.String()) ||
		strings.Contains(body, "unoffered create") {
		t.Errorf("the refusal returned row data: %s", body)
	}
	var rows, events int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM rest_tasks").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(),
		"SELECT count(*) FROM platformkit_outbox WHERE name = $1", s.Event(rest.Created)).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || events != 0 {
		t.Errorf("refused create left %d rows and %d created events, want one existing row and no event", rows, events)
	}
}
