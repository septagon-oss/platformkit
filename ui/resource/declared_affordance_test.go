package resource_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Message is the entity these two cases render: a storage column whose type
// suggests a control nobody asked for, an author-named control beside it, and a
// column the author took off the list while leaving it orderable.
type Message struct {
	entity.Base
	Subject  string `json:"subject" validate:"required"`
	Body     string `json:"body,omitempty" gorm:"type:text" ui:"hide:list"`
	Notes    string `json:"notes,omitempty" gorm:"type:text" ui:"widget:textarea"`
	ReplyTo  string `json:"replyTo,omitempty" ui:"widget:email"`
	Archived bool   `json:"archived"`
}

func (Message) TableName() string { return "messages" }

func declared() resource.Resource {
	return resource.Resource{
		Schema: entity.Schema{Module: "note", Entity: "message", Path: "/api/v1/note/messages",
			Fields: entity.Fields[*Message]()},
		Screen: "/app/note/messages",
	}
}

var controlPattern = regexp.MustCompile(`<(input|textarea|select)[^>]*name="([a-zA-Z]+)"[^>]*>`)

// controlTag is the opening tag of the control a form posts a field's value in.
func controlTag(markup, name string) (string, bool) {
	for _, tag := range controlPattern.FindAllString(markup, -1) {
		if strings.Contains(tag, `name="`+name+`"`) {
			return tag, true
		}
	}
	return "", false
}

// The list offers a column's order to the person who can see the column: a sort
// chip is the column's affordance, so a field the author took off the list with
// `hide:list` has no chip even though it stays orderable, and the fields the
// list does draw keep theirs.
func TestTheListOffersNoSortChipForAColumnTheAuthorHid(t *testing.T) {
	r := declared()
	rows := []map[string]any{{"id": "1", "subject": "Kept", "body": "hidden prose", "archived": false}}
	out := render(t, resource.List(r, opts, rows, 1, 1, "", false).Body)

	for _, query := range []string{"sort=body", "sort=-body"} {
		if strings.Contains(out, query) {
			t.Errorf("the list offers a sort chip for %s, a column it does not show", query)
		}
	}
	for _, query := range []string{"sort=subject", "sort=-subject", "sort=id", "sort=-id"} {
		if !strings.Contains(out, query) {
			t.Errorf("the list lost the sort chip for %s", query)
		}
	}
	if strings.Contains(out, "<th") && strings.Contains(out, `>Body<`) {
		t.Error("the hidden column is a header as well")
	}
}

// A `text` storage column is what the row holds, not a request for a five-row
// box: the form draws the control the author named, and where the only reason
// for a taller control is the column's type the field stays a plain input.
func TestATextColumnDrawsTheControlItsAuthorNamedAndNoOther(t *testing.T) {
	out := render(t, resource.Form(declared(), opts, "/app/note/messages/new", "New message", nil, nil, "", true).Body)

	control := func(name string) string {
		tag, ok := controlTag(out, name)
		if !ok {
			t.Fatalf("the form drew no control for %s: %s", name, out)
		}
		return tag
	}
	if tag := control("body"); !strings.HasPrefix(tag, "<input") || !strings.Contains(tag, `type="text"`) {
		t.Errorf("a text column with no declared widget drew %s", tag)
	}
	if tag := control("notes"); !strings.HasPrefix(tag, "<textarea") {
		t.Errorf("the author's own textarea was not drawn: %s", tag)
	}
	if tag := control("replyTo"); !strings.HasPrefix(tag, "<input") || !strings.Contains(tag, `type="email"`) {
		t.Errorf("the author's own email control was not drawn: %s", tag)
	}
	if tag := control("archived"); !strings.HasPrefix(tag, "<input") || !strings.Contains(tag, `type="checkbox"`) {
		t.Errorf("a boolean drew %s", tag)
	}
}
