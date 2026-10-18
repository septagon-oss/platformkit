package rest_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/richtext/richtexttest"
)

type twoRichTextFields struct {
	crud.Base
	Title   string `json:"title" validate:"required" ui:"widget:text"`
	Body    string `json:"body" gorm:"type:text" ui:"widget:richtext"`
	Summary string `json:"summary" gorm:"type:text" ui:"widget:richtext"`
}

func (twoRichTextFields) TableName() string { return "rest_rich_saves" }

const richSavesDDL = `
CREATE TABLE rest_rich_saves (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz,
	title text NOT NULL,
	body text NOT NULL DEFAULT '',
	summary text NOT NULL DEFAULT ''
);
ALTER TABLE rest_rich_saves ENABLE ROW LEVEL SECURITY;
ALTER TABLE rest_rich_saves FORCE ROW LEVEL SECURITY;
CREATE POLICY rest_rich_saves_tenant ON rest_rich_saves
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`

// orderedUses is the file ledger as kit/rest sees it, written down in the order it
// was spoken to. The save-wide pass is only worth having if it happens before the
// first filed use — a ledger that ends a use locks its file exclusively, so a save
// that named one field at a time would hold one picture while asking for the other —
// and a kit that asked after filing would still pass every case that counts calls.
type orderedUses struct {
	mu     sync.Mutex
	calls  []string
	orders []rest.SaveUses
	sets   []rest.UsesInput
}

func (u *orderedUses) OrderUses(_ context.Context, _ db.Tx[db.Tenant], in rest.SaveUses) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls, u.orders = append(u.calls, "order"), append(u.orders, in)
	return nil
}

func (u *orderedUses) SetUses(_ context.Context, _ db.Tx[db.Tenant], in rest.UsesInput) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls, u.sets = append(u.calls, "set"), append(u.sets, in)
	return nil
}

// TestASaveNamesItsWholeRecordBeforeTheLedgerFilesAnyOfIt: one save of two
// richtext fields is one sentence to the ledger, not two, and the record's own
// delete is one too. A wiring that answers FileUsesOrder must be told the record
// first and the fields afterwards — that is what makes the ledger take its exclusive
// locks in one ascending pass — and what it is told about a save is every file every
// richtext field of it names.
func TestASaveNamesItsWholeRecordBeforeTheLedgerFilesAnyOfIt(t *testing.T) {
	body, summary := uuid.New(), uuid.New()
	files := &richtexttest.FakeFiles{}
	uses := &orderedUses{}
	for _, pair := range []struct {
		id    uuid.UUID
		width int
	}{{body, 2}, {summary, 2}} {
		src := "/api/v1/file/files/" + pair.id.String() + "/content"
		files.Put(acme, pair.id, richtext.Image{Src: src, SrcSet: src + " 2w", Width: pair.width, Height: 3}, false)
	}
	s := rest.Spec[*twoRichTextFields]{Name: rest.Name{One: "Task", Other: "Tasks"},
		Module: "tasks", Entity: "task", Path: "/task",
		Read: "task:read", Write: "task:write",
		Operations:    []httpx.CRUD{rest.Create, rest.Read, rest.Delete},
		RichTextFiles: files,
		FileUses:      uses,
	}
	_, router, admin := mountAs(t, s, member{"task:write": true})
	if _, err := admin.ExecContext(t.Context(), richSavesDDL); err != nil {
		t.Fatalf("create the two-field resource: %v", err)
	}

	code, out := call(t, router, http.MethodPost, "/api/v1/tasks/task",
		`{"title":"one","body":"![chart](pk-file:`+body.String()+`)",`+
			`"summary":"![logo](pk-file:`+summary.String()+`)"}`)
	if code != http.StatusCreated {
		t.Fatalf("create with two images = %d %s, want 201", code, out)
	}
	created := id(t, out)

	uses.mu.Lock()
	had, saved := strings.Join(uses.calls, ","), len(uses.calls)
	orders, sets := append([]rest.SaveUses(nil), uses.orders...), append([]rest.UsesInput(nil), uses.sets...)
	uses.mu.Unlock()
	if had != "order,set,set" {
		t.Fatalf("the save asked %q, want \"order,set,set\": the whole record named once, then one use per field", had)
	}
	if len(orders) != 1 {
		t.Fatalf("the save named its record to the ledger %d times, want once", len(orders))
	}
	if orders[0].Record.String() != created || orders[0].Module != "tasks" || orders[0].Entity != "task" {
		t.Errorf("the save named %s/%s %s, want tasks/task %s",
			orders[0].Module, orders[0].Entity, orders[0].Record, created)
	}
	if shown := shownBy(orders[0].Files); len(shown) != 2 || !shown[body.String()] || !shown[summary.String()] {
		t.Errorf("the save named %v, want both richtext fields' files", orders[0].Files)
	}
	if len(sets) != 2 {
		t.Fatalf("the save filed %d uses, want one per richtext field", len(sets))
	}

	// The record's delete ends both fields' uses. It names nothing — the record is
	// gone, so what it shows is what the ledger holds — and it still says so before
	// ending any of it.
	code, out = call(t, router, http.MethodDelete, "/api/v1/tasks/task/"+created, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete = %d %s, want 204", code, out)
	}
	uses.mu.Lock()
	ending := strings.Join(uses.calls[saved:], ",")
	afterOrders, afterSets := append([]rest.SaveUses(nil), uses.orders...), append([]rest.UsesInput(nil), uses.sets...)
	uses.mu.Unlock()
	if ending != "order,set,set" {
		t.Fatalf("the delete asked %q, want \"order,set,set\"", ending)
	}
	if len(afterOrders) != 2 || len(afterOrders[1].Files) != 0 || afterOrders[1].Record.String() != created {
		t.Errorf("the delete named %+v, want one empty naming of %s", afterOrders[1:], created)
	}
	for _, set := range afterSets[2:] {
		if len(set.Files) != 0 || set.Record.String() != created {
			t.Errorf("the delete ended %+v, want the empty set under %s", set, created)
		}
	}
}

func shownBy(ids []uuid.UUID) map[string]bool {
	out := map[string]bool{}
	for _, id := range ids {
		out[id.String()] = true
	}
	return out
}
