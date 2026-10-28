package view_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestACommandThatAsksFirstArrivesWithTheWordsItsAuthorWrote asks the one page member
// that carries a warning a person has to read before the write: an action's Confirm.
// The author writes its three strings as command hints and the resolved reading hands
// them over, so a page that drops any one of them leaves the shell with a destructive
// button and nothing to say for itself. A sibling command that asks nothing first is
// asked for at the same time: its Confirm stays zero rather than wearing words no
// author wrote.
func TestACommandThatAsksFirstArrivesWithTheWordsItsAuthorWrote(t *testing.T) {
	t.Parallel()

	page := build(t, askingCommandInput())
	wants := view.Confirm{
		Title:        "Archive the whole collection?",
		Body:         "Every task here stops showing on the list.",
		ConfirmLabel: "Archive them",
	}
	asked := actionVerb(t, page, "archive")
	if asked.Confirm != wants {
		t.Errorf("the archive command arrives with confirm %+v, want the three words its author wrote %+v", asked.Confirm, wants)
	}
	if !asked.Destructive {
		t.Errorf("the archive command arrives with destructive false, want the tone its author declared")
	}
	quiet := actionVerb(t, page, "reindex")
	if quiet.Confirm != (view.Confirm{}) {
		t.Errorf("the reindex command, which asks nothing first, arrives with confirm %+v, want the zero Confirm", quiet.Confirm)
	}
}

// askingCommandInput is one list page over two collection commands: the first declares
// the words a person reads first, the second declares none.
func askingCommandInput() view.Input {
	s := taskSchema()
	in := input(view.KindList, listInput().Data.Records, nil)
	in.Policy = present.Resolve(present.Input{
		Schema: s,
		Hints:  entity.EntryHints{Singular: "task", Plural: "tasks", StatusField: "status"},
		Commands: []present.CommandInput{
			{Verb: "archive", Summary: "archive it", Undo: "unarchive", HasResult: true,
				Hints: entity.CommandHints{Label: "Archive", Destructive: true,
					Confirmation: &entity.CommandConfirmation{
						Title: "Archive the whole collection?", Body: "Every task here stops showing on the list.",
						ConfirmLabel: "Archive them"}}},
			{Verb: "reindex", Summary: "reindex it"},
		},
		Language: "en",
	})
	in.Resource.Commands = []view.CommandDescription{
		{Verb: "archive", Collection: true, Undo: "unarchive", HasResult: true},
		{Verb: "reindex", Collection: true},
	}
	return in
}

// actionVerb finds the drawn action of one verb, and fails the case when the page drew
// no such door, so no comparison below can pass by comparing nothing.
func actionVerb(t *testing.T, page view.Page, verb string) view.Action {
	t.Helper()

	for _, a := range page.Actions {
		if a.Verb == verb {
			return a
		}
	}
	t.Fatalf("the page drew no %q action among %d, so its confirmation was never compared", verb, len(page.Actions))
	return view.Action{}
}
