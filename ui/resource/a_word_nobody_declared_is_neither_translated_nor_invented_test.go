package resource_test

import (
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestAWordNobodyDeclaredIsNeitherTranslatedNorInvented asks the copy seam's half
// of a rule the resolver keeps one layer below: a command's success sentence is the
// module's own words, and where its author wrote none the answer is none. `say`
// returns the declared string before consulting the seam, so a translation table
// keyed by `hints.<module>/<entity>.command.<verb>.<aspect>` cannot speak for a
// write no author described — the seam is a foreign `Formatter` as much as the
// platform's own catalogue, and the served document carries the result whether or
// not a sentence was declared. The declared halves are asked with the undeclared
// one, so the case cannot pass by ignoring the seam, and the key of the undeclared
// word is named, so a seam that answers everything cannot be reached for it.
func TestAWordNobodyDeclaredIsNeitherTranslatedNorInvented(t *testing.T) {
	t.Parallel()

	var asked []string
	words := resource.WordsFor(entity.Schema{Module: "notes", Entity: "note"},
		func(key, fallback string) string {
			asked = append(asked, key)
			return "pt:" + fallback
		})

	spoken := words.Command("publish", entity.CommandHints{
		Label: "Publish", SuccessMessage: "The note is published.",
	})
	quiet := words.Command("archive", entity.CommandHints{Label: "Archive"})

	if spoken.Label != "pt:Publish" || spoken.SuccessMessage != "pt:The note is published." {
		t.Errorf("a declared label and success sentence reach %q and %q, want both through the seam, which is what a second language is for",
			spoken.Label, spoken.SuccessMessage)
	}
	if quiet.SuccessMessage != "" {
		t.Errorf("a command whose author wrote no success sentence reaches %q, want none: the catalogue answers every key here, and the platform has no way to know what this write did",
			quiet.SuccessMessage)
	}
	if slices.Contains(asked, "hints.notes/note.command.archive.successMessage") {
		t.Errorf("the seam was asked for a word nobody declared; asked keys %v", asked)
	}
}
