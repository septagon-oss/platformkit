package resource_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Post carries the pair that decides the browser's half of the field: a rich text
// body and the row's own write count, which is what a module adds when it keeps a
// version of every revision. The content module's row is exactly this shape.
type Post struct {
	entity.Base
	Title    string `json:"title" validate:"required"`
	Body     string `json:"body,omitempty" gorm:"type:text" ui:"widget:richtext;hide:list"`
	Revision int64  `json:"revision" gorm:"not null;default:1" readOnly:"true" required:"false" doc:"This row's own write count, from 1"`
}

func (Post) TableName() string { return "posts" }

func post() resource.Resource {
	return resource.Resource{
		Schema: entity.Schema{Module: "post", Entity: "post", Path: "/api/v1/post/posts", Fields: entity.Fields[*Post]()},
		Screen: "/app/post/posts",
		API:    "/api/v1/post/posts",
	}
}

// TestARichTextFieldSavesWhenThePersonSaves pins who writes a rich text field of a
// record that counts its own writes. The form is the write: it posts the field with
// the row's write count as its condition, drawn beside the control. Handing the
// browser the same record address and the same tag would start a second writer — a
// PATCH of the one field two seconds after the last keystroke, which moves the row
// off the revision the form carries and gets the person's own Save refused against a
// row that only their typing moved. It keeps no draft either, so the words typed
// before a refused save would live nowhere. An unsaved draft is therefore the only
// thing this field's browser half keeps, and the endpoint and tag are not drawn.
func TestARichTextFieldSavesWhenThePersonSaves(t *testing.T) {
	t.Parallel()
	id := "11111111-1111-1111-1111-111111111111"
	// The row as a screen reads it: the record decoded from its own JSON, so the
	// write count arrives as the number that decoding left it as.
	row := map[string]any{"id": id, "title": "Hours", "body": "## Hours", "revision": json.Number("3")}
	edit := render(t, resource.Form(post(), opts, "/app/post/posts/"+id+"/edit", "Edit", row, nil, "", false).Body)

	if !strings.Contains(edit, `data-richtext-draft="/api/v1/post/posts/`+id+`/body"`) {
		t.Errorf("the edit form does not key the draft by the record's address and the field:\n%s", edit)
	}
	for _, forbidden := range []string{"data-richtext-endpoint=", "data-richtext-etag="} {
		if strings.Contains(edit, forbidden) {
			t.Errorf("the edit form draws %s beside its own Save; the form posts this field and quotes this row's write count doing it, so a browser that PATCHes the field as the person types refuses their own Save and keeps no draft:\n%s", forbidden, edit)
		}
	}
	// The form still quotes the row's write count: the write it posts is refused
	// against a row that moved since the person read it.
	if !strings.Contains(edit, `expectedRevision`) || !strings.Contains(edit, `value="3"`) {
		t.Errorf("the edit form of a record that counts its writes quotes no condition on the write it posts:\n%s", edit)
	}
}
