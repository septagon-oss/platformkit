package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestAHiddenPrimaryFieldNeverTitlesTheRecord: an author who points
// primaryField at a hidden field does not get its value as the record's title,
// breadcrumb or any other text on the record, the list or the edit form.
func TestAHiddenPrimaryFieldNeverTitlesTheRecord(t *testing.T) {
	r := hintedResource(entity.EntryHints{PrimaryField: "body", Singular: "note", Plural: "notes"},
		fieldsWith(map[string]entity.FieldHints{"body": {Visibility: "hidden"}}))
	row := map[string]any{"id": "11111111-1111-1111-1111-111111111111", "title": "Buy milk",
		"body": "Private body", "status": "open"}
	detail := resource.Detail(r, opts, row, true)
	if detail.Title != "Buy milk" {
		t.Errorf("record title = %q, want the first visible name", detail.Title)
	}
	list := resource.List(r, opts, []map[string]any{row}, 1, 1, "", true)
	form := resource.Form(r, opts, "/app/note/notes/"+row["id"].(string), "Edit note", row, nil, "", false)
	for surface, body := range map[string]string{
		"record": render(t, detail.Body), "list": render(t, list.Body), "form": render(t, form.Body),
	} {
		if !strings.Contains(body, "Buy milk") && surface != "form" {
			t.Errorf("%s does not name the row by its visible field", surface)
		}
		if strings.Contains(body, "Private body") {
			t.Errorf("%s shows the hidden primary field", surface)
		}
	}
}
