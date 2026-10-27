package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// mountedRoutesResource is one record's edit form, asked of one operation set. Two
// roads carry that set here and they mean different things by an empty one: a screen
// rendered with no kernel behind it — a design export, a golden file — declares no
// operations and means all five, while ui/screens names the verbs whose routes this
// caller may reach, which is an empty list when that is none. A draft address is a
// route's address, so the pair has to be read the same way by both roads.
func mountedRoutesResource(ops []string) resource.Resource {
	return resource.Resource{
		Schema: entity.Schema{Module: "memo", Entity: "memo", Path: "/api/v1/memo/memos", Fields: []entity.Field{
			{Name: "id", Type: entity.TypeString, ReadOnly: true},
			{Name: "body", Type: entity.TypeString, Widget: "richtext"},
		}},
		Screen: "/app/memo/memos", API: "/api/v1/memo/memos", Operations: ops,
	}
}

// TestAnEmptyOperationListMountsNoRouteWhereNilMountsAllFive holds the two readings
// of an unset operation set apart: nil is the absence of a declaration, so every
// route the five could name is mounted and the record's edit form keeps a draft
// address for its rich text field; an empty slice is a list that named nothing, so
// no route is mounted and the same form names no address for a write no one may
// make. The field itself is drawn under both, so the answer below is about the
// address and never about a form that rendered empty.
func TestAnEmptyOperationListMountsNoRouteWhereNilMountsAllFive(t *testing.T) {
	t.Parallel()
	row := map[string]any{"id": "11111111-1111-1111-1111-111111111111", "body": "## Memo"}
	for _, tc := range []struct {
		operations []string
		state      string
		wantDraft  bool
	}{
		{operations: nil, state: "no declaration", wantDraft: true},
		{operations: []string{}, state: "a list that named nothing", wantDraft: false},
		{operations: []string{"list", "read", "update"}, state: "a list naming the write", wantDraft: true},
		{operations: []string{"list", "read"}, state: "a list withholding the write", wantDraft: false},
	} {
		edit := render(t, resource.Form(mountedRoutesResource(tc.operations), opts,
			"/app/memo/memos/"+row["id"].(string)+"/edit", "Edit memo", row, nil, "", false).Body)
		if !strings.Contains(edit, `data-richtext="1"`) || !strings.Contains(edit, `data-richtext-field="body"`) {
			t.Fatalf("the edit form drew no rich text field beside %s:\n%s", tc.state, edit)
		}
		if got := strings.Contains(edit, "data-richtext-draft="); got != tc.wantDraft {
			t.Errorf("the edit form beside %s draws a draft address: %t, want %t\n%s", tc.state, got, tc.wantDraft, edit)
		}
	}
}
