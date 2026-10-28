package present_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

// TestTheWordsAnAuthorHintsWinOverWhatResolutionDerives asks that Resolve keeps the
// words a module declared instead of deriving its own. EntryHints.Plural is "notes"
// and CommandHints.Label is "the button's word"; each is documented as winning over a
// default, and modules do write them (modules/user declares Plural "people",
// modules/task declares Label "Assign"). Each sentence is asked with its converse so
// neither can pass by ignoring the hint on one hand or the derivation on the other:
// a hint that is dropped reads as the derived word, and a derivation that is dropped
// reads as an empty one. The resolved object carries both straight onto a page, so a
// dropped hint is a person reading "Notes" where the module said "people".
func TestTheWordsAnAuthorHintsWinOverWhatResolutionDerives(t *testing.T) {
	t.Parallel()

	schema := entity.Schema{Module: "m", Entity: "note", Fields: []entity.Field{
		{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
		{Name: "title", Type: entity.TypeString},
	}}
	one := func(hints entity.CommandHints) present.CommandInput {
		return present.CommandInput{Verb: "archive", Summary: "archive the note", Hints: hints}
	}

	declared := present.Resolve(present.Input{Schema: schema, Hints: entity.EntryHints{Plural: "people"},
		Commands: []present.CommandInput{one(entity.CommandHints{Label: "Archive resolved"}), one(entity.CommandHints{})}})
	if got := declared.Identity.Plural; got != "people" {
		t.Errorf("an entry whose author hinted plural %q resolves plural %q, want the hinted word", "people", got)
	}
	if got := declared.Commands[0].Label; got != "Archive resolved" {
		t.Errorf("a command whose author hinted label %q resolves label %q, want the hinted word", "Archive resolved", got)
	}
	if got := declared.Commands[1].Label; got != "archive the note" {
		t.Errorf("a command beside a hinted one resolves to label %q, want its summary %q", got, "archive the note")
	}

	derived := present.Resolve(present.Input{Schema: schema,
		Commands: []present.CommandInput{one(entity.CommandHints{})}})
	if got := derived.Identity.Plural; got != "notes" {
		t.Errorf("an entry that hinted no plural resolves plural %q, want the derived %q", got, "notes")
	}
	if got := derived.Commands[0].Label; got != "archive the note" {
		t.Errorf("a command that hinted no label resolves label %q, want its summary %q", got, "archive the note")
	}
}
