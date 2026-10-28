package present_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

// TestAnEntryThatDeclaresNoDescriptionResolvesNone asks that resolution keeps its
// own words out of the two description slots: an entry whose author said nothing
// about what it is, or about what an empty list of it means, resolves both
// sentences empty, so a shell reads "nobody decided" and supplies its own.
//
// The declared halves ride one struct literal, and a fallback written there —
// cmp.Or(h.Description, "…") — reaches the served document as a sentence a module
// appears to have chosen. The catalogue goldens notice that today, and they are
// rewritten by UPDATE_GOLDEN=1; this asks the rule in the package that writes the
// field, where a person cannot regenerate the answer away.
func TestAnEntryThatDeclaresNoDescriptionResolvesNone(t *testing.T) {
	t.Parallel()

	schema := entity.Schema{Module: "m", Entity: "user", Fields: []entity.Field{
		{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
		{Name: "title", Type: entity.TypeString},
	}}

	got := present.Resolve(present.Input{Schema: schema}).Identity
	if got.Description != "" {
		t.Errorf("an entry that declared no description resolves %q, want none, which is what a shell reads as an invitation to say it", got.Description)
	}
	if got.EmptyDescription != "" {
		t.Errorf("an entry that declared nothing for an empty list resolves %q, want none", got.EmptyDescription)
	}
}
