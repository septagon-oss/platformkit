package history_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestARestoreClosureRechecksTheRecordsWritePermission(t *testing.T) {
	w := newWorld(t)
	writer := w.mount(grants{memoRead: true, memoWrite: true})
	id := create(t, writer, acmeHost, "First draft")
	if code, body := edit(t, writer, acmeHost, id, "Second draft"); code != http.StatusOK {
		t.Fatalf("edit = %d %s", code, body)
	}
	api, reader := mountedHistorySpec(t, w, rest.Spec[*Memo]{
		Module: "memos", Entity: "memo", Path: "/memo", Read: memoRead, Write: memoWrite,
		History: true, Versions: w.store, Immutable: []string{"status"},
	}, grants{memoRead: true})
	var command httpx.Command
	for _, c := range api.Resources()[0].Commands {
		if c.Verb == "restore" {
			command = c
		}
	}
	if command.Run == nil {
		t.Fatal("the resource has no restore command closure")
	}
	httpx.Register(api.Surfaces("memos").App, huma.Operation{
		OperationID: "memos-memo-reader-action", Method: http.MethodPost, Path: "/reader-action",
	}, httpx.Permission(memoRead), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		return nil, rest.Fault(command.Run(ctx, id, map[string]any{"version": 1, "expectedRevision": 2}))
	})
	before := w.versionsOf(id)
	code, body := call(t, reader, acmeHost, http.MethodPost, "/api/v1/memos/reader-action", "")
	if code != http.StatusForbidden {
		t.Errorf("a read-only actor invoking the restore closure = %d %s, want 403", code, body)
	}
	if title, revision := w.row(id); title != "Second draft" || revision != 2 {
		t.Errorf("the denied actor's restore changed the row to %q on revision %d", title, revision)
	}
	after := w.versionsOf(id)
	if len(after) != len(before) {
		t.Errorf("the denied actor's restore left %d versions, want %d", len(after), len(before))
	}
	if n := w.outbox(rest.RestoredEvent); n != 0 {
		t.Errorf("the denied actor's restore emitted %d restored events, want none", n)
	}
}
