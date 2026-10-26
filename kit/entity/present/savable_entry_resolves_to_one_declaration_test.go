package present_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

func savableInput() present.Input {
	return present.Input{
		Schema: entity.Schema{Entity: "outfit", Fields: []entity.Field{
			{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
			{Name: "title", Type: entity.TypeString},
			{Name: "revision", Type: entity.TypeInt, ReadOnly: true},
		}},
		Hints: entity.EntryHints{Saving: &entity.SavingHints{
			Module: "look", Kind: "outfit", KeyField: "id", VersionField: "revision",
			Words: entity.SavingWords{Save: "Save look", ListTitle: "My lookbook"},
		}},
		SavingAvailable: true,
	}
}

// TestResolveAnswersSavableOnlyWhereTheEntryDeclaredAndTheReadPublished pins the
// three ways a resolved reading answers null, and the one way it answers a
// declaration: no saving capability in this installation, no declaration on the
// entry, a declaration naming a field the record never publishes.
func TestResolveAnswersSavableOnlyWhereTheEntryDeclaredAndTheReadPublished(t *testing.T) {
	t.Parallel()
	t.Run("a declared pair the record publishes", func(t *testing.T) {
		got := present.Resolve(savableInput()).Saving
		if got == nil {
			t.Fatal("Resolve().Saving = nil, want the declaration")
		}
		if got.Module != "look" || got.Kind != "outfit" || got.KeyField != "id" || got.VersionField != "revision" {
			t.Errorf("Resolve().Saving = %+v, want the declared pair and its two fields", got)
		}
	})
	t.Run("no saving capability composed", func(t *testing.T) {
		in := savableInput()
		in.SavingAvailable = false
		if got := present.Resolve(in).Saving; got != nil {
			t.Errorf("Resolve().Saving = %+v, want nil: nothing offered saving, so nothing offers a control", got)
		}
	})
	t.Run("no declaration", func(t *testing.T) {
		in := savableInput()
		in.Hints.Saving = nil
		if got := present.Resolve(in).Saving; got != nil {
			t.Errorf("Resolve().Saving = %+v, want nil for an entry nobody declared savable", got)
		}
	})
	for name, edit := range map[string]func(*present.Input){
		"a field the record does not publish": func(in *present.Input) {
			in.Hints.Saving.KeyField = "look_id"
		},
		"a pair that is not slugs": func(in *present.Input) { in.Hints.Saving.Module = "My Looks" },
		"a version field it omits": func(in *present.Input) { in.Hints.Saving.VersionField = "version" },
		"a key field kept off every screen": func(in *present.Input) {
			for i := range in.Schema.Fields {
				if in.Schema.Fields[i].Name == "id" {
					in.Schema.Fields[i].Presentation.Visibility = "hidden"
				}
			}
		},
		// A commands-only entry publishes no record at all, so no key or version of
		// one reaches a person, and a control over values nobody was shown is the
		// refusal this guard exists to make.
		"an entry that publishes no record": func(in *present.Input) { in.CommandsOnly = true },
	} {
		t.Run(name, func(t *testing.T) {
			in := savableInput()
			edit(&in)
			if got := present.Resolve(in).Saving; got != nil {
				t.Errorf("Resolve().Saving = %+v, want nil: %s is a wiring mistake the gate refuses, not a control", got, name)
			}
		})
	}
}

// TestResolveFillsTheWordsTheAuthorLeftOutWithTheOneFallback keeps the four
// fallbacks in one place: a declared word travels, a blank one asks for the
// reading every shell shows, and no shell writes its own.
func TestResolveFillsTheWordsTheAuthorLeftOutWithTheOneFallback(t *testing.T) {
	t.Parallel()
	got := present.Resolve(savableInput()).Saving.Words
	if got.Save != "Save look" || got.ListTitle != "My lookbook" {
		t.Errorf("words = %+v, want the declared words kept", got)
	}
	if got.Saved != "Saved" || got.Remove != "Remove from saved" {
		t.Errorf("words = %+v, want the English fallback for the two words nobody wrote", got)
	}
}

func TestResolveAsksTheCallerForTheSavingWordsInTheRequestLanguage(t *testing.T) {
	t.Parallel()
	in := savableInput()
	in.Hints.Saving.Words = entity.SavingWords{}
	in.Text = func(key, fallback string) string {
		if key == "saving.remove" {
			return "Retirar"
		}
		return fallback
	}
	got := present.Resolve(in).Saving.Words
	if got.Remove != "Retirar" {
		t.Errorf("Remove = %q, want the caller's translation of saving.remove", got.Remove)
	}
	if got.ListTitle != "Saved items" {
		t.Errorf("ListTitle = %q, want the fallback for a pair that declared no title", got.ListTitle)
	}
	// A word the entry wrote is its own copy, read in this language under the entry's
	// key before the reading was built: the shared key answers only the entry that
	// wrote nothing, or one generic label would replace what the author chose.
	declared := savableInput()
	declared.Text = func(string, string) string { return "Guardar" }
	if got := present.Resolve(declared).Saving.Words; got.Save != "Save look" {
		t.Errorf("Save = %q with the shared key answering every ask, want the declared word to travel", got.Save)
	}
}

// TestResolveWritesNothingIntoTheDeclarationItResolves keeps a hint shared by ten
// languages a value: the resolved words land on the resolved reading, never back
// into the registration ui/screens resolved them from.
func TestResolveWritesNothingIntoTheDeclarationItResolves(t *testing.T) {
	t.Parallel()
	in := savableInput()
	want := *in.Hints.Saving
	resolved := present.Resolve(in)
	if resolved.Saving == nil || *in.Hints.Saving != want {
		t.Fatalf("Resolve changed the declaration or left nothing behind: %+v", resolved.Saving)
	}
}
