package admin_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestAssignmentWithoutAPersonRefusesInTheRequestsLanguage(t *testing.T) {
	owner, router := taskLanguageRouter(t)
	created := postTaskInPortuguese(t, router, "/app/task/tasks", "title=Language+case&status=open&priority=normal")
	path := created.Header().Get("Location")
	if created.Code != http.StatusSeeOther || path == "" {
		t.Fatalf("create = %d, location %q", created.Code, path)
	}
	var before int
	var beforeRow string
	if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(t.Context(), `SELECT row_to_json(t)::text FROM tasks t`).Scan(&beforeRow); err != nil {
		t.Fatal(err)
	}
	refused := postTaskInPortuguese(t, router, path+"/assign", "assigneeId=00000000-0000-0000-0000-000000000000")
	if refused.Code != http.StatusUnprocessableEntity || !strings.HasPrefix(refused.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("assignment refusal = %d (%s), want HTML 422", refused.Code, refused.Header().Get("Content-Type"))
	}
	body := refused.Body.String()
	if refused.Header().Get("Content-Language") != "pt-PT" || !strings.Contains(body, `lang="pt-PT"`) {
		t.Errorf("Portuguese assignment refusal declares Content-Language=%q, pt-PT document=%t",
			refused.Header().Get("Content-Language"), strings.Contains(body, `lang="pt-PT"`))
	}
	if strings.Contains(body, "a task is assigned to somebody") {
		t.Error("Portuguese assignment refusal retains English reason: a task is assigned to somebody")
	}
	var status string
	var assigned bool
	var after int
	var afterRow string
	if err := owner.QueryRowContext(t.Context(), `SELECT status, assignee_id IS NOT NULL FROM tasks`).Scan(&status, &assigned); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(t.Context(), `SELECT row_to_json(t)::text FROM tasks t`).Scan(&afterRow); err != nil {
		t.Fatal(err)
	}
	if afterRow != beforeRow {
		t.Error("the refused assignment changed the stored task row")
	}
	if status != "open" || assigned || after != before {
		t.Errorf("refusal left status=%q assigned=%t events=%d (before %d)", status, assigned, after, before)
	}
}
