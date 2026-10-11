package rest_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/richtext/richtexttest"
)

// usesSeen keeps what the kit handed its FileUses port, so a case can compare
// the record a use was filed under with the record the route answered.
type usesSeen struct {
	mu  sync.Mutex
	got []rest.UsesInput
}

func (u *usesSeen) SetUses(_ context.Context, _ db.Tx[db.Tenant], in rest.UsesInput) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.got = append(u.got, in)
	return nil
}

// TestACreatedRecordsImageUseNamesTheRecordItCreated: a create whose body shows
// an image files that use under the id the create answers with — the record a
// details panel links to and a release sweep asks about — and never under the
// nil id a body carries before the row is written.
func TestACreatedRecordsImageUseNamesTheRecordItCreated(t *testing.T) {
	image := uuid.New()
	files := &richtexttest.FakeFiles{}
	src := "/api/v1/file/files/" + image.String() + "/content"
	files.Put(acme, image, richtext.Image{Src: src, SrcSet: src + " 2w", Width: 2, Height: 3}, false)
	uses := &usesSeen{}
	s := rest.Spec[*richTextCreateOnly]{
		Module: "tasks", Entity: "task", Path: "/task",
		Read: "task:read", Write: "task:write",
		Operations:    []httpx.CRUD{rest.Create},
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
	uses.mu.Lock()
	defer uses.mu.Unlock()
	if len(uses.got) != 1 {
		t.Fatalf("the port was asked %d times, want once: %+v", len(uses.got), uses.got)
	}
	in := uses.got[0]
	if in.Record.String() != created {
		t.Errorf("the use is filed under record %s, want the created record %s", in.Record, created)
	}
	if len(in.Files) != 1 || in.Files[0] != image {
		t.Errorf("the use names files %v, want [%s]", in.Files, image)
	}
}
