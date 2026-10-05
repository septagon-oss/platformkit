package resource_test

// An entity that marks no field and declares only `text` columns — the shape the
// reference app's users had before they were marked — is titled by its first
// writable text field, not by its id; the id is the title only when every
// text-shaped field of the row is blank.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Memo marks nothing and has no `string` column at all.
type Memo struct {
	entity.Base
	Subject string `json:"subject" gorm:"type:text"`
	Body    string `json:"body" gorm:"type:text"`
}

func (Memo) TableName() string { return "memos" }

func memo() resource.Resource {
	return resource.Resource{
		Schema: entity.Schema{Module: "memo", Entity: "memo", Path: "/api/v1/memo/memos", Fields: entity.Fields[*Memo]()},
		Screen: "/app/memo/memos",
	}
}

func TestAnUnmarkedTextEntityIsTitledByItsFirstTextField(t *testing.T) {
	t.Parallel()
	const id = "6f1c2d3e-0000-4000-8000-000000000009"
	cases := []struct {
		name string
		row  map[string]any
		want string
	}{
		{"its first text field", map[string]any{"id": id, "subject": "Quarterly plan", "body": "Draft"}, "Quarterly plan"},
		{"the next text field when the first is blank", map[string]any{"id": id, "subject": "", "body": "Draft"}, "Draft"},
		{"its id when every text field is blank", map[string]any{"id": id, "subject": "", "body": ""}, id},
	}
	for _, c := range cases {
		if got := resource.Detail(memo(), opts, c.row, false).Title; got != c.want {
			t.Errorf("%s: the record is titled %q, want %q", c.name, got, c.want)
		}
	}
}
