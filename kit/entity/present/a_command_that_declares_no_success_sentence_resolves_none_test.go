package present_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

// TestACommandThatDeclaresNoSuccessSentenceResolvesNone asks that resolution keeps
// its own words out of what a command says once it has run: a command whose author
// wrote nothing resolves no success sentence and asks nothing first, so a shell that
// has nothing true to say after the write says nothing.
//
// Both sit in the same struct literal as the label, and the label is one place
// resolution does invent (a command's word falls back to its summary, then to its
// verb), so a fallback written beside it — cmp.Or(h.SuccessMessage, "…") — reaches the
// served document as a sentence the module appears to have chosen. That is the rule
// an entry's two descriptions follow, and it costs nothing here: the resolver has no
// way to know what a write did. The declared half is asked with its converse, so the
// case cannot pass by dropping what an author did write, and the confirmation is
// asked with it, because a shell told to warn before a write cannot un-say a warning
// nobody asked for.
func TestACommandThatDeclaresNoSuccessSentenceResolvesNone(t *testing.T) {
	t.Parallel()

	schema := entity.Schema{Module: "m", Entity: "user", Fields: []entity.Field{
		{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
	}}
	declared := entity.CommandHints{
		Label:          "Publish",
		SuccessMessage: "The note is published.",
		Confirmation: &entity.CommandConfirmation{
			Title: "Publish this note?", Body: "Anyone with the link can read it.", ConfirmLabel: "Publish",
		},
	}
	out := present.Resolve(present.Input{Schema: schema, Commands: []present.CommandInput{
		{Verb: "publish", Summary: "Publish a note", Hints: declared},
		{Verb: "archive", Summary: "Archive this note"},
	}}).Commands
	if len(out) != 2 {
		t.Fatalf("two commands handed, %d resolved, want both, because a shell lists what the caller may call", len(out))
	}

	if out[0].SuccessMessage != declared.SuccessMessage {
		t.Errorf("a command whose author declared %q resolves %q, want the declared sentence, which no other field of the policy carries",
			declared.SuccessMessage, out[0].SuccessMessage)
	}
	if out[0].Confirmation == nil {
		t.Error("a command whose author declared a warning resolves none, want the declared three strings")
	} else if out[0].Confirmation.Title != declared.Confirmation.Title ||
		out[0].Confirmation.Body != declared.Confirmation.Body ||
		out[0].Confirmation.ConfirmLabel != declared.Confirmation.ConfirmLabel {
		t.Errorf("a command whose author declared %q/%q/%q resolves %q/%q/%q, want all three, a person decides on the words that were written",
			declared.Confirmation.Title, declared.Confirmation.Body, declared.Confirmation.ConfirmLabel,
			out[0].Confirmation.Title, out[0].Confirmation.Body, out[0].Confirmation.ConfirmLabel)
	}

	if out[1].SuccessMessage != "" {
		t.Errorf("a command that declared no success sentence resolves %q, want none, which is what a shell reads as an invitation to say it",
			out[1].SuccessMessage)
	}
	if out[1].Confirmation != nil {
		t.Errorf("a command that declared no warning resolves one (%#v), want none, because the platform cannot know what the write costs",
			*out[1].Confirmation)
	}
}
