package resource_test

// A row is called by the field its entity marks `ui:"display"`, ahead of the
// field the schema happens to declare first; a row whose marked field is blank
// is called by the next text-shaped field it has a value for, never by its id
// while one exists. The list's only link, the record's heading and the tab
// title all read that one answer.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Member leads with an address and is called by its name.
type Member struct {
	entity.Base
	Email string `json:"email" gorm:"type:text"`
	Name  string `json:"name" gorm:"type:text" ui:"display"`
}

func (Member) TableName() string { return "members" }

func member() resource.Resource {
	return resource.Resource{
		Schema: entity.Schema{Module: "member", Entity: "member", Path: "/api/v1/member/members", Fields: entity.Fields[*Member]()},
		Screen: "/app/member/members",
	}
}

func TestARowIsCalledByTheFieldItsEntityMarks(t *testing.T) {
	t.Parallel()
	const id = "6f1c2d3e-0000-4000-8000-000000000001"
	row := map[string]any{"id": id, "email": "ada@acme.example", "name": "Ada Lovelace"}

	v := resource.Detail(member(), opts, row, false)
	if v.Title != "Ada Lovelace" {
		t.Errorf("the record's title is %q, want the marked field's value", v.Title)
	}
	out := render(t, resource.List(member(), opts, []map[string]any{row}, 1, 1, "", false).Body)
	link := `href="/app/member/members/` + id + `"`
	at := strings.Index(out, link)
	if at < 0 {
		t.Fatalf("the list draws no link to the row:\n%s", out)
	}
	if end := strings.Index(out[at:], "</a>"); end < 0 || !strings.Contains(out[at:at+end], "Ada Lovelace") {
		t.Errorf("the row's link is not called by the marked field:\n%s", out)
	}

	blank := map[string]any{"id": id, "email": "ada@acme.example", "name": ""}
	if got := resource.Detail(member(), opts, blank, false).Title; got != "ada@acme.example" {
		t.Errorf("a row whose marked field is blank is titled %q, want the next field it has a value for", got)
	}
}
