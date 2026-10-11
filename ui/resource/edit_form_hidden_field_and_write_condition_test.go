package resource_test

// A generated edit form answers two authors at once: the entity's, who kept a field
// off every screen with `visibility:hidden`, and the server's, which owns the row's
// write count and wants it quoted back as a condition and never typed. The two rules
// are written in one loop and the line after it, so this file holds them together:
// losing either one would still pass the test that checks the other.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Ticket keeps one field off every screen and carries the server's write count, as
// a task does once a composition gives it an approval door.
type Ticket struct {
	entity.Base
	Title    string `json:"title" doc:"Short summary"`
	Origin   string `json:"origin,omitempty" ui:"visibility:hidden" doc:"Where it was raised"`
	Revision int64  `json:"revision" readOnly:"true" required:"false" doc:"This row's own write count, from 1"`
}

func (Ticket) TableName() string { return "tickets" }

func tickets() resource.Resource {
	return resource.Resource{
		Schema: entity.Schema{Module: "ticket", Entity: "ticket", Path: "/api/v1/ticket/tickets", Fields: entity.Fields[*Ticket]()},
		Screen: "/app/ticket/tickets",
	}
}

func ticketForm(t *testing.T, row map[string]any, create bool) string {
	t.Helper()
	action, title := "/api/v1/ticket/tickets/new", "New ticket"
	if !create {
		action, title = "/api/v1/ticket/tickets/1/edit", "Edit ticket"
	}
	example := resource.FormExample("ticket-form", tickets(), opts, action, title, row, nil, "", create)
	var body strings.Builder
	if err := example.Node.Render(&body); err != nil {
		t.Fatal(err)
	}
	return body.String()
}

func TestAnEditFormKeepsAHiddenFieldOffAndQuotesTheWriteCountOnlyAsItsCondition(t *testing.T) {
	t.Parallel()
	// The row as a handler read it: JSON numbers, which is the shape display.Text reads.
	row := map[string]any{"id": "1", "title": "Paint the stairwell", "origin": "sensor", "revision": float64(3)}
	edit, create := ticketForm(t, row, false), ticketForm(t, nil, true)

	for name, body := range map[string]string{"edit": edit, "create": create} {
		if strings.Contains(body, `name="origin"`) {
			t.Errorf("the %s form offers a control for a field declared hidden", name)
		}
		if !strings.Contains(body, `name="title"`) {
			t.Errorf("the %s form does not ask for the title", name)
		}
	}

	typeable := regexp.MustCompile(`<input[^>]*name="revision"[^>]*>`)
	for _, input := range typeable.FindAllString(edit, -1) {
		if !regexp.MustCompile(`\s(readonly|disabled)(\s|=|/?>)`).MatchString(input) {
			t.Errorf("the edit form offers the write count as a field to type: %s", input)
		}
	}

	condition := regexp.MustCompile(`<input[^>]*name="` + entity.RevisionCondition + `"[^>]*>`).FindString(edit)
	if condition == "" {
		t.Fatalf("the edit form quotes no %s condition for a row whose entity counts its writes", entity.RevisionCondition)
	}
	if !strings.Contains(condition, `type="hidden"`) || !strings.Contains(condition, `value="3"`) {
		t.Errorf("the condition is not a hidden input carrying the row's own count: %s", condition)
	}
	if strings.Contains(create, `name="`+entity.RevisionCondition+`"`) {
		t.Error("a create form quotes a write count for a row nobody has read")
	}
}
