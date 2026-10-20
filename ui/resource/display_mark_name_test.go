package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// declaredMember is member() with primaryField pointed at the address every
// row has, and the `ui:"display"` name given the visibility passed in.
func declaredMember(visibility string) resource.Resource {
	r := member()
	r.Present = entity.EntryHints{PrimaryField: "email", Singular: "member", Plural: "members"}
	for i := range r.Schema.Fields {
		if r.Schema.Fields[i].Name == "name" {
			r.Schema.Fields[i].Presentation.Visibility = visibility
		}
	}
	return r
}

// TestAFilledDisplayMarkNamesTheRowAndKeepsThePrimaryFieldListed: with both
// declared, the web names a row by the name it was given, and the address the
// catalogue titles by stays a column of the list.
func TestAFilledDisplayMarkNamesTheRowAndKeepsThePrimaryFieldListed(t *testing.T) {
	t.Parallel()
	const id = "6f1c2d3e-0000-4000-8000-000000000003"
	row := map[string]any{"id": id, "email": "ada@acme.example", "name": "Ada Lovelace"}
	r := declaredMember("")
	if title := resource.Detail(r, opts, row, false).Title; title != "Ada Lovelace" {
		t.Errorf("record title = %q, want the display-marked name", title)
	}
	markup := render(t, resource.List(r, opts, []map[string]any{row}, 1, 1, "", false).Body)
	at := strings.Index(markup, `href="/app/member/members/`+id+`"`)
	if at < 0 {
		t.Fatalf("list has no link to the record:\n%s", markup)
	}
	if end := strings.Index(markup[at:], "</a>"); end < 0 || !strings.Contains(markup[at:at+end], "Ada Lovelace") {
		t.Errorf("list link does not say the display-marked name:\n%s", markup[at:])
	}
	if !strings.Contains(markup, "ada@acme.example") {
		t.Errorf("the declared primaryField is off the list:\n%s", markup)
	}
}

// TestAHiddenDisplayMarkNeverNamesTheRow: a `ui:"display"` field the author
// hid is not the record's title nor any text on its record or list; the row is
// named by its declared primaryField instead.
func TestAHiddenDisplayMarkNeverNamesTheRow(t *testing.T) {
	t.Parallel()
	const id = "6f1c2d3e-0000-4000-8000-000000000004"
	row := map[string]any{"id": id, "email": "ada@acme.example", "name": "Private name"}
	r := declaredMember("hidden")
	detail := resource.Detail(r, opts, row, false)
	if detail.Title != "ada@acme.example" {
		t.Errorf("record title = %q, want the declared primaryField", detail.Title)
	}
	list := resource.List(r, opts, []map[string]any{row}, 1, 1, "", false)
	for surface, body := range map[string]string{"record": render(t, detail.Body), "list": render(t, list.Body)} {
		if !strings.Contains(body, "ada@acme.example") {
			t.Errorf("%s does not name the row by its primaryField", surface)
		}
		if strings.Contains(body, "Private name") {
			t.Errorf("%s shows the hidden display-marked field", surface)
		}
	}
}
