package resource_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Verbs and argument names that differ only in punctuation or case still
// render controls with distinct IDs and resolved label references.
func TestCommandsWithPunctuationOnlyDifferencesKeepDistinctIDs(t *testing.T) {
	fields := []entity.Field{
		{Name: "a-b", Type: entity.TypeString, Doc: "First"},
		{Name: "a_b", Type: entity.TypeString, Doc: "Second"},
		{Name: "A-B", Type: entity.TypeString, Doc: "Third"},
	}
	var commands []resource.Command
	for _, verb := range []string{"move-on", "move_on", "Move-On", "move.on"} {
		commands = append(commands, resource.Command{Verb: verb, Label: verb, Base: "/app/note/notes",
			Fields: append([]entity.Field(nil), fields...)})
	}
	commandIdentities(t, commandDetail(commands, "one", nil), 4, 12)
}
