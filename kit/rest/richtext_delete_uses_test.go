package rest_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/richtext/richtexttest"
)

// TestADeletedRecordEndsTheUsesOfItsFields: the row and the uses its body filed
// go in one transaction. A ledger row that outlived its record is a file panel
// linking to a record no route answers, and — once the release sweep exists — a
// file nobody can ever release, because something that no longer exists is said
// to be reading it.
func TestADeletedRecordEndsTheUsesOfItsFields(t *testing.T) {
	image := uuid.New()
	files := &richtexttest.FakeFiles{}
	src := "/api/v1/file/files/" + image.String() + "/content"
	files.Put(acme, image, richtext.Image{Src: src, SrcSet: src + " 2w", Width: 2, Height: 3}, false)
	uses := &usesSeen{}
	s := rest.Spec[*richTextCreateOnly]{
		Module: "tasks", Entity: "task", Path: "/task",
		Read: "task:read", Write: "task:write",
		Operations:    []httpx.CRUD{rest.Create, rest.Read, rest.Delete},
		RichTextFiles: files,
		FileUses:      uses,
	}
	_, router, _ := mountAs(t, s, member{"task:write": true})

	code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task",
		`{"title":"one","notes":"![a chart](pk-file:`+image.String()+`)"}`)
	if code != http.StatusCreated {
		t.Fatalf("create with an image = %d %s, want 201", code, body)
	}
	created := id(t, body)

	code, body = call(t, router, http.MethodDelete, "/api/v1/tasks/task/"+created, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete = %d %s, want 204", code, body)
	}

	uses.mu.Lock()
	defer uses.mu.Unlock()
	if len(uses.got) != 2 {
		t.Fatalf("the port was asked %d times, want twice (create, then delete): %+v", len(uses.got), uses.got)
	}
	end := uses.got[1]
	if end.Record.String() != created {
		t.Errorf("the ended use names record %s, want the deleted record %s", end.Record, created)
	}
	if end.Field != "notes" || end.Module != "tasks" || end.Entity != "task" {
		t.Errorf("the ended use names %s.%s field %q, want tasks.task field \"notes\"", end.Module, end.Entity, end.Field)
	}
	if len(end.Files) != 0 {
		t.Errorf("the ended use still shows files %v, want nothing", end.Files)
	}
}
