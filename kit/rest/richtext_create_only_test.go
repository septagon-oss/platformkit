package rest_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
)

type richTextCreateOnly struct {
	crud.Base
	Title string `json:"title" validate:"required" ui:"widget:text"`
	Notes string `json:"notes" gorm:"type:text" ui:"widget:richtext"`
}

func (richTextCreateOnly) TableName() string { return "rest_tasks" }

func TestAReadlessRichTextCreateStillValidatesAndStoresMarkdown(t *testing.T) {
	s := rest.Spec[*richTextCreateOnly]{
		Module: "tasks", Entity: "task", Path: "/task",
		Read: "task:read", Write: "task:write",
		Operations:    []httpx.CRUD{rest.Create},
		RichTextFiles: richtext.RejectImages{},
	}
	api, router, admin := mountAs(t, s, member{"task:write": true})
	if got := api.Resources()[0].OperationWords(); len(got) != 1 || got[0] != "create" {
		t.Fatalf("resource offers %v, want only create", got)
	}
	if recorded(t, api, "tasks-task-create") == nil {
		t.Fatal("create route is absent")
	}

	source := "## Notes\r\n\r\n- one\r\n- two"
	want, err := richtext.Normalise(source)
	if err != nil {
		t.Fatal(err)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task",
		`{"title":"one","notes":"## Notes\r\n\r\n- one\r\n- two"}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, body)
	}
	var stored string
	if err := admin.QueryRowContext(t.Context(), "SELECT notes FROM rest_tasks WHERE id = $1", id(t, body)).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != want {
		t.Errorf("stored Markdown = %q, want %q", stored, want)
	}
	if code, _ := call(t, router, http.MethodGet, "/api/v1/tasks/task/"+id(t, body), ""); code != http.StatusNotFound {
		t.Errorf("readless resource answers GET with %d, want 404", code)
	}

	code, body = call(t, router, http.MethodPost, "/api/v1/tasks/task",
		`{"title":"two","notes":"<script>alert(1)</script>"}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "raw HTML") {
		t.Fatalf("unsupported Markdown = %d %s, want 422 naming raw HTML", code, body)
	}
	var rows, events int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM rest_tasks").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = 'tasks.task.created'").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || events != 1 {
		t.Errorf("refused create left %d rows and %d created events, want one of each", rows, events)
	}
}
