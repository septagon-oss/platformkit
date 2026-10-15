package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Article carries the one widget Note does not: a rich text body, which is what
// decides whether a form loads the editor's module.
type Article struct {
	entity.Base
	Title string `json:"title" validate:"required"`
	Body  string `json:"body,omitempty" gorm:"type:text" ui:"widget:richtext;hide:list"`
}

func (Article) TableName() string { return "articles" }

func article() resource.Resource {
	return resource.Resource{
		Schema: entity.Schema{Module: "article", Entity: "article", Path: "/api/v1/article/articles", Fields: entity.Fields[*Article]()},
		Screen: "/app/article/articles",
		API:    "/api/v1/article/articles",
	}
}

// TestOnlyAFormWithAnEditableRichTextFieldLoadsTheEditorModule pins the page
// weight claim: the editor's module is named by the one form that draws an
// editable rich text field, and by no other view — not by a form without the
// widget, not by the record's detail view, not by the list, and not by a form
// whose rich text field is immutable.
func TestOnlyAFormWithAnEditableRichTextFieldLoadsTheEditorModule(t *testing.T) {
	t.Parallel()
	row := map[string]any{"id": "11111111-1111-1111-1111-111111111111", "title": "Hours", "body": "## Hours"}

	withEditor := resource.Form(article(), opts, "/app/article/articles/new", "New", nil, nil, "", true)
	if len(withEditor.Modules) != 1 || withEditor.Modules[0] != "js/richtext.js" {
		t.Fatalf("a form with an editable richtext field names the editor module once; got %v", withEditor.Modules)
	}
	edit := resource.Form(article(), opts, "/app/article/articles/"+row["id"].(string)+"/edit", "Edit", row, nil, "", false)
	if len(edit.Modules) != 1 {
		t.Fatalf("the edit form names the editor module once; got %v", edit.Modules)
	}

	for name, modules := range map[string][]string{
		"a form without a richtext field": resource.Form(note(), opts, "/app/note/notes/new", "New", nil, nil, "", true).Modules,
		"the detail view":                 resource.Detail(article(), opts, row, true).Modules,
		"the list":                        resource.List(article(), opts, []map[string]any{row}, 1, 1, "", true).Modules,
	} {
		if len(modules) != 0 {
			t.Errorf("%s loads %v; only a form with an editable richtext field pays for the editor", name, modules)
		}
	}

	readOnly := article()
	readOnly.Immutable = []string{"body"}
	if modules := resource.Form(readOnly, opts, "/app/article/articles/"+row["id"].(string)+"/edit", "Edit", row, nil, "", false).Modules; len(modules) != 0 {
		t.Errorf("a form whose richtext field is immutable loads %v; read-only text needs no editor", modules)
	}
}

// TestTheEditFormTellsTheBrowserOnlyWhatTheServerKnows pins the attribute
// contract between ui/resource and the glue: a new record has no address to keep
// a draft under and no revision to save against, so neither attribute is drawn;
// an existing record of an entity with no write count gets a draft key and no
// endpoint, because autosave without a tag would be a conflict the client decided.
func TestTheEditFormTellsTheBrowserOnlyWhatTheServerKnows(t *testing.T) {
	t.Parallel()
	id := "11111111-1111-1111-1111-111111111111"
	row := map[string]any{"id": id, "title": "Hours", "body": "## Hours"}

	create := render(t, resource.Form(article(), opts, "/app/article/articles/new", "New", nil, nil, "", true).Body)
	for _, forbidden := range []string{"data-richtext-draft=", "data-richtext-endpoint=", "data-richtext-etag="} {
		if strings.Contains(create, forbidden) {
			t.Errorf("the create form carries %s, but a record that does not exist has nothing to be kept or saved under", forbidden)
		}
	}
	if !strings.Contains(create, `data-richtext-upload="/api/v1/file/files"`) {
		t.Errorf("the create form does not name the upload route:\n%s", create)
	}

	edit := render(t, resource.Form(article(), opts, "/app/article/articles/"+id+"/edit", "Edit", row, nil, "", false).Body)
	if !strings.Contains(edit, `data-richtext-draft="/api/v1/article/articles/`+id+`/body"`) {
		t.Errorf("the edit form does not key the draft by the record's address and the field:\n%s", edit)
	}
	for _, forbidden := range []string{"data-richtext-endpoint=", "data-richtext-etag="} {
		if strings.Contains(edit, forbidden) {
			t.Errorf("the edit form of an entity with no write count carries %s; autosave needs a tag to quote", forbidden)
		}
	}
}
