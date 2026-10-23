package resource_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Two commands that fill the same hidden argument from the displayed row still
// render controls with their own IDs and resolved labels.
func TestBoundHiddenArgumentsKeepEachCommandsOwnID(t *testing.T) {
	field := entity.Field{Name: "owner", Type: entity.TypeString, Doc: "Owner"}
	commands := []resource.Command{
		{Verb: "assign", Fields: []entity.Field{field}, Bindings: map[string]string{"owner": "title"}},
		{Verb: "release", Fields: []entity.Field{field}, Bindings: map[string]string{"owner": "title"}},
	}
	for i := range commands {
		commands[i].Label = commands[i].Verb
		commands[i].Base = "/app/note/notes"
	}
	ids := commandIdentities(t, commandDetail(commands, "one", nil), 2, 2)
	a := ids[[3]string{"/app/note/notes/one/assign", "", "owner"}]
	b := ids[[3]string{"/app/note/notes/one/release", "", "owner"}]
	if a == "" || b == "" || a == b {
		t.Fatalf("bound controls share or lack an ID: %q, %q", a, b)
	}
}
