package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// A generated list draws its declared status column as a pill on every row. The pill
// states a row's standing; the one thing a person can do from the list is the create
// action, so the screen carries exactly one primary emphasis however many rows wear
// a pill.
func TestAListWithAStatusColumnOffersOnePrimaryAction(t *testing.T) {
	r := hintedResource(entity.EntryHints{
		Singular: "entry", Plural: "entries", PrimaryField: "body", StatusField: "status",
	}, nil)
	r.Schema.Fields = []entity.Field{
		{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
		{Name: "body", Type: entity.TypeString}, {Name: "status", Type: entity.TypeString},
	}
	rows := []map[string]any{
		{"id": "1", "body": "Customer renewal", "status": "open"},
		{"id": "2", "body": "Supplier review", "status": "closed"},
		{"id": "3", "body": "Office move", "status": "waiting"},
	}
	body := render(t, resource.List(r, opts, rows, int64(len(rows)), 1, "", true).Body)
	// The create action and every row's pill are on the screen, so the count below is
	// over a list that draws both.
	for _, want := range []string{"New entry", ">open<", ">closed<", ">waiting<"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the list lacks %q:\n%s", want, body)
		}
	}
	if got := strings.Count(body, `data-variant="primary"`); got != 1 {
		t.Errorf("a list of %d rows with a status column carries %d primary actions, want 1", len(rows), got)
	}
}
